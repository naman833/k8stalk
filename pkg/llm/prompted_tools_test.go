package llm

import (
	"testing"
)

func TestExtractToolCallJSON_InlineJSON(t *testing.T) {
	text := `I'll check the pods now.
{"tool": "check_pods", "input": {"namespace": "default"}}
`
	tc, remaining := extractToolCallJSON(text)
	if tc == nil {
		t.Fatal("expected tool call, got nil")
	}
	if tc.Name != "check_pods" {
		t.Fatalf("expected name check_pods, got %s", tc.Name)
	}
	if tc.Input["namespace"] != "default" {
		t.Fatalf("expected namespace=default, got %v", tc.Input["namespace"])
	}
	if remaining == text {
		t.Fatal("expected remaining to differ from input")
	}
}

func TestExtractToolCallJSON_CodeBlock(t *testing.T) {
	text := "Let me investigate:\n```json\n{\"tool\": \"get_events\", \"input\": {\"namespace\": \"kube-system\"}}\n```\n"
	tc, _ := extractToolCallJSON(text)
	if tc == nil {
		t.Fatal("expected tool call, got nil")
	}
	if tc.Name != "get_events" {
		t.Fatalf("expected name get_events, got %s", tc.Name)
	}
}

func TestExtractToolCallJSON_NoToolCall(t *testing.T) {
	text := "The pod is crashlooping because of an OOM error. You should increase the memory limit."
	tc, remaining := extractToolCallJSON(text)
	if tc != nil {
		t.Fatalf("expected no tool call, got %+v", tc)
	}
	if remaining != text {
		t.Fatal("expected remaining to equal input when no tool call found")
	}
}

func TestExtractToolCallJSON_AlternateFields(t *testing.T) {
	text := `{"name": "check_nodes", "arguments": {"node_name": "worker-1"}}`
	tc, _ := extractToolCallJSON(text)
	if tc == nil {
		t.Fatal("expected tool call, got nil")
	}
	if tc.Name != "check_nodes" {
		t.Fatalf("expected name check_nodes, got %s", tc.Name)
	}
	if tc.Input["node_name"] != "worker-1" {
		t.Fatalf("expected node_name=worker-1, got %v", tc.Input["node_name"])
	}
}

func TestBuildToolPromptBlock(t *testing.T) {
	tools := []ToolSpec{
		{
			Name:        "get_pods",
			Description: "List pods in a namespace",
			InputSchema: map[string]any{"namespace": map[string]any{"type": "string"}},
		},
	}
	result := buildToolPromptBlock(tools)
	if result == "" {
		t.Fatal("expected non-empty prompt block")
	}
	if !contains(result, "get_pods") {
		t.Fatal("expected prompt to contain tool name")
	}
	if !contains(result, "List pods in a namespace") {
		t.Fatal("expected prompt to contain description")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
