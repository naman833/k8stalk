package gitops

import (
	"context"
	"time"
)

// SyncStatus represents the sync state of a GitOps resource.
type SyncStatus struct {
	Status     string    // "Synced", "OutOfSync", "Unknown"
	Health     string    // "Healthy", "Degraded", "Progressing", "Missing", "Unknown"
	Message    string
	Revision   string
	SyncedAt   time.Time
}

// GitOpsResource represents a resource managed by a GitOps controller.
type GitOpsResource struct {
	Kind      string
	Name      string
	Namespace string
	Status    SyncStatus
}

// Adapter is the common interface for GitOps controllers (ArgoCD, Flux).
type Adapter interface {
	Name() string
	Available(ctx context.Context) bool
	ListApplications(ctx context.Context, namespace string) ([]GitOpsResource, error)
	GetApplication(ctx context.Context, name, namespace string) (*GitOpsResource, error)
	GetResourcesForApp(ctx context.Context, name, namespace string) ([]ManagedResource, error)
}

// ManagedResource is a Kubernetes resource managed by a GitOps application.
type ManagedResource struct {
	Kind      string
	Name      string
	Namespace string
	Status    string
	Health    string
	SyncedAt  time.Time
}
