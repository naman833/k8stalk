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

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose [question]",
	Short: "One-shot agentic diagnosis query (no web UI)",
	Long: `Diagnose uses an LLM agent to investigate your cluster based on a natural
language question. It decides which tools to call and correlates findings
across resources.

Use --fast for single-shot mode — gathers all diagnostic data upfront
(status, events, logs for any flagged pods) before asking the LLM once,
rather than letting the LLM choose tools turn by turn. More reliable for
smaller/local models that struggle with multi-step tool selection and loop
termination; the default agentic mode remains better suited to larger/more
capable models for deeper multi-step correlation.

Example:
  k8stalk diagnose "why is my checkout pod crashlooping?"
  k8stalk diagnose "are there any PVC issues in the payments namespace?"
  k8stalk diagnose "why is gatekeeper-audit crashing?" --fast`,
	Args: cobra.MinimumNArgs(1),
	RunE: runDiagnose,
}

var fastMode bool

func init() {
	diagnoseCmd.Flags().BoolVar(&fastMode, "fast", false, "Single-shot mode: gather all data upfront, then ask LLM once")
	rootCmd.AddCommand(diagnoseCmd)
}

func runDiagnose(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	question := strings.Join(args, " ")

	// Load config
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

	// Initialize provider (wrap with prompted-tools fallback if needed)
	provider, err := llm.NewProvider(backendName, cfg, modelOverride)
	if err != nil {
		return fmt.Errorf("failed to create LLM provider: %w", err)
	}
	provider = llm.WrapWithPromptedTools(provider)

	// Initialize k8s client
	k8sClient, err := k8s.NewClient(kubeCtx)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Build tool registry
	registry := agent.NewRegistry()
	analyzers.RegisterAll(registry, k8sClient, namespace)
	gitops.RegisterTools(registry, k8sClient.RestConfig)

	// Create sanitizer
	san := sanitize.New(!noAnon)

	// Fast mode: single-shot diagnostic
	if fastMode {
		return runDiagnoseFast(ctx, registry, provider, san, question)
	}

	// Run agent loop
	maxTurns := 8
	systemPrompt := `You are k8stalk, an expert Kubernetes diagnostics agent. You have access to tools that can inspect cluster resources.

CRITICAL RULES:
- You MUST call the Logs tool for any pod that is crashing, restarting, or in CrashLoopBackOff. Set previous=true to get the crashed instance's logs.
- NEVER tell the user to run kubectl commands manually. You have all the tools needed.
- NEVER say you cannot access logs. You HAVE a Logs tool — use it.

Diagnostic workflow:
1. Check Pod status to identify unhealthy pods
2. For any crashing/restarting pod, IMMEDIATELY call the Logs tool (with previous=true) to get the actual error
3. Check Events for additional context
4. Correlate all findings and provide root cause analysis with evidence from the logs

Always cite specific log lines or error messages in your final answer.`

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: san.Sanitize(question)},
	}

	spin := newSpinner("Thinking...")

	// Accumulate findings across all tool calls so -o json can report them
	var allFindings []agent.Finding

	// After this many tool-calling turns, force the model to synthesize
	synthesizeAfter := 5

	for turn := 0; turn < maxTurns; turn++ {
		spin.start()
		// If we've done enough tool-calling turns, stop offering tools
		// so the model is forced to produce a text response
		var specs []llm.ToolSpec
		if turn < synthesizeAfter {
			specs = registry.Specs()
		} else if turn == synthesizeAfter {
			// Inject a nudge to synthesize
			messages = append(messages, llm.Message{
				Role:    llm.RoleUser,
				Content: "You have gathered enough information. Stop calling tools and provide your final diagnosis based on all the evidence collected above.",
			})
		}
		resp, err := provider.Chat(ctx, messages, specs)
		spin.stop()
		if err != nil {
			return fmt.Errorf("LLM chat failed: %w", err)
		}

		if resp.StopReason != "tool_use" || len(resp.ToolCalls) == 0 {
			// Final response — only exit if we have content or we've done at least one turn
			content := san.Desanitize(resp.Content)
			if content != "" || turn > 0 {
				if outputFmt == "json" {
					result := struct {
						Question string          `json:"question"`
						Findings []agent.Finding `json:"findings"`
						Response string          `json:"response"`
					}{
						Question: question,
						Findings: allFindings,
						Response: content,
					}
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(result)
				}
				if content != "" {
					fmt.Println(content)
				} else {
					fmt.Fprintln(os.Stderr, "warning: LLM returned empty response after tool calls")
				}
				return nil
			}
			// On turn 0 with empty content and no tool calls, treat as error
			return fmt.Errorf("LLM returned empty response with no tool calls; check your backend/model configuration")
		}

		// Process tool calls
		messages = append(messages, llm.Message{Role: llm.RoleAssistant, ToolCalls: resp.ToolCalls})

		for _, call := range resp.ToolCalls {
			tool := registry.Get(call.Name)
			if tool == nil {
				messages = append(messages, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: call.ID,
					Content:    fmt.Sprintf("error: unknown tool %q", call.Name),
				})
				continue
			}

			result, err := tool.Execute(ctx, call.Input)
			if err != nil {
				messages = append(messages, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: call.ID,
					Content:    fmt.Sprintf("error: %v", err),
				})
				continue
			}

			sanitizedContent := san.Sanitize(result.Content)
			messages = append(messages, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: call.ID,
				Content:    sanitizedContent,
			})

			// Accumulate structured findings for -o json output
			allFindings = append(allFindings, result.Findings...)

			if outputFmt != "json" {
				fmt.Fprintf(os.Stderr, "  [tool: %s] %s\n", call.Name, truncate(result.Content, 80))
			}
		}
		// Loop continues — next iteration calls LLM again with updated message history
	}

	fmt.Fprintf(os.Stderr, "Investigation incomplete after %d turns. The model may need a simpler question or a more capable backend.\n", maxTurns)
	return nil
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func runDiagnoseFast(ctx context.Context, registry *agent.Registry, provider llm.Provider, san *sanitize.Sanitizer, question string) error {
	fmt.Fprintln(os.Stderr, "Gathering cluster data...")

	// Step 1: Run all analyzers except Logs
	var allFindings []agent.Finding
	for _, tool := range registry.All() {
		name := tool.Spec().Name
		if name == "Logs" {
			continue
		}
		result, err := tool.Execute(ctx, map[string]any{})
		if err != nil {
			continue
		}
		allFindings = append(allFindings, result.Findings...)
	}

	// Step 2: For pods flagged with issues, fetch their logs with previous=true
	logsTool := registry.Get("Logs")
	loggedPods := map[string]bool{}
	if logsTool != nil {
		for _, f := range allFindings {
			if !strings.HasPrefix(f.Resource, "Pod/") {
				continue
			}
			if f.Severity != "critical" && f.Severity != "warning" {
				continue
			}
			parts := strings.SplitN(f.Resource, "/", 3)
			if len(parts) < 3 {
				continue
			}
			podName := parts[1]
			podNs := parts[2]
			key := podNs + "/" + podName
			if loggedPods[key] {
				continue // already fetched logs for this pod
			}
			loggedPods[key] = true

			logsResult, err := logsTool.Execute(ctx, map[string]any{
				"pod_name":  podName,
				"namespace": podNs,
				"previous":  true,
			})
			if err == nil && !logsResult.IsError {
				allFindings = append(allFindings, logsResult.Findings...)
			}
		}
	}

	// Step 3: Build a compact prompt — only critical/warning findings + logs for flagged pods
	var sb strings.Builder
	sb.WriteString("# Cluster Diagnostic Data\n\n")
	for _, f := range allFindings {
		// Skip info-level findings unless they are logs (which are always info)
		if f.Severity == "info" && !strings.HasPrefix(f.Summary, "Last ") {
			continue
		}
		sb.WriteString(fmt.Sprintf("## [%s] %s\n%s\n", f.Severity, f.Resource, f.Summary))
		if f.RawEvidence != "" {
			evidence := f.RawEvidence
			// Keep logs to last 50 lines to stay within context limits
			if lines := strings.Split(evidence, "\n"); len(lines) > 50 {
				evidence = strings.Join(lines[len(lines)-50:], "\n")
			}
			if len(evidence) > 1500 {
				evidence = evidence[len(evidence)-1500:]
			}
			sb.WriteString(fmt.Sprintf("```\n%s\n```\n", evidence))
		}
		sb.WriteString("\n")
	}

	sanitizedData := san.Sanitize(sb.String())
	sanitizedQuestion := san.Sanitize(question)

	systemPrompt := `You are k8stalk, an expert Kubernetes diagnostics agent. You are given diagnostic data collected from a cluster including pod status, events, and container logs. Analyze the evidence and provide a clear, concise root cause diagnosis with actionable remediation steps. Cite specific log lines or error messages.`

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: fmt.Sprintf("Question: %s\n\n%s", sanitizedQuestion, sanitizedData)},
	}

	// Step 4: Single LLM call with no tools
	spin := newSpinner("Analyzing...")
	spin.start()
	resp, err := provider.Chat(ctx, messages, nil)
	spin.stop()
	if err != nil {
		return fmt.Errorf("LLM synthesis failed: %w", err)
	}

	content := san.Desanitize(resp.Content)
	if content == "" {
		return fmt.Errorf("LLM returned empty response; try a different model or rephrase the question")
	}

	// Output
	if outputFmt == "json" {
		result := struct {
			Question string          `json:"question"`
			Findings []agent.Finding `json:"findings"`
			Response string          `json:"response"`
		}{
			Question: question,
			Findings: allFindings,
			Response: content,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Println(content)
	return nil
}
