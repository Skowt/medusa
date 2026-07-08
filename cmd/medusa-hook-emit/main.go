// medusa-hook-emit is a Claude Code hook command that forwards one lifecycle
// event to the Medusa hooks socket. It reads the hook payload from stdin,
// reduces it to the fields Medusa's activity tracking needs (including the
// outstanding background-task count on Stop/SubagentStop), and writes a single
// JSON line to the Unix socket.
//
// It must never write to stdout: plain stdout from a Stop hook is fed back to
// Claude as additional context and re-prompts the turn. All failure modes exit
// 0 silently — a lost event degrades activity tracking, which self-heals on
// the next event, whereas a failing hook command surfaces errors inside the
// Claude session.
package main

import (
	"flag"
	"io"
	"net"
	"os"
	"time"

	"github.com/Skowt/medusa/internal/hooks"
)

// maxStdin bounds the hook payload read: PostToolUse payloads embed the full
// tool response, which can be large; the fields we extract sit near the top
// level, but JSON requires the whole document, so allow a generous cap.
const maxStdin = 16 << 20

// socketTimeout bounds the connect+write so a wedged listener can never stall
// a Claude Code hook until its own timeout.
const socketTimeout = 2 * time.Second

func main() {
	event := flag.String("event", "", "Medusa event name for this hook rule")
	socket := flag.String("socket", "", "path to the Medusa hooks Unix socket")
	flag.Parse()

	session := os.Getenv("MEDUSA_SESSION_NAME")
	if *event == "" || *socket == "" || session == "" {
		return
	}
	// Socket existence doubles as the "Medusa is running" signal; hooks are a
	// silent no-op while Medusa is stopped.
	if _, err := os.Stat(*socket); err != nil {
		return
	}

	stdin, err := io.ReadAll(io.LimitReader(os.Stdin, maxStdin))
	if err != nil {
		stdin = nil // Emit the bare event; enrichment fields are best-effort.
	}
	line := hooks.BuildEventLine(*event, session, stdin, time.Now())
	if line == nil {
		return
	}

	conn, err := net.DialTimeout("unix", *socket, socketTimeout)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetWriteDeadline(time.Now().Add(socketTimeout))
	_, _ = conn.Write(line)
}
