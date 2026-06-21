package analyzers

import (
	"context"
	"fmt"

	"github.com/naman833/k8stalk/pkg/agent"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type IngressAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewIngressAnalyzer(clientset kubernetes.Interface, namespace string) *IngressAnalyzer {
	return &IngressAnalyzer{clientset: clientset, namespace: namespace}
}

func (i *IngressAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "Ingress",
		Description: "Analyze ingresses for issues: missing backend services, TLS secret missing, no address assigned",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
				"name":      map[string]any{"type": "string", "description": "Specific ingress name"},
			},
		},
	}
}

func (i *IngressAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns := i.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	ingList, err := i.clientset.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	var findings []agent.Finding
	for _, ing := range ingList.Items {
		findings = append(findings, i.analyzeIngress(ctx, ing)...)
	}

	content := fmt.Sprintf("Analyzed %d ingresses, found %d issues", len(ingList.Items), len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

func (i *IngressAnalyzer) analyzeIngress(ctx context.Context, ing networkingv1.Ingress) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("Ingress/%s/%s", ing.Name, ing.Namespace)

	// Check TLS secrets exist
	for _, tls := range ing.Spec.TLS {
		if tls.SecretName != "" {
			_, err := i.clientset.CoreV1().Secrets(ing.Namespace).Get(ctx, tls.SecretName, metav1.GetOptions{})
			if err != nil {
				findings = append(findings, agent.Finding{
					Severity:    "critical",
					Resource:    resource,
					Summary:     fmt.Sprintf("TLS secret %q not found", tls.SecretName),
					RawEvidence: fmt.Sprintf("TLS hosts: %v, SecretName: %s, Error: %v", tls.Hosts, tls.SecretName, err),
				})
			}
		}
	}

	// Check backend services exist
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				_, err := i.clientset.CoreV1().Services(ing.Namespace).Get(ctx, path.Backend.Service.Name, metav1.GetOptions{})
				if err != nil {
					findings = append(findings, agent.Finding{
						Severity:    "critical",
						Resource:    resource,
						Summary:     fmt.Sprintf("Backend service %q not found", path.Backend.Service.Name),
						RawEvidence: fmt.Sprintf("Host: %s, Path: %s, Service: %s", rule.Host, path.Path, path.Backend.Service.Name),
					})
				}
			}
		}
	}

	// No address assigned
	if len(ing.Status.LoadBalancer.Ingress) == 0 {
		findings = append(findings, agent.Finding{
			Severity:    "warning",
			Resource:    resource,
			Summary:     "Ingress has no address assigned",
			RawEvidence: fmt.Sprintf("IngressClass: %v, Rules: %d", ing.Spec.IngressClassName, len(ing.Spec.Rules)),
		})
	}

	return findings
}
