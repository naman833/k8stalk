package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// PromptedToolsProvider wraps any Provider that lacks native tool-calling support
// and injects tool definitions into the system prompt, then parses JSON tool-call
// blocks from the model's text output.
//
// This enables the agent loop to work with models like gemma2, phi3, and other
// small/local models that don't support OpenAI-style function calling.
type PromptedToolsProvider struct {
	inner Provider
}

// NewPromptedToolsProvider wraps a provider with text-based tool-call parsing.
func NewPromptedToolsProvider(inner Provider) *PromptedToolsProvider {
	return &PromptedToolsProvider{inner: inner}
}

func (p *PromptedToolsProvider) Name() string       { return p.inner.Name() + "+prompted" }
func (p *PromptedToolsProvider) SupportsTools() bool { return true }

func (p *PromptedToolsProvider) Chat(ctx context.Context, messages []Message, tools []ToolSpec) (*ChatResponse, error) {
	// Inject tool definitions into the system prompt
	augmented := p.injectToolPrompt(messages, tools)

	// Call the inner provider without native tools
	resp, err := p.inner.Chat(ctx, augmented, nil)
	if err != nil {
		return nil, err
	}

	// Parse tool calls from the response text
	return p.parseToolCalls(resp), nil
}

func (p *PromptedToolsProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolSpec) (<-chan StreamChunk, error) {
	// For prompted tools, we can't truly stream since we need to parse the full response
	// to detect tool calls. Use non-streaming and emit as chunks.
	resp, err := p.Chat(ctx, messages, tools)
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk, 4)
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

func (p *PromptedToolsProvider) injectToolPrompt(messages []Message, tools []ToolSpec) []Message {
	if len(tools) == 0 {
		return messages
	}

	toolDefs := buildToolPromptBlock(tools)

	// Find or create the system message and append tool definitions
	result := make([]Message, 0, len(messages)+1)
	systemFound := false

	for _, m := range messages {
		if m.Role == RoleSystem {
			result = append(result, Message{
				Role:    RoleSystem,
				Content: m.Content + "\n\n" + toolDefs,
			})
			systemFound = true
		} else if m.Role == RoleTool {
			// Convert tool results to user messages since the model doesn't understand tool role
			result = append(result, Message{
				Role:    RoleUser,
				Content: fmt.Sprintf("[Tool Result for %s]:\n%s", m.ToolCallID, m.Content),
			})
		} else {
			result = append(result, m)
		}
	}

	if !systemFound {
		// Prepend a system message with tool definitions
		result = append([]Message{{
			Role:    RoleSystem,
			Content: toolDefs,
		}}, result...)
	}

	return result
}

func buildToolPromptBlock(tools []ToolSpec) string {
	var sb strings.Builder
	sb.WriteString("# Available Tools\n\n")
	sb.WriteString("You have access to the following tools. To use a tool, respond with EXACTLY this JSON format on its own line:\n\n")
	sb.WriteString("```\n{\"tool\": \"tool_name\", \"input\": {\"param1\": \"value1\"}}\n```\n\n")
	sb.WriteString("You may call ONE tool per response. After receiving the tool result, you can call another tool or provide your final answer.\n")
	sb.WriteString("When you are done and want to give a final text answer, do NOT include any tool JSON.\n\n")
	sb.WriteString("## Tool Definitions\n\n")

	for _, t := range tools {
		sb.WriteString(fmt.Sprintf("### %s\n", t.Name))
		sb.WriteString(fmt.Sprintf("%s\n", t.Description))
		if len(t.InputSchema) > 0 {
			schemaJSON, _ := json.MarshalIndent(t.InputSchema, "", "  ")
			sb.WriteString(fmt.Sprintf("Parameters:\n```json\n%s\n```\n", string(schemaJSON)))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (p *PromptedToolsProvider) parseToolCalls(resp *ChatResponse) *ChatResponse {
	if resp.Content == "" {
		return resp
	}

	// Try to find a JSON tool call in the response
	content := resp.Content
	toolCall, remaining := extractToolCallJSON(content)

	if toolCall != nil {
		return &ChatResponse{
			Content:    strings.TrimSpace(remaining),
			ToolCalls:  []ToolCall{*toolCall},
			StopReason: "tool_use",
		}
	}

	return resp
}

// extractToolCallJSON looks for a JSON object matching {"tool": "...", "input": {...}}
// in the text. It handles both inline JSON and fenced code blocks.
func extractToolCallJSON(text string) (*ToolCall, string) {
	// Try to find JSON in code blocks first
	if idx := strings.Index(text, "```json"); idx != -1 {
		end := strings.Index(text[idx+7:], "```")
		if end != -1 {
			jsonStr := strings.TrimSpace(text[idx+7 : idx+7+end])
			if tc := tryParseToolCall(jsonStr); tc != nil {
				remaining := text[:idx] + text[idx+7+end+3:]
				return tc, remaining
			}
		}
	}
	if idx := strings.Index(text, "```"); idx != -1 {
		end := strings.Index(text[idx+3:], "```")
		if end != -1 {
			jsonStr := strings.TrimSpace(text[idx+3 : idx+3+end])
			if tc := tryParseToolCall(jsonStr); tc != nil {
				remaining := text[:idx] + text[idx+3+end+3:]
				return tc, remaining
			}
		}
	}

	// Try to find raw JSON objects in the text
	for i := 0; i < len(text); i++ {
		if text[i] == '{' {
			// Find matching closing brace
			depth := 0
			for j := i; j < len(text); j++ {
				switch text[j] {
				case '{':
					depth++
				case '}':
					depth--
					if depth == 0 {
						jsonStr := text[i : j+1]
						if tc := tryParseToolCall(jsonStr); tc != nil {
							remaining := text[:i] + text[j+1:]
							return tc, remaining
						}
						break
					}
				}
			}
		}
	}

	return nil, text
}

func tryParseToolCall(jsonStr string) *ToolCall {
	var obj map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		return nil
	}

	toolName, ok := obj["tool"].(string)
	if !ok {
		// Also try "name" field
		toolName, ok = obj["name"].(string)
		if !ok {
			return nil
		}
	}

	input := make(map[string]any)
	if inputObj, ok := obj["input"].(map[string]any); ok {
		input = inputObj
	} else if argsObj, ok := obj["arguments"].(map[string]any); ok {
		input = argsObj
	} else if paramsObj, ok := obj["parameters"].(map[string]any); ok {
		input = paramsObj
	}

	return &ToolCall{
		ID:    uuid.New().String(),
		Name:  toolName,
		Input: input,
	}
}
