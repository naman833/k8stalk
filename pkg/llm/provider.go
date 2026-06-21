package llm

import (
	"context"
)

// Role represents the role of a message in a conversation.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleSystem    Role = "system"
)

// ToolSpec describes a tool's capabilities to the LLM for tool selection.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// Message represents a single message in a conversation.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a tool invocation requested by the LLM.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ChatResponse is the complete response from a non-streaming chat call.
type ChatResponse struct {
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	StopReason string     `json:"stop_reason"` // "end_turn", "tool_use", "max_tokens"
}

// StreamChunk is a single chunk from a streaming chat response.
type StreamChunk struct {
	TextDelta string    `json:"text_delta,omitempty"`
	ToolCall  *ToolCall `json:"tool_call,omitempty"`
	Done      bool      `json:"done"`
}

// Provider is the interface all LLM backends must implement.
type Provider interface {
	Name() string
	SupportsTools() bool
	Chat(ctx context.Context, messages []Message, tools []ToolSpec) (*ChatResponse, error)
	ChatStream(ctx context.Context, messages []Message, tools []ToolSpec) (<-chan StreamChunk, error)
}
