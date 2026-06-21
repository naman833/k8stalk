package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	namespace     string
	kubeCtx       string
	backend       string
	modelOverride string
	outputFmt     string
	noAnon        bool
)

var rootCmd = &cobra.Command{
	Use:   "k8stalk",
	Short: "GitOps-aware Kubernetes diagnostics agent",
	Long: `k8stalk is a conversational, multi-LLM-provider Kubernetes diagnostics agent.
It combines k8sgpt-style static analysis with agentic multi-step reasoning
and native ArgoCD/Flux awareness.`,
}

// SetVersion wires the build-time version string into the root command,
// which enables the --version/-v flag. Falls back to "dev" if unset.
func SetVersion(v string) {
	if v == "" {
		v = "dev"
	}
	rootCmd.Version = v
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace (default: all namespaces)")
	rootCmd.PersistentFlags().StringVar(&kubeCtx, "context", "", "Kubernetes context to use")
	rootCmd.PersistentFlags().StringVarP(&backend, "backend", "b", "", "LLM backend to use (overrides default)")
	rootCmd.PersistentFlags().StringVarP(&modelOverride, "model", "m", "", "Model to use for this invocation (overrides config)")
	rootCmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text, json")
	rootCmd.PersistentFlags().BoolVar(&noAnon, "no-anonymize", false, "Disable anonymization (only use with local backends)")
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}
