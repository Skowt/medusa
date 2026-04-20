//go:build sandbox_mode

package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Skowt/medusa/internal/config"
)

// --- OAuth / credential tests ---
// These tests verify whether the sandbox profile allows or blocks operations
// that Claude Code needs for OAuth authentication.

func TestSandbox_OAuthWriteToConfigDir(t *testing.T) {
	// Claude Code stores credentials under CLAUDE_CONFIG_DIR when set.
	// The sandbox must allow writes to the config dir for OAuth to work.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	// Simulate Claude writing OAuth credentials to its config dir
	credFile := filepath.Join(env.ConfigDir, "credentials.json")
	cmd := fmt.Sprintf(`echo '{"oauth_token":"test"}' > %s`, credFile)
	out, err := runSandboxed(t, env.SBPLPath, cmd)
	if err != nil {
		t.Fatalf("writing credentials to CLAUDE_CONFIG_DIR should succeed: %v\noutput: %s", err, out)
	}

	data, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("credential file should exist: %v", err)
	}
	if !strings.Contains(string(data), "oauth_token") {
		t.Error("credential file should contain the written token")
	}
}

func TestSandbox_OAuthWriteToClaudeHomeBlocked(t *testing.T) {
	// If Claude Code ignores CLAUDE_CONFIG_DIR for some operations and
	// tries to write directly to ~/.claude/, the sandbox will block it.
	// This test documents that behavior.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	home, _ := os.UserHomeDir()

	// Create a temp dir under ~/.claude-sandbox-test to simulate ~/.claude
	// (we don't want to touch the real ~/.claude)
	testClaudeDir := filepath.Join(home, ".claude-sandbox-oauth-test")
	if err := os.MkdirAll(testClaudeDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(testClaudeDir) })

	credFile := filepath.Join(testClaudeDir, "credentials.json")
	cmd := fmt.Sprintf(`echo '{"token":"test"}' > %s`, credFile)
	_, err := runSandboxed(t, env.SBPLPath, cmd)
	if err == nil {
		t.Error("writing to arbitrary home directory path should be BLOCKED by sandbox")
		os.Remove(credFile)
	}
}

func TestSandbox_OAuthReadClaudeHome(t *testing.T) {
	// Reading ~/.claude/ should work (global file-read* is allowed).
	// This matters for reading existing credentials/config.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	home, _ := os.UserHomeDir()

	// Create a test file simulating existing credentials
	testClaudeDir := filepath.Join(home, ".claude-sandbox-read-test")
	if err := os.MkdirAll(testClaudeDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(testClaudeDir) })

	testFile := filepath.Join(testClaudeDir, "config.json")
	if err := os.WriteFile(testFile, []byte(`{"existing":"config"}`), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	out, err := runSandboxed(t, env.SBPLPath, "cat "+testFile)
	if err != nil {
		t.Fatalf("reading config files outside sandbox should succeed (file-read* is global): %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "existing") {
		t.Error("should be able to read existing config files")
	}
}

func TestSandbox_OAuthLocalhostListen(t *testing.T) {
	// OAuth flow requires listening on localhost for the callback.
	// The sandbox allows network* so this should work.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	// Use Python to bind a socket briefly on localhost, then exit
	cmd := `python3 -c "
import socket, sys
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
try:
    s.bind(('127.0.0.1', 0))
    s.listen(1)
    port = s.getsockname()[1]
    print(f'listening on {port}')
    s.close()
except Exception as e:
    print(f'error: {e}', file=sys.stderr)
    sys.exit(1)
"`
	out, err := runSandboxed(t, env.SBPLPath, cmd)
	if err != nil {
		t.Fatalf("localhost listen should succeed (network* allowed): %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "listening on") {
		t.Errorf("expected 'listening on <port>', got %q", out)
	}
}

func TestSandbox_OAuthOutboundHTTPS(t *testing.T) {
	// OAuth requires outbound HTTPS to the auth server.
	// The sandbox allows network* so this should work.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	// Use curl to make a simple HTTPS request (just check DNS + TLS works)
	out, err := runSandboxed(t, env.SBPLPath, "curl -sf -o /dev/null -w '%{http_code}' https://api.anthropic.com/ 2>&1 || true")
	if err != nil {
		t.Logf("curl attempt output: %s (err: %v)", out, err)
		// Even a connection error is fine — we're testing that the sandbox
		// doesn't block the network operation itself
	}

	// Alternative: just verify DNS resolution works
	out2, err2 := runSandboxed(t, env.SBPLPath, "python3 -c \"import socket; print(socket.getaddrinfo('api.anthropic.com', 443)[0][4][0])\"")
	if err2 != nil {
		t.Fatalf("DNS resolution should succeed (network* allowed): %v\noutput: %s", err2, out2)
	}
}

func TestSandbox_OAuthWriteClaudeJSON(t *testing.T) {
	// Claude Code may write to ~/.claude.json (the top-level config).
	// With CLAUDE_CONFIG_DIR set, it should write to $CLAUDE_CONFIG_DIR/.claude.json instead.
	// But if it falls back to ~/.claude.json, the sandbox will block it.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	home, _ := os.UserHomeDir()

	// Test 1: Writing to $CLAUDE_CONFIG_DIR/.claude.json should SUCCEED
	configJSON := filepath.Join(env.ConfigDir, ".claude.json")
	cmd := fmt.Sprintf(`echo '{"projects":{}}' > %s`, configJSON)
	out, err := runSandboxed(t, env.SBPLPath, cmd)
	if err != nil {
		t.Errorf("writing .claude.json inside CLAUDE_CONFIG_DIR should succeed: %v\noutput: %s", err, out)
	}

	// Test 2: Writing to ~/.claude.json should be BLOCKED
	// Use a sentinel file so we don't corrupt the real ~/.claude.json
	sentinelPath := filepath.Join(home, ".claude-sandbox-test-sentinel.json")
	cmd2 := fmt.Sprintf(`echo '{"test":true}' > %s`, sentinelPath)
	_, err2 := runSandboxed(t, env.SBPLPath, cmd2)
	if err2 == nil {
		t.Error("writing to ~/<dotfile>.json outside sandbox allowlist should be BLOCKED")
		os.Remove(sentinelPath)
	}
}

func TestSandbox_OAuthKeychainAccess(t *testing.T) {
	// Claude Code may use the macOS Keychain for credential storage.
	// The sandbox allows mach-lookup which covers most XPC services,
	// but we should verify security framework access works.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	// Try to list keychain items (read-only, non-destructive)
	// security find-generic-password just queries — if sandbox blocks Keychain
	// XPC, we'll see a specific error
	out, err := runSandboxed(t, env.SBPLPath, "security list-keychains 2>&1")
	if err != nil {
		t.Errorf("Keychain access may be blocked by sandbox: %v\noutput: %s", err, out)
		t.Log("If Claude Code uses Keychain for OAuth tokens, the sandbox could break authentication")
	} else {
		t.Logf("Keychain access works: %s", out)
	}
}

func TestSandbox_OAuthConfigLockDir(t *testing.T) {
	// Claude Code creates a lock directory as a sibling of the config dir
	// (e.g. "Work.lock" next to "Work/") to acquire a config lock before
	// writing credentials. The sandbox must allow this.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	lockDir := env.ConfigDir + ".lock"
	cmd := fmt.Sprintf("mkdir -p %s && touch %s/test.lock", lockDir, lockDir)
	out, err := runSandboxed(t, env.SBPLPath, cmd)
	if err != nil {
		t.Fatalf("creating config lock dir should succeed: %v\noutput: %s", err, out)
	}
	t.Cleanup(func() { os.RemoveAll(lockDir) })

	if _, err := os.Stat(filepath.Join(lockDir, "test.lock")); err != nil {
		t.Error("lock file should exist after creation")
	}
}

func TestSandbox_OAuthClaudeStateDir(t *testing.T) {
	// Claude Code writes version locks to ~/.local/state/claude/locks/.
	// The sandbox must allow writes there.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	home, _ := os.UserHomeDir()
	stateDir := filepath.Join(home, ".local", "state", "claude", "locks")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	lockFile := filepath.Join(stateDir, "sandbox-test.lock")
	cmd := fmt.Sprintf("touch %s", lockFile)
	out, err := runSandboxed(t, env.SBPLPath, cmd)
	if err != nil {
		t.Fatalf("writing to ~/.local/state/claude should succeed: %v\noutput: %s", err, out)
	}
	t.Cleanup(func() { os.Remove(lockFile) })

	if _, err := os.Stat(lockFile); err != nil {
		t.Error("lock file should exist after creation in claude state dir")
	}
}

func TestSandbox_OAuthNoProfile_WriteClaudeHomeFails(t *testing.T) {
	// When a workspace is isolated but has NO profile (CLAUDE_CONFIG_DIR unset),
	// Claude Code falls back to ~/.claude/ and ~/.claude.json for credential storage.
	// The sandbox blocks all writes there because claudeConfigDir="" means no
	// config dir is in the write allowlist.
	//
	// This is the primary way the sandbox can break OAuth.
	skipIfNoSandboxExec(t)

	home, _ := os.UserHomeDir()
	worktreeRoot := t.TempDir()

	// Simulate no profile: empty claudeConfigDir
	sbpl := GenerateSBPL(worktreeRoot, nil, "", "", config.DefaultSandboxRules().Rules)
	sbplPath, cleanup, err := WriteTempProfile(sbpl)
	if err != nil {
		t.Fatalf("WriteTempProfile: %v", err)
	}
	t.Cleanup(cleanup)

	// Claude Code would try to write OAuth tokens to ~/.claude/credentials.json
	testDir := filepath.Join(home, ".claude-sandbox-no-profile-test")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(testDir) })

	credFile := filepath.Join(testDir, "credentials.json")
	cmd := fmt.Sprintf(`echo '{"oauth_token":"test"}' > %s`, credFile)
	_, err = runSandboxed(t, sbplPath, cmd)
	if err == nil {
		t.Error("BUG: with no profile, sandbox allows writing to home — OAuth token writes should be blocked")
		os.Remove(credFile)
		return
	}

	// This confirms the problem: no profile + isolated = can't store OAuth creds
	t.Log("Confirmed: sandbox blocks credential writes when no profile is set (no CLAUDE_CONFIG_DIR)")
	t.Log("This breaks OAuth because Claude Code can't persist tokens to ~/.claude/")
}
