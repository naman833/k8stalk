package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/naman833/k8stalk/pkg/config"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage LLM provider authentication",
}

var authAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new LLM provider backend",
	RunE:  runAuthAdd,
}

var authListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured backends",
	RunE:  runAuthList,
}

var authRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a configured backend",
	RunE:  runAuthRemove,
}

var authDefaultCmd = &cobra.Command{
	Use:   "default",
	Short: "Set the default backend",
	RunE:  runAuthDefault,
}

var (
	authBackend string
	authModel   string
	authBaseURL string
	authRegion  string
)

func init() {
	authAddCmd.Flags().StringVar(&authBackend, "backend", "", "Backend name (anthropic, openai, ollama, amazonbedrock, azureopenai, google, vertexai, customrest)")
	authAddCmd.Flags().StringVar(&authModel, "model", "", "Model name")
	authAddCmd.Flags().StringVar(&authBaseURL, "baseurl", "", "Base URL for the backend")
	authAddCmd.Flags().StringVar(&authRegion, "region", "", "AWS region (for bedrock)")
	_ = authAddCmd.MarkFlagRequired("backend")

	authRemoveCmd.Flags().StringVar(&authBackend, "backend", "", "Backend to remove")
	_ = authRemoveCmd.MarkFlagRequired("backend")

	authDefaultCmd.Flags().StringVar(&authBackend, "backend", "", "Backend to set as default")
	_ = authDefaultCmd.MarkFlagRequired("backend")

	authCmd.AddCommand(authAddCmd, authListCmd, authRemoveCmd, authDefaultCmd)
	rootCmd.AddCommand(authCmd)
}

func runAuthAdd(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}

	provider := config.ProviderConfig{
		Backend: authBackend,
		Model:   authModel,
		BaseURL: authBaseURL,
		Region:  authRegion,
	}

	// Check environment for API key based on backend
	switch authBackend {
	case "anthropic":
		if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
			provider.APIKeyEnv = "ANTHROPIC_API_KEY"
		}
	case "openai":
		if key := os.Getenv("OPENAI_API_KEY"); key != "" {
			provider.APIKeyEnv = "OPENAI_API_KEY"
		}
	case "azureopenai":
		if key := os.Getenv("AZURE_OPENAI_API_KEY"); key != "" {
			provider.APIKeyEnv = "AZURE_OPENAI_API_KEY"
		}
	case "google", "vertexai":
		if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
			provider.APIKeyEnv = "GOOGLE_API_KEY"
		}
	}

	cfg.Providers = append(cfg.Providers, provider)

	// Set as default if it's the first provider
	if cfg.DefaultBackend == "" {
		cfg.DefaultBackend = authBackend
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Added backend %q (model: %s)\n", authBackend, authModel)
	return nil
}

func runAuthList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Println("No backends configured. Use 'k8stalk auth add' to add one.")
		return nil
	}

	if len(cfg.Providers) == 0 {
		fmt.Println("No backends configured.")
		return nil
	}

	fmt.Printf("Default: %s\n\n", cfg.DefaultBackend)
	for _, p := range cfg.Providers {
		parts := []string{p.Backend}
		if p.Model != "" {
			parts = append(parts, fmt.Sprintf("model=%s", p.Model))
		}
		if p.BaseURL != "" {
			parts = append(parts, fmt.Sprintf("url=%s", p.BaseURL))
		}
		if p.Region != "" {
			parts = append(parts, fmt.Sprintf("region=%s", p.Region))
		}
		marker := "  "
		if p.Backend == cfg.DefaultBackend {
			marker = "* "
		}
		fmt.Printf("%s%s\n", marker, strings.Join(parts, " | "))
	}

	return nil
}

func runAuthRemove(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("no config found: %w", err)
	}

	var filtered []config.ProviderConfig
	found := false
	for _, p := range cfg.Providers {
		if p.Backend == authBackend {
			found = true
			continue
		}
		filtered = append(filtered, p)
	}

	if !found {
		return fmt.Errorf("backend %q not found", authBackend)
	}

	cfg.Providers = filtered
	if cfg.DefaultBackend == authBackend {
		if len(cfg.Providers) > 0 {
			cfg.DefaultBackend = cfg.Providers[0].Backend
		} else {
			cfg.DefaultBackend = ""
		}
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Removed backend %q\n", authBackend)
	return nil
}

func runAuthDefault(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("no config found: %w", err)
	}

	found := false
	for _, p := range cfg.Providers {
		if p.Backend == authBackend {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("backend %q not configured; add it first with 'k8stalk auth add'", authBackend)
	}

	cfg.DefaultBackend = authBackend
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Default backend set to %q\n", authBackend)
	return nil
}
