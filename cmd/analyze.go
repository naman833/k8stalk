package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/naman833/k8stalk/pkg/agent"
	"github.com/naman833/k8stalk/pkg/analyzers"
	"github.com/naman833/k8stalk/pkg/config"
	"github.com/naman833/k8stalk/pkg/gitops"
	"github.com/naman833/k8stalk/pkg/k8s"
	"github.com/naman833/k8stalk/pkg/llm"
	"github.com/naman833/k8stalk/pkg/sanitize"
	"github.com/spf13/cobra"
)

var (
	analyzeFilter  string
	analyzeExplain bool
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Run fixed-sweep analysis across Kubernetes resources",
	Long: `Analyze scans your cluster for common issues across pods, deployments,
services, and other resources. Similar to k8sgpt analyze but with GitOps awareness.`,
	RunE: runAnalyze,
}

func init() {
	analyzeCmd.Flags().StringVar(&analyzeFilter, "filter", "", "Comma-separated list of analyzers to run (e.g. Pod,Deployment)")
	analyzeCmd.Flags().BoolVar(&analyzeExplain, "explain", false, "Use LLM to explain findings")
	rootCmd.AddCommand(analyzeCmd)
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize k8s client
	k8sClient, err := k8s.NewClient(kubeCtx)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Build tool registry with analyzers + GitOps tools
	registry := agent.NewRegistry()
	analyzers.RegisterAll(registry, k8sClient, namespace)
	gitops.RegisterTools(registry, k8sClient.RestConfig)

	// Apply filter if specified
	var toolsToRun []agent.Tool
	if analyzeFilter != "" {
		filters := strings.Split(analyzeFilter, ",")
		filterMap := make(map[string]bool)
		for _, f := range filters {
			filterMap[strings.TrimSpace(f)] = true
		}
		for _, t := range registry.All() {
			if filterMap[t.Spec().Name] {
				toolsToRun = append(toolsToRun, t)
			}
		}
		if len(toolsToRun) == 0 {
			return fmt.Errorf("no analyzers match filter %q; available: %s", analyzeFilter, strings.Join(registry.Names(), ", "))
		}
	} else {
		toolsToRun = registry.All()
	}

	// Run all selected analyzers
	var allFindings []agent.Finding
	for _, tool := range toolsToRun {
		result, err := tool.Execute(ctx, map[string]any{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: analyzer %s failed: %v\n", tool.Spec().Name, err)
			continue
		}
		allFindings = append(allFindings, result.Findings...)
	}

	if len(allFindings) == 0 {
		switch outputFmt {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode([]agent.Finding{})
		default:
			fmt.Println("No issues found.")
			return nil
		}
	}

	// If --explain is set, use LLM to provide explanation
	if analyzeExplain {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		backendName := backend
		if backendName == "" {
			backendName = cfg.DefaultBackend
		}
		if backendName == "" {
			return fmt.Errorf("no backend specified; use --backend or set a default with 'k8stalk auth default'")
		}

		provider, err := llm.NewProvider(backendName, cfg, modelOverride)
		if err != nil {
			return fmt.Errorf("failed to create LLM provider: %w", err)
		}

		san := sanitize.New(!noAnon)

		for i, finding := range allFindings {
			prompt := fmt.Sprintf("You are a Kubernetes expert. Explain the following issue and suggest a fix:\n\nResource: %s\nSeverity: %s\nSummary: %s\nEvidence:\n%s",
				finding.Resource, finding.Severity, finding.Summary, finding.RawEvidence)

			sanitizedPrompt := san.Sanitize(prompt)

			messages := []llm.Message{
				{Role: llm.RoleUser, Content: sanitizedPrompt},
			}

			resp, err := provider.Chat(ctx, messages, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: LLM explain failed for %s: %v\n", finding.Resource, err)
				continue
			}

			allFindings[i].Explanation = san.Desanitize(resp.Content)
		}
	}

	// Output results
	switch outputFmt {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(allFindings)
	default:
		for _, f := range allFindings {
			fmt.Printf("[%s] %s\n", strings.ToUpper(f.Severity), f.Resource)
			fmt.Printf("  %s\n", f.Summary)
			if f.Explanation != "" {
				fmt.Printf("  Explanation: %s\n", f.Explanation)
			}
			fmt.Println()
		}
	}

	return nil
}
