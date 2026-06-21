package analyzers

import (
	"context"
	"fmt"

	"github.com/naman833/k8stalk/pkg/agent"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type EventAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewEventAnalyzer(clientset kubernetes.Interface, namespace string) *EventAnalyzer {
	return &EventAnalyzer{clientset: clientset, namespace: namespace}
}

func (e *EventAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "Event",
		Description: "Retrieve and analyze recent Kubernetes events, especially warnings, for a namespace or specific resource",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace":     map[string]any{"type": "string", "description": "Kubernetes namespace"},
				"resource_name": map[string]any{"type": "string", "description": "Filter events by involved object name"},
			},
		},
	}
}

func (e *EventAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns := e.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	opts := metav1.ListOptions{}
	if name, ok := input["resource_name"].(string); ok && name != "" {
		opts.FieldSelector = fmt.Sprintf("involvedObject.name=%s", name)
	}

	eventList, err := e.clientset.CoreV1().Events(ns).List(ctx, opts)
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	var findings []agent.Finding
	for _, event := range eventList.Items {
		if event.Type == corev1.EventTypeWarning {
			findings = append(findings, agent.Finding{
				Severity:    "warning",
				Resource:    fmt.Sprintf("%s/%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name, event.InvolvedObject.Namespace),
				Summary:     fmt.Sprintf("[%s] %s", event.Reason, event.Message),
				RawEvidence: fmt.Sprintf("Type: Warning, Reason: %s, Count: %d, Source: %s, Message: %s", event.Reason, event.Count, event.Source.Component, event.Message),
			})
		}
	}

	content := fmt.Sprintf("Found %d events (%d warnings)", len(eventList.Items), len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}
