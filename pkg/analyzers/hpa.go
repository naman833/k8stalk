package analyzers

import (
	"context"
	"fmt"

	"github.com/naman833/k8stalk/pkg/agent"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type HPAAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewHPAAnalyzer(clientset kubernetes.Interface, namespace string) *HPAAnalyzer {
	return &HPAAnalyzer{clientset: clientset, namespace: namespace}
}

func (h *HPAAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "HPA",
		Description: "Analyze HorizontalPodAutoscalers for issues: unable to scale, at max replicas, metrics unavailable",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
				"name":      map[string]any{"type": "string", "description": "Specific HPA name"},
			},
		},
	}
}

func (h *HPAAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns := h.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	hpaList, err := h.clientset.AutoscalingV2().HorizontalPodAutoscalers(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	var findings []agent.Finding
	for _, hpa := range hpaList.Items {
		findings = append(findings, analyzeHPA(hpa)...)
	}

	content := fmt.Sprintf("Analyzed %d HPAs, found %d issues", len(hpaList.Items), len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

func analyzeHPA(hpa autoscalingv2.HorizontalPodAutoscaler) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("HPA/%s/%s", hpa.Name, hpa.Namespace)

	// At max replicas
	if hpa.Status.CurrentReplicas >= hpa.Spec.MaxReplicas {
		findings = append(findings, agent.Finding{
			Severity:    "warning",
			Resource:    resource,
			Summary:     fmt.Sprintf("HPA at max replicas (%d/%d)", hpa.Status.CurrentReplicas, hpa.Spec.MaxReplicas),
			RawEvidence: fmt.Sprintf("Current: %d, Min: %d, Max: %d", hpa.Status.CurrentReplicas, *hpa.Spec.MinReplicas, hpa.Spec.MaxReplicas),
		})
	}

	// Check conditions
	for _, cond := range hpa.Status.Conditions {
		if cond.Type == autoscalingv2.ScalingActive && cond.Status == "False" {
			findings = append(findings, agent.Finding{
				Severity:    "critical",
				Resource:    resource,
				Summary:     fmt.Sprintf("HPA unable to scale: %s", cond.Message),
				RawEvidence: fmt.Sprintf("Condition: ScalingActive=False, Reason: %s, Message: %s", cond.Reason, cond.Message),
			})
		}
		if cond.Type == autoscalingv2.AbleToScale && cond.Status == "False" {
			findings = append(findings, agent.Finding{
				Severity:    "warning",
				Resource:    resource,
				Summary:     fmt.Sprintf("HPA unable to compute metrics: %s", cond.Message),
				RawEvidence: fmt.Sprintf("Condition: AbleToScale=False, Reason: %s, Message: %s", cond.Reason, cond.Message),
			})
		}
	}

	return findings
}
