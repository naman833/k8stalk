package cmd

import (
	"fmt"

	"github.com/naman833/k8stalk/pkg/agent"
	"github.com/naman833/k8stalk/pkg/analyzers"
	"github.com/naman833/k8stalk/pkg/config"
	"github.com/naman833/k8stalk/pkg/gitops"
	"github.com/naman833/k8stalk/pkg/k8s"
	"github.com/naman833/k8stalk/pkg/llm"
	"github.com/naman833/k8stalk/pkg/sanitize"
	"github.com/naman833/k8stalk/pkg/store"
	"github.com/naman833/k8stalk/pkg/webui"
	"github.com/spf13/cobra"
)

var (
	chatPort         int
	chatClearHistory bool
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Launch the local chat UI for interactive Kubernetes diagnosis",
	Long: `Chat launches a local web server and opens a browser-based chat interface.
The LLM agent can inspect your cluster and provide multi-step diagnostic reasoning.

Chat history is stored locally and never leaves your machine.`,
	RunE: runChat,
}

func init() {
	chatCmd.Flags().IntVar(&chatPort, "port", 8080, "Port for the local web server")
	chatCmd.Flags().BoolVar(&chatClearHistory, "clear-history", false, "Delete all chat history and exit")
	rootCmd.AddCommand(chatCmd)
}

func runChat(cmd *cobra.Command, args []string) error {
	// Open store
	db, err := store.Open()
	if err != nil {
		return fmt.Errorf("open history database: %w", err)
	}
	defer db.Close()

	repo := store.NewRepository(db)

	// Handle --clear-history
	if chatClearHistory {
		if err := repo.DeleteAllSessions(); err != nil {
			return fmt.Errorf("clear history: %w", err)
		}
		fmt.Println("All chat history deleted.")
		return nil
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	backendName := backend
	if backendName == "" {
		backendName = cfg.DefaultBackend
	}
	if backendName == "" {
		return fmt.Errorf("no backend configured; run 'k8stalk auth add' first")
	}

	// Initialize provider (wrap with prompted-tools fallback if needed)
	provider, err := llm.NewProvider(backendName, cfg, modelOverride)
	if err != nil {
		return fmt.Errorf("create LLM provider: %w", err)
	}
	provider = llm.WrapWithPromptedTools(provider)

	// Initialize k8s client
	k8sClient, err := k8s.NewClient(kubeCtx)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}

	// Build tool registry
	registry := agent.NewRegistry()
	analyzers.RegisterAll(registry, k8sClient, namespace)
	gitops.RegisterTools(registry, k8sClient.RestConfig)

	// Create sanitizer and memory
	san := sanitize.New(!noAnon)
	memory := store.NewSQLiteMemory(repo)

	// Create agent
	agentInstance := agent.NewAgent(provider, registry, san, memory)

	// Start server
	addr := fmt.Sprintf("127.0.0.1:%d", chatPort)
	server := webui.NewServer(agentInstance, repo, provider, san, addr)
	return server.Start(true)
}
