package skillstats

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Service is an on-demand dashboard for a long-running host process like the
// Medusa TUI: the first URL call opens the store, scans transcripts, and starts
// the HTTP server; later calls hand back the same address.
//
// It listens on an ephemeral loopback port rather than a fixed one, so it can
// never collide with a standalone medusa-skills already serving, and is not
// reachable off the machine.
type Service struct {
	mu             sync.Mutex
	storeDir       string
	profilesRoot   string
	claudeProjects string

	url   string
	srv   *http.Server
	store *Store
}

// NewService describes a dashboard without starting anything. Nothing touches
// the filesystem until URL is called.
func NewService(storeDir, profilesRoot, claudeProjects string) *Service {
	return &Service{storeDir: storeDir, profilesRoot: profilesRoot, claudeProjects: claudeProjects}
}

// URL returns the address of a running dashboard, starting it on first call.
//
// The first call does a cold transcript scan (about a second over a few hundred
// megabytes), so callers must not run it on a UI thread.
func (s *Service) URL() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.url != "" {
		return s.url, nil
	}

	store, err := Open(s.storeDir, s.profilesRoot, s.claudeProjects)
	if err != nil {
		return "", err
	}
	store.Scan()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}
	srv := &http.Server{
		Handler:           NewServer(store).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	// Serve owns the listener from here; a serve error after startup only means
	// the dashboard stopped, which must not take the host process down.
	go func() { _ = srv.Serve(ln) }()

	s.store = store
	s.srv = srv
	s.url = "http://" + ln.Addr().String()
	return s.url, nil
}

// Close stops the dashboard if it was started. A Service that was never used is
// a no-op, so hosts can call this unconditionally on shutdown.
func (s *Service) Close() error {
	s.mu.Lock()
	srv := s.srv
	s.srv, s.url, s.store = nil, "", nil
	s.mu.Unlock()

	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
