package caffeinate

import (
	"os/exec"
	"testing"
	"time"
)

// fakeKeeper runs a long sleep in place of caffeinate, so the tests exercise
// process handling on any platform.
func fakeKeeper() *Keeper {
	return &Keeper{command: func() *exec.Cmd { return exec.Command("sleep", "60") }}
}

func TestStartThenStop(t *testing.T) {
	k := fakeKeeper()
	if k.Active() {
		t.Fatal("new keeper reports active")
	}
	if err := k.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !k.Active() {
		t.Fatal("keeper inactive after Start")
	}
	k.Stop()
	if k.Active() {
		t.Fatal("keeper still active after Stop")
	}
}

// TestStartTwiceKeepsOneProcess guards against a second press spawning a
// second caffeinate that Stop would then never reach.
func TestStartTwiceKeepsOneProcess(t *testing.T) {
	k := fakeKeeper()
	if err := k.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	first := k.cmd.Process.Pid
	if err := k.Start(); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if k.cmd.Process.Pid != first {
		t.Errorf("second Start replaced process %d with %d", first, k.cmd.Process.Pid)
	}
	k.Stop()
}

// TestExternalExitReadsInactive makes sure a process killed from outside is
// not reported as still keeping the Mac awake.
func TestExternalExitReadsInactive(t *testing.T) {
	k := fakeKeeper()
	if err := k.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = k.cmd.Process.Kill()
	deadline := time.Now().Add(5 * time.Second)
	for k.Active() {
		if time.Now().After(deadline) {
			t.Fatal("keeper still active after its process was killed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	k.Stop() // must not block or panic on an already-exited process
}

func TestStopWithoutStart(t *testing.T) {
	fakeKeeper().Stop()
}

func TestStartReportsMissingBinary(t *testing.T) {
	k := &Keeper{command: func() *exec.Cmd { return exec.Command("/nonexistent/caffeinate") }}
	if err := k.Start(); err == nil {
		t.Fatal("Start succeeded with a missing binary")
	}
	if k.Active() {
		t.Fatal("keeper active after a failed Start")
	}
}
