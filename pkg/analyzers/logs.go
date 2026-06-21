package analyzers

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/naman833/k8stalk/pkg/agent"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	logsTailLines    = int64(50)
	logsScanLines    = int64(100)
	logsMaxEvidence  = 3000
	logsErrorSection = "Likely error lines:"
	logsTailSection  = "Full tail (last 50 lines):"
)

// logsErrorKeywords are case-insensitive substrings that indicate error-relevant lines.
var logsErrorKeywords = []string{
	"panic", "fatal", "error", "exception", "traceback", "exit status", "segfault",
}

// LogsAnalyzer fetches pod logs for targeted diagnosis (e.g. CrashLoopBackOff).
type LogsAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewLogsAnalyzer(clientset kubernetes.Interface, namespace string) *LogsAnalyzer {
	return &LogsAnalyzer{
		clientset: clientset,
		namespace: namespace,
	}
}

func (l *LogsAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "Logs",
		Description: "Fetch the last 50 lines of logs from a specific pod container, with error/panic/fatal lines highlighted. Useful for diagnosing CrashLoopBackOff, application errors, startup failures, and OOMKilled pods. Use previous=true to get logs from the crashed/previous instance.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pod_name": map[string]any{
					"type":        "string",
					"description": "Name of the pod to fetch logs from",
				},
				"container": map[string]any{
					"type":        "string",
					"description": "Container name (optional, defaults to first container)",
				},
				"previous": map[string]any{
					"type":        "boolean",
					"description": "If true, fetch logs from the previous/crashed instance (useful for CrashLoopBackOff pods)",
				},
				"namespace": map[string]any{
					"type":        "string",
					"description": "Kubernetes namespace (optional, defaults to configured namespace)",
				},
			},
			"required": []string{"pod_name"},
		},
	}
}

func (l *LogsAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	podName, _ := input["pod_name"].(string)
	if podName == "" {
		return agent.ToolResult{IsError: true, Content: "pod_name is required"}, nil
	}

	ns := l.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	container, _ := input["container"].(string)
	previous, _ := input["previous"].(bool)

	// Fetch the tail (50 lines by default).
	tailLines := logsTailLines
	opts := &corev1.PodLogOptions{
		TailLines: &tailLines,
		Previous:  previous,
	}
	if container != "" {
		opts.Container = container
	}

	req := l.clientset.CoreV1().Pods(ns).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to get logs for pod %s/%s: %v", ns, podName, err)
		return agent.ToolResult{IsError: true, Content: errMsg}, nil
	}
	defer stream.Close()

	logBytes, err := io.ReadAll(stream)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to read logs for pod %s/%s: %v", ns, podName, err)
		return agent.ToolResult{IsError: true, Content: errMsg}, nil
	}

	logText := string(logBytes)
	if logText == "" {
		logText = "(no log output)"
	}

	// Extract error-relevant lines from the tail.
	tailLinesList := strings.Split(strings.TrimRight(logText, "\n"), "\n")
	errorLines := filterErrorLines(tailLinesList)

	// If no error lines found in the 50-line tail, try a larger window (100 lines).
	if len(errorLines) == 0 && logsScanLines > tailLines {
		scanLines := logsScanLines
		scanOpts := &corev1.PodLogOptions{
			TailLines: &scanLines,
			Previous:  previous,
		}
		if container != "" {
			scanOpts.Container = container
		}
		scanReq := l.clientset.CoreV1().Pods(ns).GetLogs(podName, scanOpts)
		scanStream, scanErr := scanReq.Stream(ctx)
		if scanErr == nil {
			scanBytes, readErr := io.ReadAll(scanStream)
			scanStream.Close()
			if readErr == nil && len(scanBytes) > 0 {
				scanText := strings.TrimRight(string(scanBytes), "\n")
				scanLinesList := strings.Split(scanText, "\n")
				errorLines = filterErrorLines(scanLinesList)
			}
		}
	}

	// Build structured RawEvidence: error lines first, then full tail.
	evidence := buildEvidence(errorLines, tailLinesList)

	containerLabel := container
	if containerLabel == "" {
		containerLabel = "(default)"
	}

	summary := fmt.Sprintf("Last %d lines of logs for container %s (previous=%t)", tailLines, containerLabel, previous)
	resource := fmt.Sprintf("Pod/%s/%s", podName, ns)

	finding := agent.Finding{
		Severity:    "info",
		Resource:    resource,
		Summary:     summary,
		RawEvidence: evidence,
	}

	return agent.ToolResult{
		Content:  fmt.Sprintf("%s\n\n%s", summary, evidence),
		Findings: []agent.Finding{finding},
	}, nil
}

// filterErrorLines returns lines containing any of the error keywords (case-insensitive).
func filterErrorLines(lines []string) []string {
	var matched []string
	for _, line := range lines {
		lower := strings.ToLower(line)
		for _, kw := range logsErrorKeywords {
			if strings.Contains(lower, kw) {
				matched = append(matched, line)
				break
			}
		}
	}
	return matched
}

// buildEvidence constructs the RawEvidence string, capping at logsMaxEvidence characters.
// Error matches are preserved in full; the tail section is truncated if needed.
func buildEvidence(errorLines, tailLines []string) string {
	var sb strings.Builder

	// Write error section if present.
	errorSection := ""
	if len(errorLines) > 0 {
		var esb strings.Builder
		esb.WriteString(logsErrorSection)
		esb.WriteByte('\n')
		for _, line := range errorLines {
			esb.WriteString(line)
			esb.WriteByte('\n')
		}
		esb.WriteByte('\n')
		errorSection = esb.String()
		sb.WriteString(errorSection)
	}

	// Calculate remaining budget for the tail section.
	budget := logsMaxEvidence - sb.Len()
	if budget <= 0 {
		// Error section alone exceeds cap; truncate error section at cap.
		result := errorSection
		if len(result) > logsMaxEvidence {
			result = result[:logsMaxEvidence-50] + "\n[truncated, error lines exceeded limit]\n"
		}
		return result
	}

	// Build tail section.
	sb.WriteString(logsTailSection)
	sb.WriteByte('\n')
	budget -= len(logsTailSection) + 1

	omitted := 0
	tailContent := strings.Join(tailLines, "\n")
	if len(tailContent) <= budget {
		sb.WriteString(tailContent)
	} else {
		// Write as many lines as fit within budget.
		used := 0
		written := 0
		for i, line := range tailLines {
			needed := len(line) + 1 // +1 for newline
			if i == 0 {
				needed = len(line)
			}
			if used+needed > budget-60 { // reserve space for truncation notice
				omitted = len(tailLines) - written
				break
			}
			if i > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(line)
			used += needed
			written++
		}
		if omitted > 0 {
			sb.WriteString(fmt.Sprintf("\n[truncated, %d lines omitted]", omitted))
		}
	}

	return sb.String()
}
