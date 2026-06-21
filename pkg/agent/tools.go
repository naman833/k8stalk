package agent

import (
	"context"

	"github.com/naman833/k8stalk/pkg/llm"
)

// ToolSpec is an alias for llm.ToolSpec for convenience.
type ToolSpec = llm.ToolSpec

// Finding represents a single diagnostic finding from an analyzer.
type Finding struct {
	Severity    string `json:"severity"`    // "critical", "warning", "info"
	Resource    string `json:"resource"`    // e.g. "Pod/checkout-7d9f/default"
	Summary     string `json:"summary"`
	RawEvidence string `json:"raw_evidence"`
	Explanation string `json:"explanation,omitempty"` // populated by LLM explain
}

// ToolResult is returned by a Tool's Execute method.
type ToolResult struct {
	Content  string    `json:"content"`
	IsError  bool      `json:"is_error"`
	Findings []Finding `json:"findings,omitempty"`
}

// Tool is the single interface implemented by all analyzers, GitOps adapters,
// and diagnostic actions. Both `analyze` (fixed sweep) and `chat`/`diagnose`
// (agentic) modes consume the same Tool implementations.
type Tool interface {
	Spec() ToolSpec
	Execute(ctx context.Context, input map[string]any) (ToolResult, error)
}
