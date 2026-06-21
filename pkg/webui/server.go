package webui

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/naman833/k8stalk/pkg/agent"
	"github.com/naman833/k8stalk/pkg/llm"
	"github.com/naman833/k8stalk/pkg/sanitize"
	"github.com/naman833/k8stalk/pkg/store"
)

//go:embed static/*
var staticFiles embed.FS

// Server is the local HTTP server for the chat UI.
type Server struct {
	agent      *agent.Agent
	repo       *store.Repository
	provider   llm.Provider
	san        *sanitize.Sanitizer
	addr       string
	httpServer *http.Server
}

// NewServer creates a new web UI server.
func NewServer(agentInstance *agent.Agent, repo *store.Repository, provider llm.Provider, san *sanitize.Sanitizer, addr string) *Server {
	return &Server{
		agent:    agentInstance,
		repo:     repo,
		provider: provider,
		san:      san,
		addr:     addr,
	}
}

// Start starts the HTTP server.
func (s *Server) Start(openBrowser bool) error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("DELETE /api/sessions", s.handleDeleteAllSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("GET /api/sessions/{id}/messages", s.handleGetMessages)
	mux.HandleFunc("POST /api/sessions/{id}/messages", s.handleSendMessage)
	mux.HandleFunc("POST /api/shutdown", s.handleShutdown)

	// Static files (embedded React app)
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("static files: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	actualAddr := listener.Addr().String()
	url := fmt.Sprintf("http://%s", actualAddr)

	fmt.Printf("k8stalk chat server running at %s\n", url)
	fmt.Printf("Chat history is stored locally at %s\n", "~/.config/k8stalk/history.db")
	fmt.Println("Nothing leaves this machine except what's sent to your configured LLM provider.")
	fmt.Println("Run `k8stalk chat --clear-history` anytime to delete it.")

	if openBrowser {
		openURL(url)
	}

	s.httpServer = &http.Server{Handler: mux}
	err = s.httpServer.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	// Shut down gracefully in the background
	go func() {
		time.Sleep(100 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(ctx)
		os.Exit(0)
	}()
}

func openURL(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd != nil {
		_ = cmd.Start()
	}
}
