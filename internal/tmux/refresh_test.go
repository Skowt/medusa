package tmux

import (
	"bytes"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestRefreshSessionClientsIgnoresAMissingSession(t *testing.T) {
	opts := privateServer(t)
	if err := RefreshSessionClients("no-such-session", opts); err != nil {
		t.Fatalf("RefreshSessionClients = %v; want nil for a missing session", err)
	}
}

// clientOutput collects everything a client is sent, from a background reader.
type clientOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
	at  time.Time // when the last byte arrived
}

func (o *clientOutput) collect(f *os.File) {
	b := make([]byte, 4096)
	for {
		n, err := f.Read(b)
		if n > 0 {
			o.mu.Lock()
			o.buf.Write(b[:n])
			o.at = time.Now()
			o.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// waitFor waits until want appears after offset from, and reports whether it did.
func (o *clientOutput) waitFor(want []byte, from int, timeout time.Duration) bool {
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		o.mu.Lock()
		found := bytes.Contains(o.buf.Bytes()[from:], want)
		o.mu.Unlock()
		if found {
			return true
		}
	}
	return false
}

// settle waits until the client has been sent nothing for quiet, and returns
// how much it had been sent by then.
func (o *clientOutput) settle(quiet, timeout time.Duration) int {
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		o.mu.Lock()
		idle, n := time.Since(o.at), o.buf.Len()
		o.mu.Unlock()
		if idle >= quiet {
			return n
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.Len()
}

// A client that was sent the pane once is sent it again after a refresh: that
// is what repairs a client whose copy of the screen went wrong.
func TestRefreshSessionClientsRedrawsAttachedClients(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out tmux's own post-attach redraw")
	}
	opts := privateServer(t)
	marker := []byte("redraw-marker")
	if err := StartDetachedSession("s", t.TempDir(), "printf redraw-marker; sleep 30", nil, opts, SessionTags{}); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("tmux", tmuxArgs(opts, "attach", "-t", "s")...)
	cmd.Env = append(os.Environ(), "TERM="+ClientTerm)
	client, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 10, Cols: 40})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	var out clientOutput
	go out.collect(client)
	if !out.waitFor(marker, 0, 5*time.Second) {
		t.Fatal("client never drew the pane")
	}
	// tmux redraws once more on its own about five seconds after attach (its
	// terminal queries go unanswered here), so only output after that counts.
	time.Sleep(6 * time.Second)
	from := out.settle(time.Second, 10*time.Second)
	if out.waitFor(marker, from, 500*time.Millisecond) {
		t.Fatal("client redrew without being asked; the check below would prove nothing")
	}

	if err := RefreshSessionClients("s", opts); err != nil {
		t.Fatalf("RefreshSessionClients: %v", err)
	}
	if !out.waitFor(marker, from, 5*time.Second) {
		t.Fatal("refresh did not redraw the client")
	}
}
