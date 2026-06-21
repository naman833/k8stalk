package analyzers

import (
	"context"
	"fmt"

	"github.com/naman833/k8stalk/pkg/agent"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type NetworkPolicyAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewNetworkPolicyAnalyzer(clientset kubernetes.Interface, namespace string) *NetworkPolicyAnalyzer {
	return &NetworkPolicyAnalyzer{clientset: clientset, namespace: namespace}
}

func (n *NetworkPolicyAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "NetworkPolicy",
		Description: "Analyze network policies for issues: deny-all policies that may block traffic, empty selectors",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
			},
		},
	}
}

func (n *NetworkPolicyAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns := n.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	npList, err := n.clientset.NetworkingV1().NetworkPolicies(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	var findings []agent.Finding
	for _, np := range npList.Items {
		findings = append(findings, analyzeNetworkPolicy(np)...)
	}

	content := fmt.Sprintf("Analyzed %d network policies, found %d issues", len(npList.Items), len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

func analyzeNetworkPolicy(np networkingv1.NetworkPolicy) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("NetworkPolicy/%s/%s", np.Name, np.Namespace)

	// Detect deny-all ingress
	for _, policyType := range np.Spec.PolicyTypes {
		if policyType == networkingv1.PolicyTypeIngress && len(np.Spec.Ingress) == 0 {
			findings = append(findings, agent.Finding{
				Severity:    "info",
				Resource:    resource,
				Summary:     "NetworkPolicy denies all ingress traffic (no ingress rules defined)",
				RawEvidence: fmt.Sprintf("PolicyTypes: Ingress, IngressRules: 0, PodSelector: %v", np.Spec.PodSelector),
			})
		}
		if policyType == networkingv1.PolicyTypeEgress && len(np.Spec.Egress) == 0 {
			findings = append(findings, agent.Finding{
				Severity:    "info",
				Resource:    resource,
				Summary:     "NetworkPolicy denies all egress traffic (no egress rules defined)",
				RawEvidence: fmt.Sprintf("PolicyTypes: Egress, EgressRules: 0, PodSelector: %v", np.Spec.PodSelector),
			})
		}
	}

	return findings
}
