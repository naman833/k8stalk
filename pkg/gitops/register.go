package gitops

import (
	"fmt"
	"os"

	"github.com/naman833/k8stalk/pkg/agent"
	"github.com/naman833/k8stalk/pkg/gitops/argocd"
	"github.com/naman833/k8stalk/pkg/gitops/flux"
	"k8s.io/client-go/rest"
)

// RegisterTools conditionally registers GitOps tools based on environment:
// - ArgoCD: requires ARGOCD_SERVER and ARGOCD_AUTH_TOKEN env vars
// - Flux: requires a k8s rest.Config (always available if cluster is accessible)
//
// Tools that can't be initialized (missing env vars, no CRDs) are silently skipped.
func RegisterTools(registry *agent.Registry, restConfig *rest.Config) {
	registerArgoCD(registry)
	registerFlux(registry, restConfig)
}

func registerArgoCD(registry *agent.Registry) {
	if os.Getenv("ARGOCD_SERVER") == "" || os.Getenv("ARGOCD_AUTH_TOKEN") == "" {
		return
	}

	client, err := argocd.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: ArgoCD tools unavailable: %v\n", err)
		return
	}

	registry.Register(argocd.NewAppStatusTool(client))
	registry.Register(argocd.NewAppResourcesTool(client))
	registry.Register(argocd.NewAppListTool(client))
}

func registerFlux(registry *agent.Registry, restConfig *rest.Config) {
	if restConfig == nil {
		return
	}

	client, err := flux.NewClient(restConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: Flux tools unavailable: %v\n", err)
		return
	}

	registry.Register(flux.NewKustomizationStatusTool(client))
	registry.Register(flux.NewHelmReleaseStatusTool(client))
	registry.Register(flux.NewSourceStatusTool(client))
}
