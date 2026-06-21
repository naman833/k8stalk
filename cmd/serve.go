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

var servePort int

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the chat server without opening a browser",
	Long:  `Serve starts the local HTTP server in headless mode for scripting or remote localhost access.`,
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "Port for the server")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	db, err := store.Open()
	if err != nil {
		return fmt.Errorf("open history database: %w", err)
	}
	defer db.Close()

	repo := store.NewRepository(db)

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

	provider, err := llm.NewProvider(backendName, cfg, modelOverride)
	if err != nil {
		return fmt.Errorf("create LLM provider: %w", err)
	}
	provider = llm.WrapWithPromptedTools(provider)

	k8sClient, err := k8s.NewClient(kubeCtx)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}

	registry := agent.NewRegistry()
	analyzers.RegisterAll(registry, k8sClient, namespace)
	gitops.RegisterTools(registry, k8sClient.RestConfig)

	san := sanitize.New(!noAnon)
	memory := store.NewSQLiteMemory(repo)
	agentInstance := agent.NewAgent(provider, registry, san, memory)

	addr := fmt.Sprintf("127.0.0.1:%d", servePort)
	server := webui.NewServer(agentInstance, repo, provider, san, addr)
	return server.Start(false) // headless: serve does not open a browser (use 'chat' for that)
}
