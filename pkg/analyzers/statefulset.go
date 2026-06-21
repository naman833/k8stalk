package analyzers

import (
	"context"
	"fmt"

	"github.com/naman833/k8stalk/pkg/agent"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type StatefulSetAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewStatefulSetAnalyzer(clientset kubernetes.Interface, namespace string) *StatefulSetAnalyzer {
	return &StatefulSetAnalyzer{clientset: clientset, namespace: namespace}
}

func (s *StatefulSetAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "StatefulSet",
		Description: "Analyze statefulsets for issues: unavailable replicas, stuck rollouts, partition problems",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
				"name":      map[string]any{"type": "string", "description": "Specific statefulset name"},
			},
		},
	}
}

func (s *StatefulSetAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns := s.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	stsList, err := s.clientset.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	var findings []agent.Finding
	for _, sts := range stsList.Items {
		findings = append(findings, analyzeStatefulSet(sts)...)
	}

	content := fmt.Sprintf("Analyzed %d statefulsets, found %d issues", len(stsList.Items), len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

func analyzeStatefulSet(sts appsv1.StatefulSet) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("StatefulSet/%s/%s", sts.Name, sts.Namespace)

	desired := int32(1)
	if sts.Spec.Replicas != nil {
		desired = *sts.Spec.Replicas
	}

	if desired == 0 {
		return findings
	}

	if sts.Status.ReadyReplicas < desired {
		severity := "warning"
		if sts.Status.ReadyReplicas == 0 {
			severity = "critical"
		}
		findings = append(findings, agent.Finding{
			Severity:    severity,
			Resource:    resource,
			Summary:     fmt.Sprintf("%d/%d replicas ready", sts.Status.ReadyReplicas, desired),
			RawEvidence: fmt.Sprintf("Desired: %d, Ready: %d, Current: %d, Updated: %d", desired, sts.Status.ReadyReplicas, sts.Status.CurrentReplicas, sts.Status.UpdatedReplicas),
		})
	}

	if sts.Status.CurrentRevision != sts.Status.UpdateRevision && sts.Status.UpdatedReplicas < desired {
		findings = append(findings, agent.Finding{
			Severity:    "warning",
			Resource:    resource,
			Summary:     "StatefulSet rollout in progress or stuck",
			RawEvidence: fmt.Sprintf("CurrentRevision: %s, UpdateRevision: %s, Updated: %d/%d", sts.Status.CurrentRevision, sts.Status.UpdateRevision, sts.Status.UpdatedReplicas, desired),
		})
	}

	return findings
}
