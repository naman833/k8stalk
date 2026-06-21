package analyzers

import (
	"context"
	"fmt"

	"github.com/naman833/k8stalk/pkg/agent"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type PDBAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewPDBAnalyzer(clientset kubernetes.Interface, namespace string) *PDBAnalyzer {
	return &PDBAnalyzer{clientset: clientset, namespace: namespace}
}

func (p *PDBAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "PodDisruptionBudget",
		Description: "Analyze PodDisruptionBudgets for issues: blocked evictions, zero disruptions allowed, misconfigured budgets",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
				"name":      map[string]any{"type": "string", "description": "Specific PDB name"},
			},
		},
	}
}

func (p *PDBAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns := p.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	pdbList, err := p.clientset.PolicyV1().PodDisruptionBudgets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	var findings []agent.Finding
	for _, pdb := range pdbList.Items {
		findings = append(findings, analyzePDB(pdb)...)
	}

	content := fmt.Sprintf("Analyzed %d PDBs, found %d issues", len(pdbList.Items), len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

func analyzePDB(pdb policyv1.PodDisruptionBudget) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("PDB/%s/%s", pdb.Name, pdb.Namespace)

	// Zero disruptions allowed (blocking evictions)
	if pdb.Status.DisruptionsAllowed == 0 && pdb.Status.CurrentHealthy > 0 {
		findings = append(findings, agent.Finding{
			Severity:    "warning",
			Resource:    resource,
			Summary:     "PDB allows zero disruptions (may block node drain/upgrades)",
			RawEvidence: fmt.Sprintf("DisruptionsAllowed: 0, CurrentHealthy: %d, DesiredHealthy: %d, ExpectedPods: %d", pdb.Status.CurrentHealthy, pdb.Status.DesiredHealthy, pdb.Status.ExpectedPods),
		})
	}

	// More expected than healthy
	if pdb.Status.CurrentHealthy < pdb.Status.DesiredHealthy {
		findings = append(findings, agent.Finding{
			Severity:    "critical",
			Resource:    resource,
			Summary:     fmt.Sprintf("PDB has fewer healthy pods than desired (%d/%d)", pdb.Status.CurrentHealthy, pdb.Status.DesiredHealthy),
			RawEvidence: fmt.Sprintf("CurrentHealthy: %d, DesiredHealthy: %d, ExpectedPods: %d", pdb.Status.CurrentHealthy, pdb.Status.DesiredHealthy, pdb.Status.ExpectedPods),
		})
	}

	return findings
}
