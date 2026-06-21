package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/naman833/k8stalk/pkg/config"
)

// VertexAIProvider implements Provider for Google Vertex AI.
// Uses Application Default Credentials via `gcloud auth print-access-token`.
type VertexAIProvider struct {
	GoogleProvider
	project  string
	location string
	tokenMu  sync.Mutex
	token    string
	tokenExp time.Time
}

func NewVertexAIProvider(cfg config.ProviderConfig) (Provider, error) {
	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if project == "" {
		project = os.Getenv("GCLOUD_PROJECT")
	}
	if project == "" {
		return nil, fmt.Errorf("GOOGLE_CLOUD_PROJECT environment variable not set")
	}

	location := cfg.Region
	if location == "" {
		location = "us-central1"
	}

	model := cfg.Model
	if model == "" {
		model = "gemini-2.5-flash"
	}

	baseURL := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google",
		location, project, location)

	return &VertexAIProvider{
		GoogleProvider: GoogleProvider{
			client:  &http.Client{},
			apiKey:  "", // not used; uses OAuth token
			model:   model,
			baseURL: baseURL,
		},
		project:  project,
		location: location,
	}, nil
}

func (v *VertexAIProvider) Name() string { return "vertexai" }

func (v *VertexAIProvider) getAccessToken() (string, error) {
	v.tokenMu.Lock()
	defer v.tokenMu.Unlock()

	if v.token != "" && time.Now().Before(v.tokenExp) {
		return v.token, nil
	}

	// Try gcloud CLI first
	cmd := exec.Command("gcloud", "auth", "print-access-token")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get access token via gcloud: %w (ensure gcloud is installed and authenticated)", err)
	}

	v.token = strings.TrimSpace(string(out))
	v.tokenExp = time.Now().Add(50 * time.Minute) // tokens last ~60min, refresh at 50
	return v.token, nil
}

func (v *VertexAIProvider) Chat(ctx context.Context, messages []Message, tools []ToolSpec) (*ChatResponse, error) {
	token, err := v.getAccessToken()
	if err != nil {
		return nil, err
	}

	req := v.buildRequest(messages, tools)
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent", v.baseURL, v.model)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := v.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("vertex ai request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vertex ai returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var gemResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&gemResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(gemResp.Candidates) == 0 {
		return nil, fmt.Errorf("vertex ai returned no candidates")
	}

	candidate := gemResp.Candidates[0]
	result := &ChatResponse{}

	switch candidate.FinishReason {
	case "STOP":
		result.StopReason = "end_turn"
	case "MAX_TOKENS":
		result.StopReason = "max_tokens"
	default:
		result.StopReason = "end_turn"
	}

	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			result.Content += part.Text
		}
		if part.FunctionCall != nil {
			result.StopReason = "tool_use"
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:    fmt.Sprintf("call_%d", time.Now().UnixNano()),
				Name:  part.FunctionCall.Name,
				Input: part.FunctionCall.Args,
			})
		}
	}

	return result, nil
}

func (v *VertexAIProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolSpec) (<-chan StreamChunk, error) {
	token, err := v.getAccessToken()
	if err != nil {
		return nil, err
	}

	req := v.buildRequest(messages, tools)
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", v.baseURL, v.model)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := v.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("vertex ai stream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("vertex ai returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Reuse GoogleProvider's streaming logic with the authenticated connection
	ch := make(chan StreamChunk, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		// Vertex AI streams as JSON array or SSE depending on alt=sse
		decoder := json.NewDecoder(resp.Body)
		for {
			var chunk geminiResponse
			if err := decoder.Decode(&chunk); err != nil {
				if err != io.EOF {
					// Try line-by-line SSE parsing as fallback
					break
				}
				break
			}

			if len(chunk.Candidates) == 0 {
				continue
			}

			for _, part := range chunk.Candidates[0].Content.Parts {
				if part.Text != "" {
					ch <- StreamChunk{TextDelta: part.Text}
				}
				if part.FunctionCall != nil {
					ch <- StreamChunk{
						ToolCall: &ToolCall{
							ID:    fmt.Sprintf("call_%d", time.Now().UnixNano()),
							Name:  part.FunctionCall.Name,
							Input: part.FunctionCall.Args,
						},
					}
				}
			}
		}

		ch <- StreamChunk{Done: true}
	}()

	return ch, nil
}
