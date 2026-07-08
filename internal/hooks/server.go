package hooks

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

// SocketName is the Unix socket filename inside the hooks directory. Hook
// commands write one JSON event line per connection; the socket's existence
// doubles as the "Medusa is running" signal, so hooks are a silent no-op
// while Medusa is stopped (unlike event files, which would accumulate).
const SocketName = "medusa.sock"

// SocketPath returns the hooks socket path for a hooks directory.
func SocketPath(hooksDir string) string {
	return filepath.Join(hooksDir, SocketName)
}

// maxEventLine bounds a single event message so a misbehaving writer cannot
// hold a read goroutine on unbounded input.
const maxEventLine = 64 * 1024

// connReadTimeout bounds how long a connection may take to deliver its line.
const connReadTimeout = 5 * time.Second

// Server listens on a Unix socket for JSON hook events written by Claude
// Code hooks and dispatches parsed events via a callback. Protocol: one JSON
// object per connection, newline-terminated; the server closes the
// connection after reading the line, which also signals nc to exit.
type Server struct {
	listener net.Listener
	onEvent  func(HookEvent)
}

// NewServer creates the hooks socket listener. A leftover socket file from an
// unclean shutdown is removed; a socket with a live listener means another
// Medusa instance is running, which is an error (the caller falls back to
// the file watcher only).
func NewServer(socketPath string, onEvent func(HookEvent)) (*Server, error) {
	if _, err := os.Stat(socketPath); err == nil {
		conn, err := net.DialTimeout("unix", socketPath, time.Second)
		if err == nil {
			_ = conn.Close()
			return nil, fmt.Errorf("hooks socket %s is held by another instance", socketPath)
		}
		_ = os.Remove(socketPath)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	return &Server{listener: ln, onEvent: onEvent}, nil
}

// Run accepts connections until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = s.listener.Close()
		case <-done:
		}
	}()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(connReadTimeout))
	line, err := bufio.NewReader(io.LimitReader(conn, maxEventLine)).ReadString('\n')
	// A writer that closes without a trailing newline still delivers a line
	// with err == io.EOF; only give up when nothing was read.
	if line == "" && err != nil {
		return
	}
	var raw struct {
		Event           string `json:"event"`
		TS              int64  `json:"ts"`
		Session         string `json:"session"`
		Message         string `json:"message"`
		Tool            string `json:"tool"`
		Outstanding     *int   `json:"outstanding"`
		ClaudeSessionID string `json:"claude_session_id"`
		AgentType       string `json:"agent_type"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return
	}
	if raw.Event == "" || raw.Session == "" {
		return
	}
	outstanding := OutstandingUnknown
	if raw.Outstanding != nil && *raw.Outstanding >= 0 {
		outstanding = *raw.Outstanding
	}
	if s.onEvent != nil {
		s.onEvent(HookEvent{
			SessionName:     raw.Session,
			Event:           EventType(raw.Event),
			Timestamp:       parseHookTS(raw.TS),
			Message:         raw.Message,
			Tool:            raw.Tool,
			Outstanding:     outstanding,
			ClaudeSessionID: raw.ClaudeSessionID,
			AgentType:       raw.AgentType,
		})
	}
}

// Close stops the listener; the net package removes the socket file.
func (s *Server) Close() error {
	return s.listener.Close()
}
