package cmd

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// spinner provides a restartable terminal spinner for long-running operations.
// It writes to stderr and uses carriage returns to avoid polluting stdout.
// Safe to call start/stop repeatedly across multiple agent-loop turns.
type spinner struct {
	mu      sync.Mutex
	running bool
	done    chan struct{}
	message string
	active  bool // false when globally disabled (json mode, non-TTY)
}

func newSpinner(message string) *spinner {
	return &spinner{
		message: message,
		active:  outputFmt != "json" && isTerminal(os.Stderr),
	}
}

// start begins the spinner animation. Idempotent — no-op if already running.
func (s *spinner) start() {
	if !s.active {
		return
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.done = make(chan struct{})
	s.mu.Unlock()

	go func() {
		frames := []rune{'|', '/', '-', '\\'}
		i := 0
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-s.done:
				fmt.Fprintf(os.Stderr, "\r\033[K")
				return
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "\r\033[K%c %s", frames[i%len(frames)], s.message)
				i++
			}
		}
	}()
}

// stop halts the spinner and clears the line. Idempotent — no-op if not running.
func (s *spinner) stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	done := s.done
	s.mu.Unlock()

	close(done)
	// Wait for goroutine to clear the line before caller prints
	time.Sleep(10 * time.Millisecond)
}

// isTerminal checks whether the given file is a terminal.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
