package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestDisconnectPidFile_NoPidFile exercises the "never connected" case: no
// pid file at all. Must be a silent no-op (no panic, nothing left behind).
func TestDisconnectPidFile_NoPidFile(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "never-connected.pid")

	disconnectPidFile(pidFile)

	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatalf("expected pid file to not exist, got err=%v", err)
	}
}

// TestDisconnectPidFile_StalePid exercises a pid file pointing at a pid that
// does not exist - must not error and must still remove the file.
func TestDisconnectPidFile_StalePid(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "stale.pid")
	if err := os.WriteFile(pidFile, []byte("999999999"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	disconnectPidFile(pidFile)

	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatalf("expected pid file to be removed, got err=%v", err)
	}
}

// TestDisconnectPidFile_NonNumericPid covers a corrupted pid file - treated
// the same as the old shell's `kill $(cat ...)` failing and being swallowed
// by `|| true`: no panic, file still removed.
func TestDisconnectPidFile_NonNumericPid(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "corrupt.pid")
	if err := os.WriteFile(pidFile, []byte("not-a-pid"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	disconnectPidFile(pidFile)

	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatalf("expected pid file to be removed, got err=%v", err)
	}
}

// TestDisconnectPidFile_LiveProcess covers the common case: a real running
// process is killed and the pid file is removed.
func TestDisconnectPidFile_LiveProcess(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start sleep: %v", err)
	}
	pid := cmd.Process.Pid

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "live.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	disconnectPidFile(pidFile)

	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatalf("expected pid file to be removed, got err=%v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		// process exited - good.
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("process %d was not killed by disconnectPidFile", pid)
	}
}
