//go:build !windows

package main

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestKillPID_KillsTheGivenPid verifies killPID signals exactly the pid it
// is given - the same bounded scope as the original `kill <pid>` shell
// command it replaces. (A process-group kill via -pgid was tried and
// reverted here: a pid read back out of a stale pid file can have been
// recycled by an unrelated process by the time disconnect runs, so
// signaling "the whole group of whatever currently holds that pid" is
// unsafe in a way signaling just the pid itself is not - see killPID's
// doc comment.)
func TestKillPID_KillsTheGivenPid(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start sleep: %v", err)
	}
	pid := cmd.Process.Pid

	if err := killPID(pid); err != nil {
		t.Fatalf("killPID: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		// exited - good.
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("process %d was not killed by killPID", pid)
	}
}

// TestKillPID_AlreadyGoneIsNotAnError covers the "stale pid" case: signaling
// a pid that no longer exists must not be treated as a hard error by the
// caller (runDisconnect ignores killPID's return value either way, but the
// function itself should behave predictably here too).
func TestKillPID_AlreadyGoneIsNotAnError(t *testing.T) {
	// A pid vanishingly unlikely to be in use.
	err := killPID(999999999)
	if err == nil {
		return
	}
	if err != syscall.ESRCH {
		t.Fatalf("expected ESRCH or nil for a nonexistent pid, got %v", err)
	}
}

// TestKillPID_ZeroOrNegativeIsNoOp guards against accidentally signaling
// pid 0 (the caller's own process group) or a negative pid.
func TestKillPID_ZeroOrNegativeIsNoOp(t *testing.T) {
	if err := killPID(0); err != nil {
		t.Fatalf("killPID(0) should be a no-op, got %v", err)
	}
	if err := killPID(-1); err != nil {
		t.Fatalf("killPID(-1) should be a no-op, got %v", err)
	}
}
