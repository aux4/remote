//go:build windows

package main

// procgroup_windows.go is the Windows counterpart to procgroup_unix.go.
// Windows has no POSIX process-group signal; killing the direct process is
// the best effort available here without additional job-object plumbing.

import (
	"os"
	"os/exec"
)

func setProcessGroup(cmd *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// detachNewSession is a no-op on Windows; a started process is not tied to
// the launcher's console in the same way POSIX sessions work, and this
// package does not depend on true session detachment there.
func detachNewSession(cmd *exec.Cmd) {}

// killPID is the Windows counterpart to the unix killPID: there is no POSIX
// process-group signal available, so this is the same best-effort "kill the
// direct process" killProcessGroup already falls back to on this platform.
func killPID(pid int) error {
	if pid <= 0 {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
