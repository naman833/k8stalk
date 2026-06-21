package llm

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/naman833/k8stalk/pkg/config"
)

// CustomRESTProvider implements Provider for any OpenAI-compatible API endpoint.
// Works with vLLM, LM Studio, LocalAI, llama.cpp server, text-generation-webui, etc.
type CustomRESTProvider struct {
	OpenAIProvider
	supportsNativeTools bool
}

func NewCustomRESTProvider(cfg config.ProviderConfig) (Provider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		return nil, fmt.Errorf("customrest requires base_url (e.g. http://localhost:8000/v1)")
	}

	model := cfg.Model
	if model == "" {
		model = "default"
	}

	// Optional API key
	apiKey := ""
	if cfg.APIKeyEnv != "" {
		apiKey = os.Getenv(cfg.APIKeyEnv)
	}

	return &CustomRESTProvider{
		OpenAIProvider: OpenAIProvider{
			client:  &http.Client{},
			baseURL: baseURL,
			apiKey:  apiKey,
			model:   model,
		},
		supportsNativeTools: true, // assume yes; will fall back to prompted if it fails
	}, nil
}

func (c *CustomRESTProvider) Name() string { return "customrest" }

func (c *CustomRESTProvider) SupportsTools() bool { return c.supportsNativeTools }

func (c *CustomRESTProvider) Chat(ctx context.Context, messages []Message, tools []ToolSpec) (*ChatResponse, error) {
	// Try with native tools first
	resp, err := c.OpenAIProvider.Chat(ctx, messages, tools)
	if err != nil {
		// If native tools fail, try without tools (caller can use prompted fallback)
		if len(tools) > 0 {
			c.supportsNativeTools = false
			return c.OpenAIProvider.Chat(ctx, messages, nil)
		}
		return nil, err
	}
	return resp, nil
}

func (c *CustomRESTProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolSpec) (<-chan StreamChunk, error) {
	if !c.supportsNativeTools {
		return c.OpenAIProvider.ChatStream(ctx, messages, nil)
	}
	return c.OpenAIProvider.ChatStream(ctx, messages, tools)
}
