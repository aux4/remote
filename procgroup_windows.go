//go:build windows

package main

// procgroup_windows.go is the Windows counterpart to procgroup_unix.go.
// Windows has no POSIX process-group signal; killing the direct process is
// the best effort available here without additional job-object plumbing.

import "os/exec"

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
