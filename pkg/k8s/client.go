package k8s

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client wraps the Kubernetes client-go clientset with convenience methods.
type Client struct {
	Clientset  kubernetes.Interface
	RestConfig *rest.Config
	Context    string
}

// NewClient creates a new Kubernetes client. If context is empty, uses the current context.
func NewClient(context string) (*Client, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}

	if context != "" {
		configOverrides.CurrentContext = context
	}

	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Resolve the active context name
	rawConfig, err := kubeConfig.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get raw config: %w", err)
	}

	activeContext := rawConfig.CurrentContext
	if context != "" {
		activeContext = context
	}

	return &Client{
		Clientset:  clientset,
		RestConfig: config,
		Context:    activeContext,
	}, nil
}

// DefaultKubeconfigPath returns the default kubeconfig path.
func DefaultKubeconfigPath() string {
	if env := os.Getenv("KUBECONFIG"); env != "" {
		return env
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kube", "config")
}
