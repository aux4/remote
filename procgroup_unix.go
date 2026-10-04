//go:build !windows

package main

// procgroup_unix.go isolates the POSIX-only syscalls runExec needs to kill a
// whole process group (the aux4 command and anything it spawned), not just
// the direct child, on timeout or when the relay connection drops.

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts cmd's child in its own process group so
// killProcessGroup can signal every descendant at once.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to cmd's entire process group. Falls back to
// killing just the direct process if the group id cannot be resolved (e.g.
// the process already exited).
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return cmd.Process.Kill()
	}
	return syscall.Kill(-pgid, syscall.SIGKILL)
}

// detachNewSession starts cmd in a new session so it keeps running after the
// short-lived "connect" launcher process that started it exits - the Go
// equivalent of the shell's `nohup ... &`.
func detachNewSession(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
