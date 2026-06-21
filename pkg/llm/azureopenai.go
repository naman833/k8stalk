package llm

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/naman833/k8stalk/pkg/config"
)

// AzureOpenAIProvider wraps OpenAIProvider with Azure-specific auth and URL handling.
type AzureOpenAIProvider struct {
	OpenAIProvider
}

func (a *AzureOpenAIProvider) Name() string { return "azureopenai" }

// azureTransport replaces OpenAI's Bearer auth with Azure's api-key header.
type azureTransport struct {
	apiKey string
	base   http.RoundTripper
}

func (t *azureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Del("Authorization")
	req.Header.Set("api-key", t.apiKey)
	return t.base.RoundTrip(req)
}

// NewAzureOpenAIProviderFull creates an Azure OpenAI provider.
// base_url should be: https://<resource>.openai.azure.com/openai/deployments/<deployment>
func NewAzureOpenAIProviderFull(cfg config.ProviderConfig) (Provider, error) {
	apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
	if cfg.APIKeyEnv != "" {
		apiKey = os.Getenv(cfg.APIKeyEnv)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_API_KEY environment variable not set")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		return nil, fmt.Errorf("azure openai requires base_url (e.g. https://<resource>.openai.azure.com/openai/deployments/<deployment>)")
	}

	model := cfg.Model
	if model == "" {
		model = "gpt-5.4"
	}

	// Build the URL with API version
	apiVersion := "2024-06-01"
	chatURL := strings.TrimSuffix(baseURL, "/")
	if !strings.Contains(chatURL, "api-version=") {
		if strings.Contains(chatURL, "?") {
			chatURL += "&api-version=" + apiVersion
		} else {
			chatURL += "?api-version=" + apiVersion
		}
	}

	client := &http.Client{
		Transport: &azureTransport{
			apiKey: apiKey,
			base:   http.DefaultTransport,
		},
	}

	return &AzureOpenAIProvider{
		OpenAIProvider: OpenAIProvider{
			client:  client,
			baseURL: chatURL,
			apiKey:  apiKey,
			model:   model,
		},
	}, nil
}
