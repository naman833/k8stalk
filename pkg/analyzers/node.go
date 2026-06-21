package analyzers

import (
	"context"
	"fmt"
	"strings"

	"github.com/naman833/k8stalk/pkg/agent"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type NodeAnalyzer struct {
	clientset kubernetes.Interface
}

func NewNodeAnalyzer(clientset kubernetes.Interface) *NodeAnalyzer {
	return &NodeAnalyzer{clientset: clientset}
}

func (n *NodeAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "Node",
		Description: "Analyze nodes for issues: NotReady, disk/memory/PID pressure, unschedulable, cordoned",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Specific node name"},
			},
		},
	}
}

func (n *NodeAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	nodeList, err := n.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	nodes := nodeList.Items
	if name, ok := input["name"].(string); ok && name != "" {
		var filtered []corev1.Node
		for _, node := range nodes {
			if strings.EqualFold(node.Name, name) {
				filtered = append(filtered, node)
			}
		}
		nodes = filtered
	}

	var findings []agent.Finding
	for _, node := range nodes {
		findings = append(findings, analyzeNode(node)...)
	}

	content := fmt.Sprintf("Analyzed %d nodes, found %d issues", len(nodes), len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

func analyzeNode(node corev1.Node) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("Node/%s", node.Name)

	// Check unschedulable (cordoned)
	if node.Spec.Unschedulable {
		findings = append(findings, agent.Finding{
			Severity:    "warning",
			Resource:    resource,
			Summary:     "Node is cordoned (unschedulable)",
			RawEvidence: fmt.Sprintf("Unschedulable: true"),
		})
	}

	// Check conditions
	for _, cond := range node.Status.Conditions {
		switch cond.Type {
		case corev1.NodeReady:
			if cond.Status != corev1.ConditionTrue {
				findings = append(findings, agent.Finding{
					Severity:    "critical",
					Resource:    resource,
					Summary:     fmt.Sprintf("Node is NotReady: %s", cond.Message),
					RawEvidence: fmt.Sprintf("Condition: Ready=%s, Reason: %s, Message: %s", cond.Status, cond.Reason, cond.Message),
				})
			}
		case corev1.NodeMemoryPressure:
			if cond.Status == corev1.ConditionTrue {
				findings = append(findings, agent.Finding{
					Severity:    "critical",
					Resource:    resource,
					Summary:     "Node has memory pressure",
					RawEvidence: fmt.Sprintf("Condition: MemoryPressure=True, Reason: %s, Message: %s", cond.Reason, cond.Message),
				})
			}
		case corev1.NodeDiskPressure:
			if cond.Status == corev1.ConditionTrue {
				findings = append(findings, agent.Finding{
					Severity:    "critical",
					Resource:    resource,
					Summary:     "Node has disk pressure",
					RawEvidence: fmt.Sprintf("Condition: DiskPressure=True, Reason: %s, Message: %s", cond.Reason, cond.Message),
				})
			}
		case corev1.NodePIDPressure:
			if cond.Status == corev1.ConditionTrue {
				findings = append(findings, agent.Finding{
					Severity:    "warning",
					Resource:    resource,
					Summary:     "Node has PID pressure",
					RawEvidence: fmt.Sprintf("Condition: PIDPressure=True, Reason: %s, Message: %s", cond.Reason, cond.Message),
				})
			}
		case corev1.NodeNetworkUnavailable:
			if cond.Status == corev1.ConditionTrue {
				findings = append(findings, agent.Finding{
					Severity:    "critical",
					Resource:    resource,
					Summary:     "Node network is unavailable",
					RawEvidence: fmt.Sprintf("Condition: NetworkUnavailable=True, Reason: %s, Message: %s", cond.Reason, cond.Message),
				})
			}
		}
	}

	return findings
}
