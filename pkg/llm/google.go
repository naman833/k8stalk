package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/naman833/k8stalk/pkg/config"
)

// GoogleProvider implements Provider for Google's Gemini API (AI Studio).
type GoogleProvider struct {
	client  *http.Client
	apiKey  string
	model   string
	baseURL string
}

func NewGoogleProvider(cfg config.ProviderConfig) (Provider, error) {
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if cfg.APIKeyEnv != "" {
		apiKey = os.Getenv(cfg.APIKeyEnv)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("GOOGLE_API_KEY environment variable not set")
	}

	model := cfg.Model
	if model == "" {
		model = "gemini-2.5-flash"
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}

	return &GoogleProvider{
		client:  &http.Client{},
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
	}, nil
}

func (g *GoogleProvider) Name() string       { return "google" }
func (g *GoogleProvider) SupportsTools() bool { return true }

// Gemini API types
type geminiRequest struct {
	Contents         []geminiContent         `json:"contents"`
	Tools            []geminiToolDeclaration `json:"tools,omitempty"`
	SystemInstruction *geminiContent         `json:"systemInstruction,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type geminiFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiToolDeclaration struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}

type geminiFunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

func (g *GoogleProvider) Chat(ctx context.Context, messages []Message, tools []ToolSpec) (*ChatResponse, error) {
	req := g.buildRequest(messages, tools)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", g.baseURL, g.model, g.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("google request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("google returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var gemResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&gemResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(gemResp.Candidates) == 0 {
		return nil, fmt.Errorf("google returned no candidates")
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
				ID:    uuid.New().String(),
				Name:  part.FunctionCall.Name,
				Input: part.FunctionCall.Args,
			})
		}
	}

	return result, nil
}

func (g *GoogleProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolSpec) (<-chan StreamChunk, error) {
	req := g.buildRequest(messages, tools)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?key=%s&alt=sse", g.baseURL, g.model, g.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("google stream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("google returned status %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" || len(line) < 6 || line[:6] != "data: " {
				continue
			}
			data := line[6:]

			var chunk geminiResponse
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
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
							ID:    uuid.New().String(),
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

func (g *GoogleProvider) buildRequest(messages []Message, tools []ToolSpec) geminiRequest {
	req := geminiRequest{}

	// Convert tools
	if len(tools) > 0 {
		decls := make([]geminiFunctionDeclaration, 0, len(tools))
		for _, t := range tools {
			params := map[string]any{"type": "object", "properties": t.InputSchema}
			decls = append(decls, geminiFunctionDeclaration{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			})
		}
		req.Tools = []geminiToolDeclaration{{FunctionDeclarations: decls}}
	}

	// Convert messages
	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			req.SystemInstruction = &geminiContent{
				Parts: []geminiPart{{Text: m.Content}},
			}
		case RoleUser:
			req.Contents = append(req.Contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{{Text: m.Content}},
			})
		case RoleAssistant:
			parts := []geminiPart{}
			if m.Content != "" {
				parts = append(parts, geminiPart{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				parts = append(parts, geminiPart{
					FunctionCall: &geminiFunctionCall{
						Name: tc.Name,
						Args: tc.Input,
					},
				})
			}
			req.Contents = append(req.Contents, geminiContent{
				Role:  "model",
				Parts: parts,
			})
		case RoleTool:
			// Find the tool name from the ToolCallID by looking at previous messages
			toolName := "unknown"
			for i := len(req.Contents) - 1; i >= 0; i-- {
				for _, p := range req.Contents[i].Parts {
					if p.FunctionCall != nil {
						toolName = p.FunctionCall.Name
						break
					}
				}
				if toolName != "unknown" {
					break
				}
			}
			req.Contents = append(req.Contents, geminiContent{
				Role: "user",
				Parts: []geminiPart{{
					FunctionResponse: &geminiFunctionResponse{
						Name:     toolName,
						Response: map[string]any{"result": m.Content},
					},
				}},
			})
		}
	}

	return req
}
