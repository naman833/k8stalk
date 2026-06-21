package analyzers

import (
	"context"
	"fmt"

	"github.com/naman833/k8stalk/pkg/agent"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type PVCAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewPVCAnalyzer(clientset kubernetes.Interface, namespace string) *PVCAnalyzer {
	return &PVCAnalyzer{clientset: clientset, namespace: namespace}
}

func (p *PVCAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "PersistentVolumeClaim",
		Description: "Analyze PVCs for issues: pending claims, lost volumes, capacity problems",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
				"name":      map[string]any{"type": "string", "description": "Specific PVC name"},
			},
		},
	}
}

func (p *PVCAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns := p.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	pvcList, err := p.clientset.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	var findings []agent.Finding
	for _, pvc := range pvcList.Items {
		findings = append(findings, analyzePVC(pvc)...)
	}

	content := fmt.Sprintf("Analyzed %d PVCs, found %d issues", len(pvcList.Items), len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

func analyzePVC(pvc corev1.PersistentVolumeClaim) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("PVC/%s/%s", pvc.Name, pvc.Namespace)

	switch pvc.Status.Phase {
	case corev1.ClaimPending:
		findings = append(findings, agent.Finding{
			Severity:    "critical",
			Resource:    resource,
			Summary:     "PVC is pending (not bound to a PV)",
			RawEvidence: fmt.Sprintf("Phase: Pending, StorageClass: %v, AccessModes: %v", pvc.Spec.StorageClassName, pvc.Spec.AccessModes),
		})
	case corev1.ClaimLost:
		findings = append(findings, agent.Finding{
			Severity:    "critical",
			Resource:    resource,
			Summary:     "PVC has lost its bound PersistentVolume",
			RawEvidence: fmt.Sprintf("Phase: Lost, VolumeName: %s", pvc.Spec.VolumeName),
		})
	}

	return findings
}
