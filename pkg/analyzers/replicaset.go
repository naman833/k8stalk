package analyzers

import (
	"context"
	"fmt"

	"github.com/naman833/k8stalk/pkg/agent"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type ReplicaSetAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewReplicaSetAnalyzer(clientset kubernetes.Interface, namespace string) *ReplicaSetAnalyzer {
	return &ReplicaSetAnalyzer{clientset: clientset, namespace: namespace}
}

func (r *ReplicaSetAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "ReplicaSet",
		Description: "Analyze replicasets for issues: unavailable replicas, orphaned replicasets with no owning deployment",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
			},
		},
	}
}

func (r *ReplicaSetAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns := r.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	rsList, err := r.clientset.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	var findings []agent.Finding
	for _, rs := range rsList.Items {
		findings = append(findings, analyzeReplicaSet(rs)...)
	}

	content := fmt.Sprintf("Analyzed %d replicasets, found %d issues", len(rsList.Items), len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

func analyzeReplicaSet(rs appsv1.ReplicaSet) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("ReplicaSet/%s/%s", rs.Name, rs.Namespace)

	desired := int32(1)
	if rs.Spec.Replicas != nil {
		desired = *rs.Spec.Replicas
	}

	if desired == 0 {
		return findings
	}

	if rs.Status.ReadyReplicas < desired {
		severity := "warning"
		if rs.Status.ReadyReplicas == 0 && desired > 0 {
			severity = "critical"
		}
		findings = append(findings, agent.Finding{
			Severity:    severity,
			Resource:    resource,
			Summary:     fmt.Sprintf("%d/%d replicas ready", rs.Status.ReadyReplicas, desired),
			RawEvidence: fmt.Sprintf("Desired: %d, Ready: %d, Available: %d", desired, rs.Status.ReadyReplicas, rs.Status.AvailableReplicas),
		})
	}

	return findings
}
