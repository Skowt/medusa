package config

import (
	"path/filepath"
	"testing"
)

// Each profile keeps its own assistant: launching Codex under one must not
// change what another opens on.
func TestAssistantIsRememberedPerProfile(t *testing.T) {
	s := defaultUISettings()
	s.RememberAssistant("Work", "claude")
	s.RememberAssistant("Default", "codex")

	if got := s.AssistantFor("Work"); got != "claude" {
		t.Errorf("Work = %q, want claude", got)
	}
	if got := s.AssistantFor("Default"); got != "codex" {
		t.Errorf("Default = %q, want codex", got)
	}
}

// A profile that has never launched anything, and a workspace with no profile,
// open on the most recent assistant used anywhere.
func TestAssistantFallsBackToGlobalLastUsed(t *testing.T) {
	s := defaultUISettings()
	s.RememberAssistant("Default", "codex")

	if got := s.AssistantFor("Fresh"); got != "codex" {
		t.Errorf("unseen profile = %q, want the global codex", got)
	}
	if got := s.AssistantFor(""); got != "codex" {
		t.Errorf("no profile = %q, want the global codex", got)
	}
}

func TestAssistantByProfileFollowsRenameAndDelete(t *testing.T) {
	s := defaultUISettings()
	s.RememberAssistant("Old", "codex")
	s.LastAssistant = "claude"

	s.RenameProfileAssistant("Old", "New")
	if got := s.AssistantFor("New"); got != "codex" {
		t.Errorf("renamed profile = %q, want codex", got)
	}
	if _, ok := s.LastAssistantByProfile["Old"]; ok {
		t.Error("old profile name still has an entry after rename")
	}

	s.ForgetProfileAssistant("New")
	if got := s.AssistantFor("New"); got != "claude" {
		t.Errorf("deleted profile = %q, want the global claude", got)
	}
}

func TestAssistantByProfileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s := defaultUISettings()
	s.RememberAssistant("Work", "claude")
	s.RememberAssistant("Default", "codex")
	if err := saveUISettings(path, s); err != nil {
		t.Fatal(err)
	}

	loaded := loadUISettings(path)
	if got := loaded.AssistantFor("Work"); got != "claude" {
		t.Errorf("Work after reload = %q, want claude", got)
	}
	if got := loaded.AssistantFor("Default"); got != "codex" {
		t.Errorf("Default after reload = %q, want codex", got)
	}
}
