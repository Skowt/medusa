package ide

import (
	"os"
	"path/filepath"
	"testing"
)

func mkBundle(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDetectMacGroupsByIDESystemBeforeUser(t *testing.T) {
	sys := t.TempDir()
	usr := t.TempDir()
	mkBundle(t, sys, "Cursor.app")
	mkBundle(t, usr, "Cursor.app")
	mkBundle(t, sys, "Zed.app")

	got := detectMac([]scanDir{{sys, "System"}, {usr, "User"}})

	if len(got) != 3 {
		t.Fatalf("want 3 installs, got %d: %+v", len(got), got)
	}
	if got[0].Name != "Cursor" || got[0].Location != "System" {
		t.Errorf("got[0] = %+v, want Cursor/System", got[0])
	}
	if got[1].Name != "Cursor" || got[1].Location != "User" {
		t.Errorf("got[1] = %+v, want Cursor/User", got[1])
	}
	if got[2].Name != "Zed" || got[2].Location != "System" {
		t.Errorf("got[2] = %+v, want Zed/System", got[2])
	}
	if got[0].LaunchPath != filepath.Join(sys, "Cursor.app") {
		t.Errorf("LaunchPath = %q, want bundle path", got[0].LaunchPath)
	}
}

func TestDisplayLabelBracketsOnlyOnCollision(t *testing.T) {
	installs := []Install{
		{Name: "Cursor", Location: "System"},
		{Name: "Cursor", Location: "User"},
		{Name: "Zed", Location: "System"},
	}
	if got := DisplayLabel(installs, 0); got != "Cursor (System)" {
		t.Errorf("got %q, want %q", got, "Cursor (System)")
	}
	if got := DisplayLabel(installs, 1); got != "Cursor (User)" {
		t.Errorf("got %q, want %q", got, "Cursor (User)")
	}
	if got := DisplayLabel(installs, 2); got != "Zed" {
		t.Errorf("got %q, want %q (no bracket when unique)", got, "Zed")
	}
}

func TestDetectPathGroupsByCommand(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	writeExec(t, filepath.Join(a, "cursor"))
	writeExec(t, filepath.Join(b, "cursor"))
	writeExec(t, filepath.Join(a, "code"))
	// A non-executable file named like an IDE command must be ignored.
	if err := os.WriteFile(filepath.Join(b, "code"), []byte("not exec\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := detectPath([]string{a, b})

	if len(got) != 3 {
		t.Fatalf("want 3 installs, got %d: %+v", len(got), got)
	}
	if got[0].Name != "cursor" || got[0].Location != a {
		t.Errorf("got[0] = %+v, want cursor in %q", got[0], a)
	}
	if got[1].Name != "cursor" || got[1].Location != b {
		t.Errorf("got[1] = %+v, want cursor in %q", got[1], b)
	}
	if got[2].Name != "code" || got[2].Location != a {
		t.Errorf("got[2] = %+v, want code in %q", got[2], a)
	}
	if got[2].LaunchPath != filepath.Join(a, "code") {
		t.Errorf("got[2].LaunchPath = %q, want %q", got[2].LaunchPath, filepath.Join(a, "code"))
	}
}

func writeExec(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
