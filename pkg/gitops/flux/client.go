package flux

import (
	"context"
	"fmt"
	"strings"

	"github.com/naman833/k8stalk/pkg/agent"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// Client reads Flux CRDs using a dynamic client. No separate API server needed.
type Client struct {
	dynamic dynamic.Interface
}

// NewClient creates a Flux client from a rest.Config.
func NewClient(config *rest.Config) (*Client, error) {
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}
	return &Client{dynamic: dynClient}, nil
}

var (
	kustomizationGVR = schema.GroupVersionResource{
		Group:    "kustomize.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "kustomizations",
	}
	helmReleaseGVR = schema.GroupVersionResource{
		Group:    "helm.toolkit.fluxcd.io",
		Version:  "v2",
		Resource: "helmreleases",
	}
	gitRepoGVR = schema.GroupVersionResource{
		Group:    "source.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "gitrepositories",
	}
	ociRepoGVR = schema.GroupVersionResource{
		Group:    "source.toolkit.fluxcd.io",
		Version:  "v1beta2",
		Resource: "ocirepositories",
	}
)

// FluxResource represents a Flux custom resource with its status.
type FluxResource struct {
	Kind       string
	Name       string
	Namespace  string
	Ready      bool
	Message    string
	Revision   string
	Suspended  bool
}

func (c *Client) ListKustomizations(ctx context.Context, namespace string) ([]FluxResource, error) {
	return c.listResources(ctx, kustomizationGVR, namespace, "Kustomization")
}

func (c *Client) ListHelmReleases(ctx context.Context, namespace string) ([]FluxResource, error) {
	return c.listResources(ctx, helmReleaseGVR, namespace, "HelmRelease")
}

func (c *Client) ListGitRepositories(ctx context.Context, namespace string) ([]FluxResource, error) {
	return c.listResources(ctx, gitRepoGVR, namespace, "GitRepository")
}

func (c *Client) listResources(ctx context.Context, gvr schema.GroupVersionResource, namespace, kind string) ([]FluxResource, error) {
	var list *unstructured.UnstructuredList
	var err error

	if namespace != "" {
		list, err = c.dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	} else {
		list, err = c.dynamic.Resource(gvr).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", kind, err)
	}

	var resources []FluxResource
	for _, item := range list.Items {
		resources = append(resources, parseFluxResource(item, kind))
	}
	return resources, nil
}

func parseFluxResource(obj unstructured.Unstructured, kind string) FluxResource {
	fr := FluxResource{
		Kind:      kind,
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}

	// Check suspended
	suspended, _, _ := unstructured.NestedBool(obj.Object, "spec", "suspend")
	fr.Suspended = suspended

	// Parse status conditions
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _ := cond["type"].(string)
		if condType == "Ready" {
			condStatus, _ := cond["status"].(string)
			fr.Ready = condStatus == "True"
			fr.Message, _ = cond["message"].(string)
			break
		}
	}

	// Parse revision from status
	revision, _, _ := unstructured.NestedString(obj.Object, "status", "lastAppliedRevision")
	if revision == "" {
		revision, _, _ = unstructured.NestedString(obj.Object, "status", "artifact", "revision")
	}
	fr.Revision = revision

	return fr
}

// KustomizationStatusTool exposes flux_kustomization_status.
type KustomizationStatusTool struct {
	client *Client
}

func NewKustomizationStatusTool(client *Client) *KustomizationStatusTool {
	return &KustomizationStatusTool{client: client}
}

func (t *KustomizationStatusTool) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "flux_kustomization_status",
		Description: "Get the status of Flux Kustomizations. Shows ready state, last applied revision, and any error messages.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":      map[string]any{"type": "string", "description": "Specific Kustomization name (optional, lists all if empty)"},
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
			},
		},
	}
}

func (t *KustomizationStatusTool) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns, _ := input["namespace"].(string)
	name, _ := input["name"].(string)

	resources, err := t.client.ListKustomizations(ctx, ns)
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, nil
	}

	if name != "" {
		var filtered []FluxResource
		for _, r := range resources {
			if r.Name == name {
				filtered = append(filtered, r)
			}
		}
		resources = filtered
	}

	return formatFluxResult("Kustomization", resources), nil
}

// HelmReleaseStatusTool exposes flux_helmrelease_status.
type HelmReleaseStatusTool struct {
	client *Client
}

func NewHelmReleaseStatusTool(client *Client) *HelmReleaseStatusTool {
	return &HelmReleaseStatusTool{client: client}
}

func (t *HelmReleaseStatusTool) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "flux_helmrelease_status",
		Description: "Get the status of Flux HelmReleases. Shows ready state, chart version, last applied revision, and any error messages.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":      map[string]any{"type": "string", "description": "Specific HelmRelease name (optional)"},
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
			},
		},
	}
}

func (t *HelmReleaseStatusTool) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns, _ := input["namespace"].(string)
	name, _ := input["name"].(string)

	resources, err := t.client.ListHelmReleases(ctx, ns)
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, nil
	}

	if name != "" {
		var filtered []FluxResource
		for _, r := range resources {
			if r.Name == name {
				filtered = append(filtered, r)
			}
		}
		resources = filtered
	}

	return formatFluxResult("HelmRelease", resources), nil
}

// SourceStatusTool exposes flux_source_status.
type SourceStatusTool struct {
	client *Client
}

func NewSourceStatusTool(client *Client) *SourceStatusTool {
	return &SourceStatusTool{client: client}
}

func (t *SourceStatusTool) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "flux_source_status",
		Description: "Get the status of Flux source objects (GitRepository, OCIRepository). Shows ready state, latest revision, and any fetch errors.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"kind":      map[string]any{"type": "string", "description": "Source kind: GitRepository or OCIRepository"},
				"name":      map[string]any{"type": "string", "description": "Specific source name (optional)"},
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
			},
		},
	}
}

func (t *SourceStatusTool) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns, _ := input["namespace"].(string)
	name, _ := input["name"].(string)
	kind, _ := input["kind"].(string)

	var resources []FluxResource
	var err error

	switch kind {
	case "OCIRepository":
		resources, err = t.client.listResources(ctx, ociRepoGVR, ns, "OCIRepository")
	default:
		resources, err = t.client.ListGitRepositories(ctx, ns)
	}

	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, nil
	}

	if name != "" {
		var filtered []FluxResource
		for _, r := range resources {
			if r.Name == name {
				filtered = append(filtered, r)
			}
		}
		resources = filtered
	}

	return formatFluxResult(kind, resources), nil
}

func formatFluxResult(kind string, resources []FluxResource) agent.ToolResult {
	var findings []agent.Finding
	var lines []string

	for _, r := range resources {
		status := "Ready"
		if !r.Ready {
			status = "NotReady"
		}
		if r.Suspended {
			status = "Suspended"
		}

		line := fmt.Sprintf("%s/%s (%s) status=%s rev=%s", r.Kind, r.Name, r.Namespace, status, r.Revision)
		if !r.Ready && r.Message != "" {
			line += fmt.Sprintf(" msg=%s", r.Message)
		}
		lines = append(lines, line)

		if !r.Ready && !r.Suspended {
			findings = append(findings, agent.Finding{
				Severity:    "warning",
				Resource:    fmt.Sprintf("Flux/%s/%s/%s", r.Kind, r.Name, r.Namespace),
				Summary:     fmt.Sprintf("%s is not ready: %s", r.Kind, r.Message),
				RawEvidence: fmt.Sprintf("Kind: %s, Name: %s, Ready: false, Message: %s", r.Kind, r.Name, r.Message),
			})
		}
	}

	content := fmt.Sprintf("Flux %s (%d):\n%s", kind, len(resources), strings.Join(lines, "\n"))
	return agent.ToolResult{Content: content, Findings: findings}
}
