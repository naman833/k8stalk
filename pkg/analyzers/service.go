package analyzers

import (
	"context"
	"fmt"

	"github.com/naman833/k8stalk/pkg/agent"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type ServiceAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewServiceAnalyzer(clientset kubernetes.Interface, namespace string) *ServiceAnalyzer {
	return &ServiceAnalyzer{clientset: clientset, namespace: namespace}
}

func (s *ServiceAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "Service",
		Description: "Analyze services for issues: no endpoints, selector mismatches, LoadBalancer pending",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
				"name":      map[string]any{"type": "string", "description": "Specific service name"},
			},
		},
	}
}

func (s *ServiceAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns := s.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	svcList, err := s.clientset.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	var findings []agent.Finding
	for _, svc := range svcList.Items {
		findings = append(findings, s.analyzeService(ctx, svc)...)
	}

	content := fmt.Sprintf("Analyzed %d services, found %d issues", len(svcList.Items), len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

func (s *ServiceAnalyzer) analyzeService(ctx context.Context, svc corev1.Service) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("Service/%s/%s", svc.Name, svc.Namespace)

	// Skip headless services and ExternalName services
	if svc.Spec.ClusterIP == "None" || svc.Spec.Type == corev1.ServiceTypeExternalName {
		return findings
	}

	// Check for endpoints
	if len(svc.Spec.Selector) > 0 {
		endpoints, err := s.clientset.CoreV1().Endpoints(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
		if err == nil {
			totalAddresses := 0
			for _, subset := range endpoints.Subsets {
				totalAddresses += len(subset.Addresses)
			}
			if totalAddresses == 0 {
				findings = append(findings, agent.Finding{
					Severity:    "critical",
					Resource:    resource,
					Summary:     "Service has no active endpoints",
					RawEvidence: fmt.Sprintf("Type: %s, Selector: %v, Endpoints: 0", svc.Spec.Type, svc.Spec.Selector),
				})
			}
		}
	}

	// Check LoadBalancer pending
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		if len(svc.Status.LoadBalancer.Ingress) == 0 {
			findings = append(findings, agent.Finding{
				Severity:    "warning",
				Resource:    resource,
				Summary:     "LoadBalancer service has no external IP assigned",
				RawEvidence: fmt.Sprintf("Type: LoadBalancer, Ingress: pending"),
			})
		}
	}

	return findings
}
