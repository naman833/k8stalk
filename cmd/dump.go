package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/naman833/k8stalk/pkg/agent"
	"github.com/naman833/k8stalk/pkg/analyzers"
	"github.com/naman833/k8stalk/pkg/config"
	"github.com/naman833/k8stalk/pkg/k8s"
	"github.com/spf13/cobra"
)

var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Export a diagnostic bundle of cluster state",
	Long:  `Dump exports all analyzer findings as a JSON bundle for offline review or sharing.`,
	RunE:  runDump,
}

func init() {
	rootCmd.AddCommand(dumpCmd)
}

func runDump(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	k8sClient, err := k8s.NewClient(kubeCtx)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}

	registry := agent.NewRegistry()
	analyzers.RegisterAll(registry, k8sClient, namespace)

	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}

	type DumpOutput struct {
		Context   string          `json:"context"`
		Namespace string          `json:"namespace"`
		Backend   string          `json:"backend"`
		Findings  []agent.Finding `json:"findings"`
	}

	dump := DumpOutput{
		Context:   k8sClient.Context,
		Namespace: namespace,
		Backend:   cfg.DefaultBackend,
	}

	for _, tool := range registry.All() {
		result, err := tool.Execute(ctx, map[string]any{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s failed: %v\n", tool.Spec().Name, err)
			continue
		}
		dump.Findings = append(dump.Findings, result.Findings...)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(dump)
}
