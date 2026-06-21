package argocd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client communicates with the ArgoCD API server using token-based auth.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a new ArgoCD API client.
// It reads ARGOCD_SERVER and ARGOCD_AUTH_TOKEN from environment.
func NewClient() (*Client, error) {
	server := os.Getenv("ARGOCD_SERVER")
	if server == "" {
		return nil, fmt.Errorf("ARGOCD_SERVER environment variable not set")
	}
	token := os.Getenv("ARGOCD_AUTH_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("ARGOCD_AUTH_TOKEN environment variable not set")
	}

	return &Client{
		baseURL: server,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// NewClientWithConfig creates a client with explicit parameters.
func NewClientWithConfig(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Application represents an ArgoCD Application.
type Application struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Project string `json:"project"`
		Source  struct {
			RepoURL        string `json:"repoURL"`
			Path           string `json:"path"`
			TargetRevision string `json:"targetRevision"`
		} `json:"source"`
		Destination struct {
			Server    string `json:"server"`
			Namespace string `json:"namespace"`
		} `json:"destination"`
	} `json:"spec"`
	Status struct {
		Sync struct {
			Status   string `json:"status"`
			Revision string `json:"revision"`
		} `json:"sync"`
		Health struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"health"`
		OperationState *struct {
			FinishedAt string `json:"finishedAt"`
			Phase      string `json:"phase"`
			Message    string `json:"message"`
		} `json:"operationState"`
		Resources []ResourceStatus `json:"resources"`
	} `json:"status"`
}

// ResourceStatus represents the status of a resource managed by an Application.
type ResourceStatus struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Health    *struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"health"`
}

// ListApplications lists all ArgoCD applications.
func (c *Client) ListApplications(ctx context.Context) ([]Application, error) {
	var result struct {
		Items []Application `json:"items"`
	}
	if err := c.get(ctx, "/api/v1/applications", &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

// GetApplication gets a specific application by name.
func (c *Client) GetApplication(ctx context.Context, name string) (*Application, error) {
	var app Application
	if err := c.get(ctx, fmt.Sprintf("/api/v1/applications/%s", name), &app); err != nil {
		return nil, err
	}
	return &app, nil
}

// GetApplicationDiff gets the diff (managed resources) for an application.
func (c *Client) GetApplicationDiff(ctx context.Context, name string) ([]ResourceStatus, error) {
	app, err := c.GetApplication(ctx, name)
	if err != nil {
		return nil, err
	}
	return app.Status.Resources, nil
}

func (c *Client) get(ctx context.Context, path string, result any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("argocd request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("argocd returned status %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}
