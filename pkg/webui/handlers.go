package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/naman833/k8stalk/pkg/llm"
	"github.com/naman833/k8stalk/pkg/store"
)

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.repo.ListSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []store.Session{}
	}
	writeJSON(w, sessions)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	id := uuid.New().String()
	if err := s.repo.CreateSession(id, "New chat", ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	session, _ := s.repo.GetSession(id)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, session)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	session, err := s.repo.GetSession(id)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	writeJSON(w, session)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.DeleteSession(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteAllSessions(w http.ResponseWriter, r *http.Request) {
	if err := s.repo.DeleteAllSessions(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	messages, err := s.repo.GetMessages(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if messages == nil {
		messages = []llm.Message{}
	}
	writeJSON(w, messages)
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}

	// Ensure session exists
	_, err := s.repo.GetSession(sessionID)
	if err != nil {
		if err2 := s.repo.CreateSession(sessionID, "New chat", ""); err2 != nil {
			http.Error(w, "failed to create session", http.StatusInternalServerError)
			return
		}
	}

	// Stream response via SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Run agent
	ch, err := s.agent.Run(r.Context(), sessionID, req.Content)
	if err != nil {
		sendSSE(w, flusher, "error", fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		return
	}

	for chunk := range ch {
		if chunk.ToolCall != nil {
			data, _ := json.Marshal(map[string]any{
				"type":      "tool_call",
				"tool_name": chunk.ToolCall.Name,
				"tool_id":   chunk.ToolCall.ID,
			})
			sendSSE(w, flusher, "tool", string(data))
		}
		if chunk.TextDelta != "" {
			data, _ := json.Marshal(map[string]string{"text": chunk.TextDelta})
			sendSSE(w, flusher, "delta", string(data))
		}
		if chunk.Done {
			sendSSE(w, flusher, "done", `{"done":true}`)
		}
	}
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// Ensure store.Session is imported
var _ = time.Now
