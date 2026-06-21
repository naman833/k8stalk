package agent

import "github.com/naman833/k8stalk/pkg/llm"

// Memory interface for session/message history.
type Memory interface {
	Load(sessionID string) []llm.Message
	Save(sessionID string, messages []llm.Message)
}

// InMemoryStore is a simple in-memory implementation for CLI use.
type InMemoryStore struct {
	sessions map[string][]llm.Message
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{sessions: make(map[string][]llm.Message)}
}

func (m *InMemoryStore) Load(sessionID string) []llm.Message {
	msgs, ok := m.sessions[sessionID]
	if !ok {
		return nil
	}
	// Return a copy
	result := make([]llm.Message, len(msgs))
	copy(result, msgs)
	return result
}

func (m *InMemoryStore) Save(sessionID string, messages []llm.Message) {
	stored := make([]llm.Message, len(messages))
	copy(stored, messages)
	m.sessions[sessionID] = stored
}
