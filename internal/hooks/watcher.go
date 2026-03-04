package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// EventType represents the type of Claude Code hook event.
type EventType string

const (
	EventStop                       EventType = "Stop"
	EventNotificationIdle           EventType = "NotificationIdle"
	EventNotificationPermission     EventType = "NotificationPermission"
	EventNotificationElicitation    EventType = "NotificationElicitation"
	EventPreToolUse                 EventType = "PreToolUse"
	EventUserPromptSubmit           EventType = "UserPromptSubmit"
)

// debounceWindow is the quiet period before processing a file change.
// Filesystem events can fire multiple times per write; this coalesces them
// and ensures we always read the final state.
const debounceWindow = 100 * time.Millisecond

// HookEvent is the parsed event delivered to the callback.
type HookEvent struct {
	SessionName string
	Event       EventType
	Timestamp   time.Time
}

// Watcher monitors a directory for JSON hook event files written by
// Claude Code hooks and dispatches parsed events via a callback.
type Watcher struct {
	hooksDir  string
	watcher   *fsnotify.Watcher
	onEvent   func(HookEvent)
	pending   map[string]*time.Timer // trailing-edge debounce timers
	mu        sync.Mutex
	closeOnce sync.Once
}

// NewWatcher creates a new hooks directory watcher.
func NewWatcher(hooksDir string, onEvent func(HookEvent)) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fw.Add(hooksDir); err != nil {
		fw.Close()
		return nil, err
	}
	return &Watcher{
		hooksDir: hooksDir,
		watcher:  fw,
		onEvent:  onEvent,
		pending:  make(map[string]*time.Timer),
	}, nil
}

// Run processes file system events until the context is canceled.
func (w *Watcher) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case event, ok := <-w.watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			if !strings.HasSuffix(event.Name, ".json") {
				continue
			}

			base := filepath.Base(event.Name)
			sessionName := strings.TrimSuffix(base, ".json")
			if sessionName == "" {
				continue
			}

			// Trailing-edge debounce: reset timer on each event so we
			// always process the latest file content after a quiet period.
			path := event.Name
			w.mu.Lock()
			if t, ok := w.pending[sessionName]; ok {
				t.Stop()
			}
			w.pending[sessionName] = time.AfterFunc(debounceWindow, func() {
				w.processFile(path, sessionName)
				w.mu.Lock()
				delete(w.pending, sessionName)
				w.mu.Unlock()
			})
			w.mu.Unlock()

		case _, ok := <-w.watcher.Errors:
			if !ok {
				return nil
			}
		}
	}
}

func (w *Watcher) processFile(path, sessionName string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var raw struct {
		Event string `json:"event"`
		TS    int64  `json:"ts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	if raw.Event == "" {
		return
	}
	he := HookEvent{
		SessionName: sessionName,
		Event:       EventType(raw.Event),
		Timestamp:   time.Unix(raw.TS, 0),
	}
	if w.onEvent != nil {
		w.onEvent(he)
	}
}

// Close stops the watcher and releases resources.
func (w *Watcher) Close() error {
	var err error
	w.closeOnce.Do(func() {
		w.mu.Lock()
		for _, t := range w.pending {
			t.Stop()
		}
		w.pending = nil
		w.mu.Unlock()
		err = w.watcher.Close()
	})
	return err
}

// CleanStaleFiles removes hook event files older than maxAge.
func CleanStaleFiles(hooksDir string, maxAge time.Duration) {
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(hooksDir, entry.Name()))
		}
	}
}
