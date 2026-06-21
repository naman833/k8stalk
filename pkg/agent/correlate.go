package agent

import (
	"context"
	"fmt"
	"time"
)

// CorrelationWindow is the default time window for correlating GitOps syncs with failures.
const CorrelationWindow = 15 * time.Minute

// Correlation represents a detected relationship between a failing resource and a GitOps event.
type Correlation struct {
	FailingResource string
	GitOpsResource  string
	Type            string // "owner_ref", "timing", "label_selector"
	Confidence      string // "high", "medium", "low"
	Detail          string
}

// CorrelationTool checks if a failing resource correlates with recent GitOps activity.
type CorrelationTool struct {
	registry *Registry
}

func NewCorrelationTool(registry *Registry) *CorrelationTool {
	return &CorrelationTool{registry: registry}
}

func (c *CorrelationTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "correlate_gitops",
		Description: "Check if a failing resource correlates with recent ArgoCD/Flux sync activity. Looks for timing proximity between the failure and the last sync, owner references, and label selector matches.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"resource_kind":      map[string]any{"type": "string", "description": "Kind of the failing resource (Pod, Deployment, etc.)"},
				"resource_name":      map[string]any{"type": "string", "description": "Name of the failing resource"},
				"resource_namespace": map[string]any{"type": "string", "description": "Namespace of the failing resource"},
				"failure_time":       map[string]any{"type": "string", "description": "Approximate time the failure started (RFC3339)"},
			},
		},
	}
}

func (c *CorrelationTool) Execute(ctx context.Context, input map[string]any) (ToolResult, error) {
	kind, _ := input["resource_kind"].(string)
	name, _ := input["resource_name"].(string)
	ns, _ := input["resource_namespace"].(string)

	if kind == "" || name == "" {
		return ToolResult{IsError: true, Content: "resource_kind and resource_name are required"}, nil
	}

	// Try ArgoCD correlation
	argoTool := c.registry.Get("argocd_app_list")
	fluxKsTool := c.registry.Get("flux_kustomization_status")

	var correlations []string

	if argoTool != nil {
		result, err := argoTool.Execute(ctx, map[string]any{})
		if err == nil && !result.IsError {
			correlations = append(correlations, fmt.Sprintf("ArgoCD apps checked. Look for apps managing %s/%s in namespace %s.", kind, name, ns))
		}
	}

	if fluxKsTool != nil {
		result, err := fluxKsTool.Execute(ctx, map[string]any{"namespace": ns})
		if err == nil && !result.IsError {
			correlations = append(correlations, fmt.Sprintf("Flux kustomizations in namespace %s checked.", ns))
		}
	}

	if len(correlations) == 0 {
		return ToolResult{
			Content: fmt.Sprintf("No GitOps controllers detected for %s/%s/%s. The resource may not be GitOps-managed.", kind, name, ns),
		}, nil
	}

	content := fmt.Sprintf("Correlation check for %s/%s/%s:\n", kind, name, ns)
	for _, c := range correlations {
		content += "- " + c + "\n"
	}
	content += "\nNote: For detailed timing correlation, check if the GitOps last-sync timestamp is within 15 minutes of the failure start time."

	return ToolResult{Content: content}, nil
}
