package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCopyIgnoredFiles(t *testing.T) {
	// Create a real git repo so git ls-files works
	src := t.TempDir()
	dst := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	writeFile := func(path string, content []byte) {
		t.Helper()
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
	}
	mkdirAll := func(path string) {
		t.Helper()
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", path, err)
		}
	}

	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")

	// Create .gitignore
	writeFile(filepath.Join(src, ".gitignore"), []byte("*.secret\n.env*\ncreds/\n"))

	// Create a tracked file and commit so we have a repo
	writeFile(filepath.Join(src, "main.go"), []byte("package main"))
	run("add", ".")
	run("commit", "-m", "init")

	// Create ignored files
	writeFile(filepath.Join(src, ".env"), []byte("DB_URL=postgres://..."))
	writeFile(filepath.Join(src, ".env.local"), []byte("LOCAL=1"))
	writeFile(filepath.Join(src, "api.secret"), []byte("key=abc123"))
	mkdirAll(filepath.Join(src, "creds"))
	writeFile(filepath.Join(src, "creds", "key.pem"), []byte("-----BEGIN RSA-----"))

	// Create a non-ignored file (should NOT be copied since it's tracked)
	writeFile(filepath.Join(src, "README.md"), []byte("# hi"))
	run("add", "README.md")
	run("commit", "-m", "add readme")

	copyIgnoredFiles(src, dst)

	// Should exist (ignored files)
	for _, path := range []string{
		".env",
		".env.local",
		"api.secret",
		filepath.Join("creds", "key.pem"),
	} {
		full := filepath.Join(dst, path)
		if _, err := os.Stat(full); os.IsNotExist(err) {
			t.Errorf("expected %s to be copied, but it doesn't exist", path)
		}
	}

	// Should NOT exist (tracked files)
	for _, path := range []string{
		"main.go",
		"README.md",
		".gitignore",
	} {
		full := filepath.Join(dst, path)
		if _, err := os.Stat(full); err == nil {
			t.Errorf("expected %s to NOT be copied, but it exists", path)
		}
	}

	// Verify content preserved
	content, _ := os.ReadFile(filepath.Join(dst, ".env"))
	if string(content) != "DB_URL=postgres://..." {
		t.Errorf("expected .env content 'DB_URL=postgres://...', got %q", string(content))
	}
	content, _ = os.ReadFile(filepath.Join(dst, "creds", "key.pem"))
	if string(content) != "-----BEGIN RSA-----" {
		t.Errorf("expected creds/key.pem content, got %q", string(content))
	}
}
