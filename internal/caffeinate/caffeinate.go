// Package caffeinate keeps a Mac awake by holding a `caffeinate -d` process
// open for as long as the user wants it.
package caffeinate

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"

	"github.com/Skowt/medusa/internal/safego"
)

// Keeper owns at most one running caffeinate process.
type Keeper struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan struct{}
	command func() *exec.Cmd
}

// New returns a Keeper that runs the system caffeinate binary.
func New() *Keeper {
	return &Keeper{command: systemCommand}
}

// systemCommand builds the caffeinate invocation. -w ties the assertion to
// medusa's own lifetime, so a crash that skips Shutdown cannot leave the Mac
// awake until someone finds and kills the stray process.
func systemCommand() *exec.Cmd {
	return exec.Command("caffeinate", "-d", "-w", strconv.Itoa(os.Getpid()))
}

// Supported reports whether caffeinate can run here: macOS only.
func Supported() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := exec.LookPath("caffeinate")
	return err == nil
}

// Active reports whether a caffeinate process is currently running. A process
// that exited on its own (killed from outside, say) reads as inactive.
func (k *Keeper) Active() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.runningLocked()
}

func (k *Keeper) runningLocked() bool {
	if k.done == nil {
		return false
	}
	select {
	case <-k.done:
		return false
	default:
		return true
	}
}

// Start launches caffeinate. It is a no-op when one is already running.
func (k *Keeper) Start() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.runningLocked() {
		return nil
	}
	cmd := k.command()
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	safego.Go("caffeinate.wait", func() {
		_ = cmd.Wait()
		close(done)
	})
	k.cmd, k.done = cmd, done
	return nil
}

// Stop ends the running caffeinate process, if any, and waits for it to exit.
func (k *Keeper) Stop() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.runningLocked() {
		_ = k.cmd.Process.Kill()
		<-k.done
	}
	k.cmd, k.done = nil, nil
}
