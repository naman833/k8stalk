package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/naman833/k8stalk/pkg/config"
)

// OllamaProvider implements the Provider interface for Ollama.
type OllamaProvider struct {
	baseURL string
	model   string
	client  *http.Client
}

func NewOllamaProvider(cfg config.ProviderConfig) (Provider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	model := cfg.Model
	if model == "" {
		return nil, fmt.Errorf("no model configured for ollama backend — run 'k8stalk init' or set --model explicitly")
	}
	return &OllamaProvider{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{},
	}, nil
}

func (o *OllamaProvider) Name() string { return "ollama" }

func (o *OllamaProvider) SupportsTools() bool { return true }

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaTool struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ollamaChatResponse struct {
	Message    ollamaMessage `json:"message"`
	Done       bool          `json:"done"`
	DoneReason string        `json:"done_reason"`
}

type ollamaToolCall struct {
	Function ollamaToolCallFunction `json:"function"`
}

type ollamaToolCallFunction struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func (o *OllamaProvider) Chat(ctx context.Context, messages []Message, tools []ToolSpec) (*ChatResponse, error) {
	ollamaMessages := make([]ollamaMessage, 0, len(messages))
	for _, m := range messages {
		msg := ollamaMessage{
			Role:    string(m.Role),
			Content: m.Content,
		}
		// Include tool_calls for assistant messages that made tool calls
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, ollamaToolCall{
					Function: ollamaToolCallFunction{
						Name:      tc.Name,
						Arguments: tc.Input,
					},
				})
			}
		}
		ollamaMessages = append(ollamaMessages, msg)
	}

	reqBody := ollamaChatRequest{
		Model:    o.model,
		Messages: ollamaMessages,
		Stream:   false,
	}

	// Convert tools if available (newer Ollama models support tool calling)
	if len(tools) > 0 {
		for _, t := range tools {
			reqBody.Tools = append(reqBody.Tools, ollamaTool{
				Type: "function",
				Function: ollamaToolFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.InputSchema,
				},
			})
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var ollamaResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	chatResp := &ChatResponse{
		Content:    ollamaResp.Message.Content,
		StopReason: "end_turn",
	}

	// Parse native tool calls from message
	if len(ollamaResp.Message.ToolCalls) > 0 {
		chatResp.StopReason = "tool_use"
		for _, tc := range ollamaResp.Message.ToolCalls {
			chatResp.ToolCalls = append(chatResp.ToolCalls, ToolCall{
				ID:    fmt.Sprintf("call_%s_%d", tc.Function.Name, len(chatResp.ToolCalls)),
				Name:  tc.Function.Name,
				Input: tc.Function.Arguments,
			})
		}
	}

	return chatResp, nil
}

func (o *OllamaProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolSpec) (<-chan StreamChunk, error) {
	// When tools are provided, use non-streaming Chat and emit results as chunks
	// (Ollama tool calls come in the final non-streamed message)
	if len(tools) > 0 {
		resp, err := o.Chat(ctx, messages, tools)
		if err != nil {
			return nil, err
		}
		ch := make(chan StreamChunk, len(resp.ToolCalls)+2)
		go func() {
			defer close(ch)
			if resp.Content != "" {
				ch <- StreamChunk{TextDelta: resp.Content}
			}
			for _, tc := range resp.ToolCalls {
				tc := tc
				ch <- StreamChunk{ToolCall: &tc}
			}
			ch <- StreamChunk{Done: true}
		}()
		return ch, nil
	}

	ollamaMessages := make([]ollamaMessage, 0, len(messages))
	for _, m := range messages {
		ollamaMessages = append(ollamaMessages, ollamaMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	reqBody := ollamaChatRequest{
		Model:    o.model,
		Messages: ollamaMessages,
		Stream:   true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama stream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		decoder := json.NewDecoder(resp.Body)
		for {
			var chunk ollamaChatResponse
			if err := decoder.Decode(&chunk); err != nil {
				if err != io.EOF {
					fmt.Fprintf(os.Stderr, "ollama stream decode error: %v\n", err)
				}
				ch <- StreamChunk{Done: true}
				return
			}

			ch <- StreamChunk{
				TextDelta: chunk.Message.Content,
				Done:      chunk.Done,
			}

			if chunk.Done {
				return
			}
		}
	}()

	return ch, nil
}
