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

// OpenAI API types

type openaiRequest struct {
	Model       string              `json:"model"`
	Messages    []openaiMessage     `json:"messages"`
	Tools       []openaiTool        `json:"tools,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Temperature *float64            `json:"temperature,omitempty"`
}

type openaiMessage struct {
	Role       string              `json:"role"`
	Content    string              `json:"content,omitempty"`
	ToolCalls  []openaiToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
}

type openaiTool struct {
	Type     string             `json:"type"`
	Function openaiToolFunction `json:"function"`
}

type openaiToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openaiToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openaiToolCallFunction `json:"function"`
}

type openaiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiResponse struct {
	Choices []openaiChoice `json:"choices"`
}

type openaiChoice struct {
	Message      openaiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
	Delta        openaiMessage `json:"delta"`
}

type openaiStreamChunk struct {
	Choices []openaiChoice `json:"choices"`
}

// OpenAIProvider implements Provider for OpenAI's API.
type OpenAIProvider struct {
	client  *http.Client
	baseURL string
	apiKey  string
	model   string
}

func NewOpenAIProvider(cfg config.ProviderConfig) (Provider, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if cfg.APIKeyEnv != "" {
		apiKey = os.Getenv(cfg.APIKeyEnv)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
	}

	model := cfg.Model
	if model == "" {
		model = "gpt-5.4"
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	return &OpenAIProvider{
		client:  &http.Client{},
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
	}, nil
}

func (o *OpenAIProvider) Name() string         { return "openai" }
func (o *OpenAIProvider) SupportsTools() bool   { return true }

func (o *OpenAIProvider) Chat(ctx context.Context, messages []Message, tools []ToolSpec) (*ChatResponse, error) {
	req := o.buildRequest(messages, tools, false)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var oaiResp openaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("openai returned no choices")
	}

	choice := oaiResp.Choices[0]
	result := &ChatResponse{
		Content: choice.Message.Content,
	}

	switch choice.FinishReason {
	case "tool_calls":
		result.StopReason = "tool_use"
	case "stop":
		result.StopReason = "end_turn"
	case "length":
		result.StopReason = "max_tokens"
	default:
		result.StopReason = choice.FinishReason
	}

	for _, tc := range choice.Message.ToolCalls {
		var input map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
			input = map[string]any{"_raw": tc.Function.Arguments}
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	return result, nil
}

func (o *OpenAIProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolSpec) (<-chan StreamChunk, error) {
	req := o.buildRequest(messages, tools, true)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai stream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("openai returned status %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		o.processSSEStream(resp.Body, ch)
	}()

	return ch, nil
}

func (o *OpenAIProvider) buildRequest(messages []Message, tools []ToolSpec, stream bool) openaiRequest {
	oaiMessages := make([]openaiMessage, 0, len(messages))
	for _, m := range messages {
		msg := openaiMessage{
			Role:    string(m.Role),
			Content: m.Content,
		}
		if m.Role == RoleTool {
			msg.ToolCallID = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Input)
				msg.ToolCalls = append(msg.ToolCalls, openaiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openaiToolCallFunction{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				})
			}
		}
		oaiMessages = append(oaiMessages, msg)
	}

	req := openaiRequest{
		Model:     o.model,
		Messages:  oaiMessages,
		Stream:    stream,
		MaxTokens: 4096,
	}

	if len(tools) > 0 {
		for _, t := range tools {
			req.Tools = append(req.Tools, openaiTool{
				Type: "function",
				Function: openaiToolFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.InputSchema,
				},
			})
		}
	}

	return req
}

func (o *OpenAIProvider) processSSEStream(body io.Reader, ch chan<- StreamChunk) {
	scanner := bufio.NewScanner(body)
	// Track partial tool calls being assembled across chunks
	toolCalls := make(map[int]*openaiToolCall)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line == "data: [DONE]" {
			if line == "data: [DONE]" {
				// Emit any completed tool calls
				for _, tc := range toolCalls {
					var input map[string]any
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
						input = map[string]any{"_raw": tc.Function.Arguments}
					}
					ch <- StreamChunk{
						ToolCall: &ToolCall{
							ID:    tc.ID,
							Name:  tc.Function.Name,
							Input: input,
						},
					}
				}
				ch <- StreamChunk{Done: true}
				return
			}
			continue
		}

		if len(line) < 6 || line[:6] != "data: " {
			continue
		}
		data := line[6:]

		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			ch <- StreamChunk{TextDelta: delta.Content}
		}

		// Accumulate tool call deltas
		for i, tc := range delta.ToolCalls {
			existing, ok := toolCalls[i]
			if !ok {
				id := tc.ID
				if id == "" {
					id = uuid.New().String()
				}
				toolCalls[i] = &openaiToolCall{
					ID:   id,
					Type: "function",
					Function: openaiToolCallFunction{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			} else {
				if tc.Function.Name != "" {
					existing.Function.Name = tc.Function.Name
				}
				existing.Function.Arguments += tc.Function.Arguments
			}
		}
	}

	// If we exit without [DONE], still emit what we have
	for _, tc := range toolCalls {
		var input map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
			input = map[string]any{"_raw": tc.Function.Arguments}
		}
		ch <- StreamChunk{
			ToolCall: &ToolCall{
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			},
		}
	}
	ch <- StreamChunk{Done: true}
}
