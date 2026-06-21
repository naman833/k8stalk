package argocd

import (
	"context"
	"fmt"
	"strings"

	"github.com/naman833/k8stalk/pkg/agent"
)

// AppStatusTool exposes argocd_app_status to the agent.
type AppStatusTool struct {
	client *Client
}

func NewAppStatusTool(client *Client) *AppStatusTool {
	return &AppStatusTool{client: client}
}

func (t *AppStatusTool) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "argocd_app_status",
		Description: "Get the sync and health status of an ArgoCD Application. Returns sync status (Synced/OutOfSync), health (Healthy/Degraded/Progressing), last sync revision, and any error messages.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"app_name": map[string]any{"type": "string", "description": "Name of the ArgoCD Application"},
			},
			"required": []string{"app_name"},
		},
	}
}

func (t *AppStatusTool) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	name, _ := input["app_name"].(string)
	if name == "" {
		return agent.ToolResult{IsError: true, Content: "app_name is required"}, nil
	}

	app, err := t.client.GetApplication(ctx, name)
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, nil
	}

	var findings []agent.Finding
	resource := fmt.Sprintf("ArgoCD/Application/%s", name)

	// Check sync status
	if app.Status.Sync.Status == "OutOfSync" {
		findings = append(findings, agent.Finding{
			Severity:    "warning",
			Resource:    resource,
			Summary:     "Application is OutOfSync",
			RawEvidence: fmt.Sprintf("SyncStatus: OutOfSync, Revision: %s", app.Status.Sync.Revision),
		})
	}

	// Check health
	switch app.Status.Health.Status {
	case "Degraded":
		findings = append(findings, agent.Finding{
			Severity:    "critical",
			Resource:    resource,
			Summary:     fmt.Sprintf("Application health is Degraded: %s", app.Status.Health.Message),
			RawEvidence: fmt.Sprintf("Health: Degraded, Message: %s", app.Status.Health.Message),
		})
	case "Missing":
		findings = append(findings, agent.Finding{
			Severity:    "critical",
			Resource:    resource,
			Summary:     "Application has missing resources",
			RawEvidence: fmt.Sprintf("Health: Missing"),
		})
	}

	// Check operation state
	if app.Status.OperationState != nil && app.Status.OperationState.Phase == "Failed" {
		findings = append(findings, agent.Finding{
			Severity:    "critical",
			Resource:    resource,
			Summary:     fmt.Sprintf("Last sync operation failed: %s", app.Status.OperationState.Message),
			RawEvidence: fmt.Sprintf("Phase: Failed, Message: %s, FinishedAt: %s", app.Status.OperationState.Message, app.Status.OperationState.FinishedAt),
		})
	}

	content := fmt.Sprintf("Application: %s\nSync: %s (rev: %s)\nHealth: %s\nProject: %s\nRepo: %s",
		name, app.Status.Sync.Status, app.Status.Sync.Revision,
		app.Status.Health.Status, app.Spec.Project, app.Spec.Source.RepoURL)

	return agent.ToolResult{Content: content, Findings: findings}, nil
}

// AppResourcesTool exposes argocd_app_resources to the agent.
type AppResourcesTool struct {
	client *Client
}

func NewAppResourcesTool(client *Client) *AppResourcesTool {
	return &AppResourcesTool{client: client}
}

func (t *AppResourcesTool) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "argocd_app_resources",
		Description: "List all Kubernetes resources managed by an ArgoCD Application, with their sync and health status. Useful for finding which resources are degraded or out of sync.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"app_name": map[string]any{"type": "string", "description": "Name of the ArgoCD Application"},
			},
			"required": []string{"app_name"},
		},
	}
}

func (t *AppResourcesTool) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	name, _ := input["app_name"].(string)
	if name == "" {
		return agent.ToolResult{IsError: true, Content: "app_name is required"}, nil
	}

	resources, err := t.client.GetApplicationDiff(ctx, name)
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, nil
	}

	var findings []agent.Finding
	var lines []string
	for _, r := range resources {
		health := "Unknown"
		if r.Health != nil {
			health = r.Health.Status
		}
		line := fmt.Sprintf("%s/%s (%s) sync=%s health=%s", r.Kind, r.Name, r.Namespace, r.Status, health)
		lines = append(lines, line)

		if r.Status == "OutOfSync" || (r.Health != nil && r.Health.Status == "Degraded") {
			findings = append(findings, agent.Finding{
				Severity: "warning",
				Resource: fmt.Sprintf("%s/%s/%s", r.Kind, r.Name, r.Namespace),
				Summary:  fmt.Sprintf("Resource sync=%s health=%s in app %s", r.Status, health, name),
			})
		}
	}

	content := fmt.Sprintf("Application %s manages %d resources:\n%s", name, len(resources), strings.Join(lines, "\n"))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

// AppListTool lists all ArgoCD Applications.
type AppListTool struct {
	client *Client
}

func NewAppListTool(client *Client) *AppListTool {
	return &AppListTool{client: client}
}

func (t *AppListTool) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "argocd_app_list",
		Description: "List all ArgoCD Applications with their sync and health status. Use this to get an overview of all GitOps-managed applications.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *AppListTool) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	apps, err := t.client.ListApplications(ctx)
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, nil
	}

	var findings []agent.Finding
	var lines []string
	for _, app := range apps {
		line := fmt.Sprintf("%s sync=%s health=%s", app.Metadata.Name, app.Status.Sync.Status, app.Status.Health.Status)
		lines = append(lines, line)

		if app.Status.Health.Status == "Degraded" || app.Status.Sync.Status == "OutOfSync" {
			findings = append(findings, agent.Finding{
				Severity: "warning",
				Resource: fmt.Sprintf("ArgoCD/Application/%s", app.Metadata.Name),
				Summary:  fmt.Sprintf("sync=%s health=%s", app.Status.Sync.Status, app.Status.Health.Status),
			})
		}
	}

	content := fmt.Sprintf("ArgoCD Applications (%d):\n%s", len(apps), strings.Join(lines, "\n"))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}
