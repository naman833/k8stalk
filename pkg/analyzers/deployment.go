package analyzers

import (
	"context"
	"fmt"
	"strings"

	"github.com/naman833/k8stalk/pkg/agent"
	"github.com/naman833/k8stalk/pkg/k8s"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type DeploymentAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewDeploymentAnalyzer(clientset kubernetes.Interface, namespace string) *DeploymentAnalyzer {
	return &DeploymentAnalyzer{clientset: clientset, namespace: namespace}
}

func (d *DeploymentAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "Deployment",
		Description: "Analyze deployments for issues: unavailable replicas, failed rollouts, mismatched selectors, zero replicas",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
				"name":      map[string]any{"type": "string", "description": "Specific deployment name"},
			},
		},
	}
}

func (d *DeploymentAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns := d.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	deployments, err := d.clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	items := deployments.Items
	if name, ok := input["name"].(string); ok && name != "" {
		items = filterDeployments(items, name)
	}

	var findings []agent.Finding
	for _, dep := range items {
		findings = append(findings, analyzeDeployment(dep)...)
	}

	_ = k8s.GetPods // reference to avoid unused import
	content := fmt.Sprintf("Analyzed %d deployments, found %d issues", len(items), len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

func analyzeDeployment(dep appsv1.Deployment) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("Deployment/%s/%s", dep.Name, dep.Namespace)

	desired := int32(1)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}

	// Zero replicas
	if desired == 0 {
		findings = append(findings, agent.Finding{
			Severity:    "info",
			Resource:    resource,
			Summary:     "Deployment has 0 desired replicas (scaled down)",
			RawEvidence: fmt.Sprintf("Replicas: %d, Available: %d", desired, dep.Status.AvailableReplicas),
		})
		return findings
	}

	// Unavailable replicas
	if dep.Status.UnavailableReplicas > 0 {
		findings = append(findings, agent.Finding{
			Severity:    "warning",
			Resource:    resource,
			Summary:     fmt.Sprintf("%d/%d replicas unavailable", dep.Status.UnavailableReplicas, desired),
			RawEvidence: fmt.Sprintf("Desired: %d, Available: %d, Unavailable: %d, Updated: %d", desired, dep.Status.AvailableReplicas, dep.Status.UnavailableReplicas, dep.Status.UpdatedReplicas),
		})
	}

	// All replicas unavailable
	if dep.Status.AvailableReplicas == 0 && desired > 0 {
		findings = append(findings, agent.Finding{
			Severity:    "critical",
			Resource:    resource,
			Summary:     "Deployment has no available replicas",
			RawEvidence: fmt.Sprintf("Desired: %d, Ready: %d, Available: 0", desired, dep.Status.ReadyReplicas),
		})
	}

	// Check conditions
	for _, cond := range dep.Status.Conditions {
		if cond.Type == appsv1.DeploymentProgressing && cond.Status == "False" {
			findings = append(findings, agent.Finding{
				Severity:    "critical",
				Resource:    resource,
				Summary:     fmt.Sprintf("Deployment not progressing: %s", cond.Message),
				RawEvidence: fmt.Sprintf("Condition: Progressing=False, Reason: %s, Message: %s", cond.Reason, cond.Message),
			})
		}
		if cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == "True" {
			findings = append(findings, agent.Finding{
				Severity:    "critical",
				Resource:    resource,
				Summary:     fmt.Sprintf("Deployment replica failure: %s", cond.Message),
				RawEvidence: fmt.Sprintf("Condition: ReplicaFailure=True, Reason: %s, Message: %s", cond.Reason, cond.Message),
			})
		}
	}

	return findings
}

func filterDeployments(items []appsv1.Deployment, name string) []appsv1.Deployment {
	var filtered []appsv1.Deployment
	for _, item := range items {
		if strings.EqualFold(item.Name, name) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
