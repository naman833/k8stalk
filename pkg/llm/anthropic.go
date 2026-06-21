package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/naman833/k8stalk/pkg/config"
)

// AnthropicProvider implements the Provider interface for Anthropic's Claude API.
type AnthropicProvider struct {
	client *anthropic.Client
	model  string
}

func NewAnthropicProvider(cfg config.ProviderConfig) (Provider, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if cfg.APIKeyEnv != "" {
		apiKey = os.Getenv(cfg.APIKeyEnv)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
	}

	model := cfg.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	return &AnthropicProvider{
		client: &client,
		model:  model,
	}, nil
}

func (a *AnthropicProvider) Name() string { return "anthropic" }

func (a *AnthropicProvider) SupportsTools() bool { return true }

func (a *AnthropicProvider) Chat(ctx context.Context, messages []Message, tools []ToolSpec) (*ChatResponse, error) {
	// Convert messages to Anthropic format
	var anthropicMessages []anthropic.MessageParam
	var systemBlocks []anthropic.TextBlockParam

	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{
				Text: m.Content,
			})
		case RoleUser:
			anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(m.Content),
			))
		case RoleAssistant:
			if len(m.ToolCalls) > 0 {
				var blocks []anthropic.ContentBlockParamUnion
				if m.Content != "" {
					blocks = append(blocks, anthropic.NewTextBlock(m.Content))
				}
				for _, tc := range m.ToolCalls {
					blocks = append(blocks, anthropic.ContentBlockParamOfRequestToolUseBlock(tc.ID, tc.Input, tc.Name))
				}
				anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(blocks...))
			} else {
				anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(
					anthropic.NewTextBlock(m.Content),
				))
			}
		case RoleTool:
			anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false),
			))
		}
	}

	// Build request params
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: 4096,
		Messages:  anthropicMessages,
	}

	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}

	// Convert tools to Anthropic format
	if len(tools) > 0 {
		var anthropicTools []anthropic.ToolUnionParam
		for _, t := range tools {
			anthropicTools = append(anthropicTools, anthropic.ToolUnionParam{
				OfTool: &anthropic.ToolParam{
					Name:        t.Name,
					Description: param.NewOpt(t.Description),
					InputSchema: anthropic.ToolInputSchemaParam{
						Properties: t.InputSchema,
					},
				},
			})
		}
		params.Tools = anthropicTools
	}

	resp, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic chat failed: %w", err)
	}

	// Parse response
	result := &ChatResponse{
		StopReason: string(resp.StopReason),
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "tool_use":
			var inputMap map[string]any
			if err := json.Unmarshal(block.Input, &inputMap); err != nil {
				inputMap = make(map[string]any)
			}
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: inputMap,
			})
		}
	}

	return result, nil
}

func (a *AnthropicProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolSpec) (<-chan StreamChunk, error) {
	// For Phase 0, stream uses non-streaming internally and emits the full response
	resp, err := a.Chat(ctx, messages, tools)
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk, 2)
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
