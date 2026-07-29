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

// initIgnoreRepo builds a git repo whose .gitignore holds the given patterns and
// returns its path alongside a fresh destination dir.
func initIgnoreRepo(t *testing.T, ignore string) (string, string) {
	t.Helper()
	src, dst := t.TempDir(), t.TempDir()

	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	if err := os.WriteFile(filepath.Join(src, ".gitignore"), []byte(ignore), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	return src, dst
}

// A venv interpreter is a symlink into a Python install; dereferencing it produces a
// binary that can no longer resolve libpython from its new location. Links must survive
// the copy as links.
func TestCopyIgnoredFilesPreservesSymlinks(t *testing.T) {
	src, dst := initIgnoreRepo(t, "secrets/\n")

	outside := filepath.Join(t.TempDir(), "real-interpreter")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\necho hi"), 0o755); err != nil {
		t.Fatalf("write outside target: %v", err)
	}

	secrets := filepath.Join(src, "secrets")
	if err := os.MkdirAll(secrets, 0o755); err != nil {
		t.Fatalf("mkdir secrets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secrets, "key.pem"), []byte("KEY"), 0o600); err != nil {
		t.Fatalf("write key.pem: %v", err)
	}
	// Relative link within the copied tree, and an absolute link out of it.
	if err := os.Symlink("key.pem", filepath.Join(secrets, "key-alias.pem")); err != nil {
		t.Fatalf("symlink key-alias.pem: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(secrets, "interpreter")); err != nil {
		t.Fatalf("symlink interpreter: %v", err)
	}

	copyIgnoredFiles(src, dst)

	for _, name := range []string{"key-alias.pem", "interpreter"} {
		path := filepath.Join(dst, "secrets", name)
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("expected %s to be copied: %v", name, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s was dereferenced into a regular file; symlink must be preserved", name)
		}
	}

	// Link targets must be reproduced verbatim so both kinds still resolve.
	if target, _ := os.Readlink(filepath.Join(dst, "secrets", "key-alias.pem")); target != "key.pem" {
		t.Errorf("relative link target = %q, want %q", target, "key.pem")
	}
	if target, _ := os.Readlink(filepath.Join(dst, "secrets", "interpreter")); target != outside {
		t.Errorf("absolute link target = %q, want %q", target, outside)
	}
	if content, err := os.ReadFile(filepath.Join(dst, "secrets", "key-alias.pem")); err != nil || string(content) != "KEY" {
		t.Errorf("relative link does not resolve: content=%q err=%v", content, err)
	}
}

// Re-running the copy over an existing worktree must not fail on links already there.
func TestCopyIgnoredFilesSymlinkCopyIsIdempotent(t *testing.T) {
	src, dst := initIgnoreRepo(t, "secrets/\n")

	secrets := filepath.Join(src, "secrets")
	if err := os.MkdirAll(secrets, 0o755); err != nil {
		t.Fatalf("mkdir secrets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secrets, "key.pem"), []byte("KEY"), 0o600); err != nil {
		t.Fatalf("write key.pem: %v", err)
	}
	if err := os.Symlink("key.pem", filepath.Join(secrets, "key-alias.pem")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	copyIgnoredFiles(src, dst)
	copyIgnoredFiles(src, dst)

	link := filepath.Join(dst, "secrets", "key-alias.pem")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("link missing after second copy: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("link became a regular file on the second copy")
	}
}

func TestCopyIgnoredFilesSkipsBuildAndEnvDirs(t *testing.T) {
	src, dst := initIgnoreRepo(t, ".venv/\nnode_modules/\n.ruff_cache/\n.env\n")

	for _, rel := range []string{
		".venv/bin/python",
		".venv/lib/python3.13/site-packages/foo.py",
		"node_modules/left-pad/index.js",
		".ruff_cache/cache",
	} {
		full := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, ".env"), []byte("DB=1"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	copyIgnoredFiles(src, dst)

	for _, rel := range []string{
		".venv/bin/python",
		".venv/lib/python3.13/site-packages/foo.py",
		"node_modules/left-pad/index.js",
		".ruff_cache/cache",
	} {
		if _, err := os.Lstat(filepath.Join(dst, rel)); err == nil {
			t.Errorf("expected %s to be skipped, but it was copied", rel)
		}
	}

	// The files the feature actually exists for still come across.
	if content, err := os.ReadFile(filepath.Join(dst, ".env")); err != nil || string(content) != "DB=1" {
		t.Errorf(".env should still be copied: content=%q err=%v", content, err)
	}
}

func TestIsSkippedPath(t *testing.T) {
	skipped := []string{
		".venv/bin/python",
		"backend/.venv/bin/python",
		"venv/pyvenv.cfg",
		"node_modules/pkg/index.js",
		"web/node_modules/.bin/tsc",
		"a/__pycache__/mod.pyc",
		".ruff_cache/x",
	}
	for _, path := range skipped {
		if !isSkippedPath(path) {
			t.Errorf("isSkippedPath(%q) = false, want true", path)
		}
	}

	kept := []string{
		".env",
		".env.local",
		"creds/key.pem",
		"config/app.yaml",
		// Substring matches must not trigger a skip.
		"my.venv.notes",
		"venvtools/config",
		"src/node_modules_helper.js",
	}
	for _, path := range kept {
		if isSkippedPath(path) {
			t.Errorf("isSkippedPath(%q) = true, want false", path)
		}
	}
}
