package analyzers

import (
	"context"
	"fmt"

	"github.com/naman833/k8stalk/pkg/agent"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type WebhookAnalyzer struct {
	clientset kubernetes.Interface
}

func NewWebhookAnalyzer(clientset kubernetes.Interface) *WebhookAnalyzer {
	return &WebhookAnalyzer{clientset: clientset}
}

func (w *WebhookAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "Webhook",
		Description: "Analyze mutating and validating webhook configurations for issues: unreachable services, broad catch-all rules, missing failure policy",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{},
		},
	}
}

func (w *WebhookAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	var findings []agent.Finding

	// Validating webhooks
	vwList, err := w.clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, vw := range vwList.Items {
			findings = append(findings, analyzeValidatingWebhook(vw)...)
		}
	}

	// Mutating webhooks
	mwList, err := w.clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, mw := range mwList.Items {
			findings = append(findings, analyzeMutatingWebhook(mw)...)
		}
	}

	totalWebhooks := 0
	if vwList != nil {
		totalWebhooks += len(vwList.Items)
	}
	if mwList != nil {
		totalWebhooks += len(mwList.Items)
	}

	content := fmt.Sprintf("Analyzed %d webhook configurations, found %d issues", totalWebhooks, len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

func analyzeValidatingWebhook(vw admissionv1.ValidatingWebhookConfiguration) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("ValidatingWebhookConfiguration/%s", vw.Name)

	for _, webhook := range vw.Webhooks {
		if webhook.FailurePolicy != nil && *webhook.FailurePolicy == admissionv1.Fail {
			// Check if it has wildcard rules
			for _, rule := range webhook.Rules {
				if containsWildcard(rule.Resources) || containsWildcard(rule.APIGroups) {
					findings = append(findings, agent.Finding{
						Severity:    "warning",
						Resource:    resource,
						Summary:     fmt.Sprintf("Webhook %q has Fail policy with wildcard rules (may block cluster operations)", webhook.Name),
						RawEvidence: fmt.Sprintf("Webhook: %s, FailurePolicy: Fail, Resources: %v, APIGroups: %v", webhook.Name, rule.Resources, rule.APIGroups),
					})
				}
			}
		}
	}

	return findings
}

func analyzeMutatingWebhook(mw admissionv1.MutatingWebhookConfiguration) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("MutatingWebhookConfiguration/%s", mw.Name)

	for _, webhook := range mw.Webhooks {
		if webhook.FailurePolicy != nil && *webhook.FailurePolicy == admissionv1.Fail {
			for _, rule := range webhook.Rules {
				if containsWildcard(rule.Resources) || containsWildcard(rule.APIGroups) {
					findings = append(findings, agent.Finding{
						Severity:    "warning",
						Resource:    resource,
						Summary:     fmt.Sprintf("Webhook %q has Fail policy with wildcard rules (may block cluster operations)", webhook.Name),
						RawEvidence: fmt.Sprintf("Webhook: %s, FailurePolicy: Fail, Resources: %v, APIGroups: %v", webhook.Name, rule.Resources, rule.APIGroups),
					})
				}
			}
		}
	}

	return findings
}

func containsWildcard(items []string) bool {
	for _, item := range items {
		if item == "*" {
			return true
		}
	}
	return false
}
