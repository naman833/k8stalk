package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/naman833/k8stalk/pkg/llm"
)

// Session represents a chat session.
type Session struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	ClusterContext string    `json:"cluster_context,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// StoredMessage represents a persisted message.
type StoredMessage struct {
	ID         int64     `json:"id"`
	SessionID  string    `json:"session_id"`
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	ToolCalls  string    `json:"tool_calls_json,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Repository provides CRUD operations for sessions and messages.
type Repository struct {
	db *sql.DB
}

// NewRepository creates a new Repository.
func NewRepository(db *DB) *Repository {
	return &Repository{db: db.Conn()}
}

// CreateSession creates a new session.
func (r *Repository) CreateSession(id, title, context string) error {
	_, err := r.db.Exec(
		"INSERT INTO sessions (id, title, cluster_context) VALUES (?, ?, ?)",
		id, title, context,
	)
	return err
}

// GetSession retrieves a session by ID.
func (r *Repository) GetSession(id string) (*Session, error) {
	var s Session
	err := r.db.QueryRow(
		"SELECT id, title, cluster_context, created_at, updated_at FROM sessions WHERE id = ?", id,
	).Scan(&s.ID, &s.Title, &s.ClusterContext, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListSessions returns all sessions ordered by most recently updated.
func (r *Repository) ListSessions() ([]Session, error) {
	rows, err := r.db.Query("SELECT id, title, cluster_context, created_at, updated_at FROM sessions ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Title, &s.ClusterContext, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// DeleteSession deletes a session and all its messages (CASCADE).
func (r *Repository) DeleteSession(id string) error {
	_, err := r.db.Exec("DELETE FROM sessions WHERE id = ?", id)
	return err
}

// DeleteAllSessions removes all sessions and messages.
func (r *Repository) DeleteAllSessions() error {
	_, err := r.db.Exec("DELETE FROM sessions")
	return err
}

// UpdateSessionTitle updates the title of a session.
func (r *Repository) UpdateSessionTitle(id, title string) error {
	_, err := r.db.Exec("UPDATE sessions SET title = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", title, id)
	return err
}

// AddMessage inserts a message into a session.
func (r *Repository) AddMessage(sessionID string, msg llm.Message) error {
	var toolCallsJSON string
	if len(msg.ToolCalls) > 0 {
		data, _ := json.Marshal(msg.ToolCalls)
		toolCallsJSON = string(data)
	}

	_, err := r.db.Exec(
		"INSERT INTO messages (session_id, role, content, tool_calls_json, tool_call_id) VALUES (?, ?, ?, ?, ?)",
		sessionID, string(msg.Role), msg.Content, toolCallsJSON, msg.ToolCallID,
	)
	if err != nil {
		return err
	}

	// Touch session updated_at
	_, _ = r.db.Exec("UPDATE sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", sessionID)
	return nil
}

// GetMessages retrieves all messages for a session.
func (r *Repository) GetMessages(sessionID string) ([]llm.Message, error) {
	rows, err := r.db.Query(
		"SELECT role, content, tool_calls_json, tool_call_id FROM messages WHERE session_id = ? ORDER BY id ASC",
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []llm.Message
	for rows.Next() {
		var role, content string
		var toolCallsJSON, toolCallID sql.NullString
		if err := rows.Scan(&role, &content, &toolCallsJSON, &toolCallID); err != nil {
			return nil, err
		}

		msg := llm.Message{
			Role:    llm.Role(role),
			Content: content,
		}

		if toolCallsJSON.Valid && toolCallsJSON.String != "" {
			var calls []llm.ToolCall
			if err := json.Unmarshal([]byte(toolCallsJSON.String), &calls); err == nil {
				msg.ToolCalls = calls
			}
		}
		if toolCallID.Valid {
			msg.ToolCallID = toolCallID.String
		}

		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// SessionMessageCount returns the number of messages in a session.
func (r *Repository) SessionMessageCount(sessionID string) (int, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ?", sessionID).Scan(&count)
	return count, err
}

// SQLiteMemory adapts the Repository to the agent.Memory interface.
type SQLiteMemory struct {
	repo *Repository
}

// NewSQLiteMemory creates a Memory backed by SQLite.
func NewSQLiteMemory(repo *Repository) *SQLiteMemory {
	return &SQLiteMemory{repo: repo}
}

// Load retrieves messages for a session.
func (m *SQLiteMemory) Load(sessionID string) []llm.Message {
	msgs, err := m.repo.GetMessages(sessionID)
	if err != nil {
		return nil
	}
	return msgs
}

// Save persists a message history for a session.
// It only saves the last message (incremental save).
func (m *SQLiteMemory) Save(sessionID string, messages []llm.Message) {
	if len(messages) == 0 {
		return
	}

	// Ensure session exists
	_, err := m.repo.GetSession(sessionID)
	if err != nil {
		_ = m.repo.CreateSession(sessionID, "New chat", "")
	}

	// Get existing count and only save new messages
	count, _ := m.repo.SessionMessageCount(sessionID)
	for i := count; i < len(messages); i++ {
		_ = m.repo.AddMessage(sessionID, messages[i])
	}

	// Auto-title from first user message
	if count == 0 {
		for _, msg := range messages {
			if msg.Role == llm.RoleUser && msg.Content != "" {
				title := msg.Content
				if len(title) > 60 {
					title = title[:60] + "..."
				}
				_ = m.repo.UpdateSessionTitle(sessionID, title)
				break
			}
		}
	}
}

// Ensure interfaces are satisfied
var _ fmt.Stringer = (*Session)(nil)

func (s *Session) String() string {
	return fmt.Sprintf("Session{id=%s, title=%s}", s.ID, s.Title)
}
