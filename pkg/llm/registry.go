package llm

import (
	"fmt"

	"github.com/naman833/k8stalk/pkg/config"
)

// ProviderConstructor creates a Provider from config.
type ProviderConstructor func(cfg config.ProviderConfig) (Provider, error)

var registry = map[string]ProviderConstructor{
	"anthropic":     NewAnthropicProvider,
	"ollama":        NewOllamaProvider,
	"openai":        NewOpenAIProvider,
	"azureopenai":   NewAzureOpenAIProviderFull,
	"google":        NewGoogleProvider,
	"vertexai":      NewVertexAIProvider,
	"amazonbedrock": NewBedrockProvider,
	"customrest":    NewCustomRESTProvider,
}

// RegisterProvider adds a new provider constructor.
func RegisterProvider(name string, constructor ProviderConstructor) {
	registry[name] = constructor
}

// NewProvider creates a provider by backend name using the given config.
// If modelOverride is non-empty, it takes precedence over the model in config.
func NewProvider(backendName string, cfg *config.Config, modelOverride ...string) (Provider, error) {
	var providerCfg config.ProviderConfig
	found := false
	for _, p := range cfg.Providers {
		if p.Backend == backendName {
			providerCfg = p
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("backend %q not configured; run 'k8stalk auth add --backend %s'", backendName, backendName)
	}

	if len(modelOverride) > 0 && modelOverride[0] != "" {
		providerCfg.Model = modelOverride[0]
	}

	constructor, ok := registry[backendName]
	if !ok {
		return nil, fmt.Errorf("unknown backend %q; supported: anthropic, ollama, openai, azureopenai, google, vertexai, amazonbedrock, customrest", backendName)
	}

	return constructor(providerCfg)
}

// WrapWithPromptedTools wraps a provider with the prompted-tools fallback
// if it doesn't support native tool calling.
func WrapWithPromptedTools(p Provider) Provider {
	if p.SupportsTools() {
		return p
	}
	return NewPromptedToolsProvider(p)
}
