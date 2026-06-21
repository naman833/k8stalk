package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/naman833/k8stalk/pkg/config"
	"github.com/naman833/k8stalk/pkg/llm"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive first-run setup wizard",
	Long: `Interactively configure k8stalk with your preferred LLM backend.
Prompts for backend, model, and credentials, then verifies connectivity
before saving to ~/.config/k8stalk/config.yaml.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

var supportedBackends = []string{
	"anthropic",
	"ollama",
	"openai",
	"azureopenai",
	"google",
	"vertexai",
	"amazonbedrock",
	"customrest",
}

func runInit(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)

	// Check for existing config
	existing, _ := config.Load()
	if len(existing.Providers) > 0 {
		fmt.Printf("Config already exists at %s with %d provider(s).\n", config.ConfigPath(), len(existing.Providers))
		fmt.Print("Overwrite? [y/N]: ")
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Select backend
	fmt.Println("\nSupported backends:")
	for i, b := range supportedBackends {
		fmt.Printf("  %d) %s\n", i+1, b)
	}
	fmt.Print("\nSelect backend [1-8]: ")
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)
	idx, err := strconv.Atoi(choice)
	if err != nil || idx < 1 || idx > len(supportedBackends) {
		return fmt.Errorf("invalid choice %q", choice)
	}
	backendChoice := supportedBackends[idx-1]

	provider := config.ProviderConfig{
		Backend: backendChoice,
	}

	// Backend-specific prompts
	switch backendChoice {
	case "ollama":
		provider.BaseURL = promptWithDefault(reader, "Ollama base URL", "http://localhost:11434")
		provider.Model, err = promptOllamaModel(reader)
		if err != nil {
			return err
		}

	case "anthropic":
		provider.APIKeyEnv = promptAPIKeyEnv(reader, "ANTHROPIC_API_KEY")
		provider.Model = promptWithDefault(reader, "Model", "claude-sonnet-4-6")

	case "openai":
		provider.APIKeyEnv = promptAPIKeyEnv(reader, "OPENAI_API_KEY")
		provider.Model = promptWithDefault(reader, "Model", "gpt-5.4")

	case "azureopenai":
		provider.APIKeyEnv = promptAPIKeyEnv(reader, "AZURE_OPENAI_API_KEY")
		fmt.Print("Azure deployment URL (e.g. https://<resource>.openai.azure.com/openai/deployments/<deployment>): ")
		url, _ := reader.ReadString('\n')
		provider.BaseURL = strings.TrimSpace(url)
		if provider.BaseURL == "" {
			return fmt.Errorf("base URL is required for Azure OpenAI")
		}
		provider.Model = promptWithDefault(reader, "Model", "gpt-5.4")

	case "google":
		provider.APIKeyEnv = promptAPIKeyEnv(reader, "GOOGLE_API_KEY")
		provider.Model = promptWithDefault(reader, "Model", "gemini-2.5-flash")

	case "vertexai":
		provider.Model = promptWithDefault(reader, "Model", "gemini-2.5-flash")
		provider.Region = promptWithDefault(reader, "GCP region", "us-central1")
		fmt.Println("Note: Vertex AI uses Application Default Credentials.")
		fmt.Println("Ensure GOOGLE_CLOUD_PROJECT is set and `gcloud auth application-default login` has been run.")

	case "amazonbedrock":
		provider.Region = promptWithDefault(reader, "AWS region", "us-east-1")
		provider.Model = promptWithDefault(reader, "Model", "anthropic.claude-sonnet-4-6-v1:0")
		fmt.Println("Note: Bedrock uses AWS credentials from environment (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY).")

	case "customrest":
		fmt.Print("Base URL (e.g. http://localhost:8000/v1): ")
		url, _ := reader.ReadString('\n')
		provider.BaseURL = strings.TrimSpace(url)
		if provider.BaseURL == "" {
			return fmt.Errorf("base URL is required for custom REST")
		}
		provider.Model = promptWithDefault(reader, "Model", "default")
		fmt.Print("API key env var name (leave empty if none): ")
		keyEnv, _ := reader.ReadString('\n')
		keyEnv = strings.TrimSpace(keyEnv)
		if keyEnv != "" {
			provider.APIKeyEnv = keyEnv
		}
	}

	// Connectivity test
	fmt.Printf("\nTesting connectivity to %s (model: %s)...\n", backendChoice, provider.Model)

	testCfg := &config.Config{
		DefaultBackend: backendChoice,
		Providers:      []config.ProviderConfig{provider},
	}

	for {
		testProvider, err := llm.NewProvider(backendChoice, testCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating provider: %v\n", err)
			fmt.Print("Retry with different settings? [y/N]: ")
			ans, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(ans)) != "y" {
				return fmt.Errorf("setup aborted")
			}
			// Let user fix the issue — for now just retry
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		resp, err := testProvider.Chat(ctx, []llm.Message{
			{Role: llm.RoleUser, Content: "Reply with exactly: ok"},
		}, nil)
		cancel()

		if err != nil {
			fmt.Fprintf(os.Stderr, "Connectivity test failed: %v\n", err)
			fmt.Print("Retry? [y/N]: ")
			ans, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(ans)) != "y" {
				return fmt.Errorf("setup aborted — fix the issue and run 'k8stalk init' again")
			}
			continue
		}

		fmt.Printf("Connected successfully (response: %s)\n", truncate(resp.Content, 40))
		break
	}

	// Save config
	cfg := &config.Config{
		DefaultBackend: backendChoice,
		Providers:      []config.ProviderConfig{provider},
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("\nYou're set up with %s (%s).\n", backendChoice, provider.Model)

	// Try to detect current k8s context
	if ctxName := detectKubeContext(); ctxName != "" {
		fmt.Printf("Using your current Kubernetes context: %s.\n", ctxName)
	}

	fmt.Println("\nRun your first scan:")
	fmt.Println("  k8stalk analyze")
	fmt.Println("\nThen try: k8stalk diagnose \"<question>\" or k8stalk chat for the interactive UI.")

	return nil
}

func promptWithDefault(reader *bufio.Reader, label, defaultVal string) string {
	fmt.Printf("%s [%s]: ", label, defaultVal)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func promptAPIKeyEnv(reader *bufio.Reader, defaultEnv string) string {
	val := os.Getenv(defaultEnv)
	if val != "" {
		fmt.Printf("Found %s in environment.\n", defaultEnv)
		return defaultEnv
	}
	fmt.Printf("Set %s in your shell, then press Enter (or type a different env var name): ", defaultEnv)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultEnv
	}
	return input
}

func promptOllamaModel(reader *bufio.Reader) (string, error) {
	// Try to get installed models from `ollama list`
	models := listOllamaModels()
	if len(models) > 0 {
		fmt.Println("\nInstalled Ollama models:")
		for i, m := range models {
			fmt.Printf("  %d) %s\n", i+1, m)
		}
		fmt.Printf("\nSelect model [1-%d] or type a name: ", len(models))
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if idx, err := strconv.Atoi(input); err == nil && idx >= 1 && idx <= len(models) {
			return models[idx-1], nil
		}
		if input != "" {
			return input, nil
		}
		return models[0], nil
	}

	// No installed models — don't guess a model name that will fail the
	// connectivity check anyway. Tell the user how to fix it and exit cleanly.
	return "", fmt.Errorf("no Ollama models found. Pull one first, e.g.: ollama pull gemma4:12b — then run 'k8stalk init' again")
}

func listOllamaModels() []string {
	cmd := exec.Command("ollama", "list")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var models []string
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if i == 0 { // skip header
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 1 {
			models = append(models, fields[0])
		}
	}
	return models
}

func detectKubeContext() string {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
	rawConfig, err := kubeConfig.RawConfig()
	if err != nil {
		return ""
	}
	return rawConfig.CurrentContext
}
