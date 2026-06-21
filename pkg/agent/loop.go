package agent

import (
	"context"
	"fmt"

	"github.com/naman833/k8stalk/pkg/llm"
	"github.com/naman833/k8stalk/pkg/sanitize"
)

const maxTurns = 8

// ErrMaxTurnsExceeded is returned when the agent loop hits the safety cap.
var ErrMaxTurnsExceeded = fmt.Errorf("agent reached maximum turns (%d) without completing", maxTurns)

// Agent orchestrates the agentic reasoning loop.
type Agent struct {
	provider  llm.Provider
	registry  *Registry
	sanitizer *sanitize.Sanitizer
	memory    Memory
}

// NewAgent creates a new Agent instance.
func NewAgent(provider llm.Provider, registry *Registry, sanitizer *sanitize.Sanitizer, memory Memory) *Agent {
	return &Agent{
		provider:  provider,
		registry:  registry,
		sanitizer: sanitizer,
		memory:    memory,
	}
}

// Run executes the agentic reasoning loop for a user message.
// It returns a channel that streams chunks of the response.
func (a *Agent) Run(ctx context.Context, sessionID string, userMessage string) (<-chan llm.StreamChunk, error) {
	history := a.memory.Load(sessionID)
	if len(history) == 0 {
		history = append(history, llm.Message{
			Role:    llm.RoleSystem,
			Content: SystemPrompt,
		})
	}
	history = append(history, llm.Message{Role: llm.RoleUser, Content: userMessage})

	ch := make(chan llm.StreamChunk, 64)

	go func() {
		defer close(ch)
		a.runLoop(ctx, sessionID, history, ch)
	}()

	return ch, nil
}

// RunSync executes the agentic loop and returns the final response content.
func (a *Agent) RunSync(ctx context.Context, sessionID string, userMessage string) (string, []Finding, error) {
	history := a.memory.Load(sessionID)
	if len(history) == 0 {
		history = append(history, llm.Message{
			Role:    llm.RoleSystem,
			Content: SystemPrompt,
		})
	}
	history = append(history, llm.Message{Role: llm.RoleUser, Content: userMessage})

	var allFindings []Finding

	for turn := 0; turn < maxTurns; turn++ {
		resp, err := a.provider.Chat(ctx, history, a.registry.Specs())
		if err != nil {
			return "", nil, fmt.Errorf("LLM chat failed (turn %d): %w", turn, err)
		}

		if resp.StopReason != "tool_use" || len(resp.ToolCalls) == 0 {
			// Final response
			content := a.sanitizer.Desanitize(resp.Content)
			history = append(history, llm.Message{Role: llm.RoleAssistant, Content: resp.Content})
			a.memory.Save(sessionID, history)
			return content, allFindings, nil
		}

		// Process tool calls
		history = append(history, llm.Message{Role: llm.RoleAssistant, ToolCalls: resp.ToolCalls})

		for _, call := range resp.ToolCalls {
			tool := a.registry.Get(call.Name)
			if tool == nil {
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: call.ID,
					Content:    fmt.Sprintf("error: unknown tool %q", call.Name),
				})
				continue
			}

			result, err := tool.Execute(ctx, call.Input)
			if err != nil {
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: call.ID,
					Content:    fmt.Sprintf("error: %v", err),
				})
				continue
			}

			allFindings = append(allFindings, result.Findings...)
			sanitized := a.sanitizer.Sanitize(result.Content)
			history = append(history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: call.ID,
				Content:    sanitized,
			})
		}
	}

	return "", allFindings, ErrMaxTurnsExceeded
}

func (a *Agent) runLoop(ctx context.Context, sessionID string, history []llm.Message, ch chan<- llm.StreamChunk) {
	for turn := 0; turn < maxTurns; turn++ {
		resp, err := a.provider.Chat(ctx, history, a.registry.Specs())
		if err != nil {
			ch <- llm.StreamChunk{TextDelta: fmt.Sprintf("\nError: %v", err), Done: true}
			return
		}

		if resp.StopReason != "tool_use" || len(resp.ToolCalls) == 0 {
			content := a.sanitizer.Desanitize(resp.Content)
			ch <- llm.StreamChunk{TextDelta: content, Done: true}
			history = append(history, llm.Message{Role: llm.RoleAssistant, Content: resp.Content})
			a.memory.Save(sessionID, history)
			return
		}

		// Notify about tool calls
		history = append(history, llm.Message{Role: llm.RoleAssistant, ToolCalls: resp.ToolCalls})

		for _, call := range resp.ToolCalls {
			ch <- llm.StreamChunk{ToolCall: &llm.ToolCall{ID: call.ID, Name: call.Name, Input: call.Input}}

			tool := a.registry.Get(call.Name)
			if tool == nil {
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: call.ID,
					Content:    fmt.Sprintf("error: unknown tool %q", call.Name),
				})
				continue
			}

			result, err := tool.Execute(ctx, call.Input)
			if err != nil {
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: call.ID,
					Content:    fmt.Sprintf("error: %v", err),
				})
				continue
			}

			sanitized := a.sanitizer.Sanitize(result.Content)
			history = append(history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: call.ID,
				Content:    sanitized,
			})
		}
	}

	ch <- llm.StreamChunk{TextDelta: "\nReached maximum reasoning steps.", Done: true}
}
