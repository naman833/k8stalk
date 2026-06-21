package analyzers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/naman833/k8stalk/pkg/agent"
	"github.com/naman833/k8stalk/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PodAnalyzer checks for pod-level issues: CrashLoopBackOff, ImagePullBackOff,
// pending pods, OOMKilled, etc.
type PodAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewPodAnalyzer(clientset kubernetes.Interface, namespace string) *PodAnalyzer {
	return &PodAnalyzer{
		clientset: clientset,
		namespace: namespace,
	}
}

func (p *PodAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "Pod",
		Description: "Analyze pods for issues: CrashLoopBackOff, ImagePullBackOff, pending, OOMKilled, unready containers, restarts",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{
					"type":        "string",
					"description": "Kubernetes namespace to check (empty for all namespaces)",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Specific pod name to analyze (optional)",
				},
			},
		},
	}
}

func (p *PodAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns := p.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	pods, err := k8s.GetPods(ctx, p.clientset, ns)
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	// Filter by name if specified
	if name, ok := input["name"].(string); ok && name != "" {
		var filtered []corev1.Pod
		for _, pod := range pods {
			if pod.Name == name {
				filtered = append(filtered, pod)
			}
		}
		pods = filtered
	}

	var findings []agent.Finding
	for _, pod := range pods {
		podFindings := analyzePod(pod)
		findings = append(findings, podFindings...)
	}

	content := fmt.Sprintf("Analyzed %d pods, found %d issues", len(pods), len(findings))
	if len(findings) > 0 {
		var summaries []string
		for _, f := range findings {
			summaries = append(summaries, fmt.Sprintf("- [%s] %s: %s", f.Severity, f.Resource, f.Summary))
		}
		content = fmt.Sprintf("Analyzed %d pods, found %d issues:\n%s", len(pods), len(findings), strings.Join(summaries, "\n"))
	}

	return agent.ToolResult{
		Content:  content,
		Findings: findings,
	}, nil
}

func analyzePod(pod corev1.Pod) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("Pod/%s/%s", pod.Name, pod.Namespace)

	// Check pod phase
	switch pod.Status.Phase {
	case corev1.PodPending:
		finding := agent.Finding{
			Severity: "warning",
			Resource: resource,
			Summary:  "Pod is in Pending state",
		}
		// Check conditions for scheduling issues
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
				finding.Summary = fmt.Sprintf("Pod is Pending: %s", cond.Message)
				finding.RawEvidence = fmt.Sprintf("Condition: %s, Reason: %s, Message: %s", cond.Type, cond.Reason, cond.Message)
				finding.Severity = "critical"
			}
		}
		findings = append(findings, finding)

	case corev1.PodFailed:
		finding := agent.Finding{
			Severity:    "critical",
			Resource:    resource,
			Summary:     fmt.Sprintf("Pod failed: %s", pod.Status.Reason),
			RawEvidence: fmt.Sprintf("Phase: Failed, Reason: %s, Message: %s", pod.Status.Reason, pod.Status.Message),
		}
		findings = append(findings, finding)
	}

	// Check container statuses
	for _, cs := range pod.Status.ContainerStatuses {
		findings = append(findings, analyzeContainerStatus(resource, cs)...)
	}

	// Check init container statuses
	for _, cs := range pod.Status.InitContainerStatuses {
		findings = append(findings, analyzeContainerStatus(resource, cs)...)
	}

	return findings
}

// formatTime renders a Kubernetes API timestamp in RFC3339 form, returning
// "unknown" for the zero value so summaries stay readable.
func formatTime(t metav1.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.Format(time.RFC3339)
}

func analyzeContainerStatus(resource string, cs corev1.ContainerStatus) []agent.Finding {
	var findings []agent.Finding

	// Check waiting state
	if cs.State.Waiting != nil {
		switch cs.State.Waiting.Reason {
		case "CrashLoopBackOff":
			findings = append(findings, agent.Finding{
				Severity:    "critical",
				Resource:    resource,
				Summary:     fmt.Sprintf("Container %q is in CrashLoopBackOff (restarts: %d)", cs.Name, cs.RestartCount),
				RawEvidence: fmt.Sprintf("Container: %s, State: Waiting, Reason: CrashLoopBackOff, RestartCount: %d, Message: %s", cs.Name, cs.RestartCount, cs.State.Waiting.Message),
			})
		case "ImagePullBackOff", "ErrImagePull":
			findings = append(findings, agent.Finding{
				Severity:    "critical",
				Resource:    resource,
				Summary:     fmt.Sprintf("Container %q cannot pull image: %s", cs.Name, cs.State.Waiting.Message),
				RawEvidence: fmt.Sprintf("Container: %s, State: Waiting, Reason: %s, Image: %s, Message: %s", cs.Name, cs.State.Waiting.Reason, cs.Image, cs.State.Waiting.Message),
			})
		case "CreateContainerConfigError":
			findings = append(findings, agent.Finding{
				Severity:    "critical",
				Resource:    resource,
				Summary:     fmt.Sprintf("Container %q config error: %s", cs.Name, cs.State.Waiting.Message),
				RawEvidence: fmt.Sprintf("Container: %s, State: Waiting, Reason: CreateContainerConfigError, Message: %s", cs.Name, cs.State.Waiting.Message),
			})
		default:
			if cs.State.Waiting.Reason != "" && cs.State.Waiting.Reason != "ContainerCreating" && cs.State.Waiting.Reason != "PodInitializing" {
				findings = append(findings, agent.Finding{
					Severity:    "warning",
					Resource:    resource,
					Summary:     fmt.Sprintf("Container %q is waiting: %s", cs.Name, cs.State.Waiting.Reason),
					RawEvidence: fmt.Sprintf("Container: %s, State: Waiting, Reason: %s, Message: %s", cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message),
				})
			}
		}
	}

	// Check terminated state
	if cs.State.Terminated != nil {
		switch cs.State.Terminated.Reason {
		case "OOMKilled":
			findings = append(findings, agent.Finding{
				Severity:    "critical",
				Resource:    resource,
				Summary:     fmt.Sprintf("Container %q was OOMKilled (exit code: %d)", cs.Name, cs.State.Terminated.ExitCode),
				RawEvidence: fmt.Sprintf("Container: %s, State: Terminated, Reason: OOMKilled, ExitCode: %d", cs.Name, cs.State.Terminated.ExitCode),
			})
		case "Error":
			if cs.State.Terminated.ExitCode != 0 {
				findings = append(findings, agent.Finding{
					Severity:    "warning",
					Resource:    resource,
					Summary:     fmt.Sprintf("Container %q terminated with error (exit code: %d)", cs.Name, cs.State.Terminated.ExitCode),
					RawEvidence: fmt.Sprintf("Container: %s, State: Terminated, Reason: Error, ExitCode: %d, Message: %s", cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Message),
				})
			}
		}
	}

	// Check last termination state. This surfaces WHY a container last died even
	// when its current state is Running or Waiting (e.g. CrashLoopBackOff). The
	// reason/exitCode/finishedAt come straight from the Kubernetes API — the same
	// data `kubectl describe pod` renders under "Last State" — so this is a
	// deterministic finding requiring no LLM.
	if cs.LastTerminationState.Terminated != nil {
		term := cs.LastTerminationState.Terminated
		if term.Reason != "" {
			severity := "info"
			if term.Reason == "OOMKilled" || term.Reason == "Error" {
				severity = "critical"
			}

			var summary string
			if term.Reason == "OOMKilled" {
				summary = fmt.Sprintf("Container %q was OOMKilled (exit code %d) at %s", cs.Name, term.ExitCode, formatTime(term.FinishedAt))
			} else {
				summary = fmt.Sprintf("Container %q last terminated with reason %q (exit code %d) at %s", cs.Name, term.Reason, term.ExitCode, formatTime(term.FinishedAt))
			}

			findings = append(findings, agent.Finding{
				Severity: severity,
				Resource: resource,
				Summary:  summary,
				RawEvidence: fmt.Sprintf(
					"Container: %s, LastState: Terminated, Reason: %s, ExitCode: %d, ContainerID: %s, StartedAt: %s, FinishedAt: %s",
					cs.Name, term.Reason, term.ExitCode, term.ContainerID, formatTime(term.StartedAt), formatTime(term.FinishedAt),
				),
			})
		}
	}

	// Check high restart count
	if cs.RestartCount > 5 && cs.State.Waiting == nil {
		findings = append(findings, agent.Finding{
			Severity:    "warning",
			Resource:    resource,
			Summary:     fmt.Sprintf("Container %q has high restart count: %d", cs.Name, cs.RestartCount),
			RawEvidence: fmt.Sprintf("Container: %s, RestartCount: %d, Ready: %t", cs.Name, cs.RestartCount, cs.Ready),
		})
	}

	// Check readiness
	if !cs.Ready && cs.State.Running != nil {
		findings = append(findings, agent.Finding{
			Severity:    "warning",
			Resource:    resource,
			Summary:     fmt.Sprintf("Container %q is running but not ready", cs.Name),
			RawEvidence: fmt.Sprintf("Container: %s, Ready: false, Started: %v", cs.Name, cs.Started),
		})
	}

	return findings
}
