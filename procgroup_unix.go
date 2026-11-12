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

// killPID signals the daemon process started by "connect" for
// runDisconnect, with the exact same signal and scope as the shell pipeline
// it replaces (`kill $(cat "$dataDir/$name.pid")` - SIGTERM, that one pid
// only). Deliberately NOT a process-group kill: unlike a running "aux4 exec"
// child (which setProcessGroup/killProcessGroup put in and tear down as a
// whole group because it is a short-lived, uniquely-identified child this
// same process just started), a pid read back out of a pid file on disk can,
// after the daemon has long since exited, have been reused by a completely
// unrelated process by the time disconnect runs - verified empirically: an
// earlier version of this function that resolved and signaled -pgid instead
// of the bare pid took down an unrelated process group sharing that
// recycled pid during a real test run. Signaling only the exact pid bounds
// the blast radius to "this one process, if it is even still the one we
// started" - the same bound the original `kill <pid>` always had.
func killPID(pid int) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}
