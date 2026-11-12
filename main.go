package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: aux4-remote <command> [args...]")
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "client":
		// client <name> <server> <exposeJSON>
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote client <name> <server> <exposeJSON>")
			os.Exit(1)
		}
		runClient(args[0], args[1], args[2])

	case "connect":
		// connect <dataDir> <name> <server> <exposeJSON> — validates name,
		// creates dataDir (0700) and the pid/log files (0600) itself instead
		// of relying on the .aux4 layer to interpolate dataDir/name into raw
		// shell commands, and launches the "client" daemon detached.
		if len(args) < 4 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote connect <dataDir> <name> <server> <exposeJSON>")
			os.Exit(1)
		}
		runConnect(args[0], args[1], args[2], args[3])

	case "call":
		// call <server> <machine> <args...>
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote call <server> <machine> <cmd...>")
			os.Exit(1)
		}
		runCall(args[0], args[1], args[2:])

	case "call-line":
		// call-line <server> <commandLine>  — commandLine is a shell-quoted
		// string (machine + command), for callers whose command needs
		// single-dash flags that aux4's own arg parser would otherwise
		// swallow as its own options.
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote call-line <server> <commandLine>")
			os.Exit(1)
		}
		argv := splitCommandLine(args[1])
		if len(argv) == 0 {
			fmt.Fprintln(os.Stderr, "Error: no target machine specified")
			os.Exit(1)
		}
		if len(argv) < 2 {
			fmt.Fprintln(os.Stderr, "Error: no command specified")
			os.Exit(1)
		}
		runCall(args[0], argv[0], argv[1:])

	case "call-auto":
		// call-auto <server> <argsJSON> <commandLine> [extraParamsJSON]  —
		// picks the positional (argsJSON) or --command (commandLine) form
		// depending on which one is actually set, so the .aux4 layer never has
		// to branch between two separate invocations (a conditional "A || B"
		// there risks running both when A legitimately exits non-zero).
		//
		// extraParamsJSON, when present, is value(*) from the .aux4 layer -
		// every `--flag` the caller passed that is not one of remote's own
		// flags. In the positional form (no --command) each such flag is
		// forwarded to the remote aux4 command as its own "--flag value" pair,
		// mirroring aux4/cloud's "every other flag is forwarded to the remote
		// command" UX. It is ignored with --command, whose shell-quoted string
		// is already the caller's complete, explicit command line.
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote call-auto <server> <argsJSON> <commandLine> [extraParamsJSON]")
			os.Exit(1)
		}
		server, argsJSON, commandLine := args[0], args[1], args[2]
		extraParamsJSON := ""
		if len(args) > 3 {
			extraParamsJSON = args[3]
		}
		var argv []string
		if strings.TrimSpace(commandLine) != "" {
			argv = splitCommandLine(commandLine)
		} else if err := json.Unmarshal([]byte(argsJSON), &argv); err != nil {
			argv = nil
		} else {
			argv = append(argv, forwardedFlags(extraParamsJSON)...)
		}
		if len(argv) == 0 {
			fmt.Fprintln(os.Stderr, "Error: no target machine specified")
			os.Exit(1)
		}
		if len(argv) < 2 {
			fmt.Fprintln(os.Stderr, "Error: no command specified")
			os.Exit(1)
		}
		runCall(server, argv[0], argv[1:])

	case "call-json":
		// call-json <server> <argsJSON>  — argsJSON is ["machine","cmd","arg1",...]
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote call-json <server> <argsJSON>")
			os.Exit(1)
		}
		var argv []string
		if err := json.Unmarshal([]byte(args[1]), &argv); err != nil || len(argv) == 0 {
			fmt.Fprintln(os.Stderr, "Error: no target machine specified")
			os.Exit(1)
		}
		if len(argv) < 2 {
			fmt.Fprintln(os.Stderr, "Error: no command specified")
			os.Exit(1)
		}
		runCall(args[0], argv[0], argv[1:])

	case "status":
		// status <pidFile> <name>
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote status <pidFile> <name>")
			os.Exit(1)
		}
		runStatus(args[0], args[1])

	case "disconnect":
		// disconnect <dataDir> <name> — validates name itself instead of
		// relying on the .aux4 layer to interpolate dataDir/name into a raw
		// `kill $(cat ...)` shell command.
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote disconnect <dataDir> <name>")
			os.Exit(1)
		}
		runDisconnect(args[0], args[1])

	case "machine-list":
		// machine-list <server>
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote machine-list <server>")
			os.Exit(1)
		}
		runList(args[0])

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

// runConnect validates name, creates dataDir and the pid/log files with
// restrictive permissions, and launches "aux4-remote client" detached from
// this short-lived process - the Go equivalent of the shell's
// `mkdir -p "$dataDir" && nohup ... > "$log" 2>&1 & echo $! > "$pid"`, done
// without ever building a shell command line out of dataDir/name (closing the
// injection risk of interpolating them unquoted into a shell string) and
// without ever reading dataDir/name back out of a path that could have been
// built from a `..`-containing or slash-containing name.
func runConnect(dataDir, name, server, exposeJSON string) {
	if err := validateName(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	// Validated here (not just inside the spawned "client" daemon) so a
	// rejected ws:// URL is reported directly to the caller, with a non-zero
	// exit, instead of silently failing in the background daemon's log file
	// while this launcher reports a misleading "Connecting..." success.
	if _, err := buildWSURL(server); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// 0700: only this user can see or traverse the pid/log files below, which
	// can reveal command output and the machine's own access token refresh
	// activity via log contents.
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot create %s: %v\n", dataDir, err)
		os.Exit(1)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot set permissions on %s: %v\n", dataDir, err)
		os.Exit(1)
	}

	// name is already validated above (no "/", no "..", no control chars), so
	// joining it with dataDir cannot escape dataDir.
	logFile := filepath.Join(dataDir, name+".log")
	pidFile := filepath.Join(dataDir, name+".pid")

	log, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot open %s: %v\n", logFile, err)
		os.Exit(1)
	}
	defer log.Close()

	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	cmd := exec.Command(self, "client", name, server, exposeJSON)
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.Stdin = nil
	detachNewSession(cmd)

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to start the client daemon: %v\n", err)
		os.Exit(1)
	}
	// Process.Release() sets Process.Pid to -1, so the real pid must be
	// captured before releasing.
	pid := cmd.Process.Pid

	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot write %s: %v\n", pidFile, err)
		os.Exit(1)
	}

	// This launcher process exits right after starting the daemon; it does
	// not wait for or stay attached to it (the detached session keeps it
	// running), matching the previous `nohup ... &` behavior.
	_ = cmd.Process.Release()

	fmt.Printf("Connecting %s to %s (logs: %s)\n", name, server, logFile)
}

// runStatus reports whether the daemon referenced by pidFile is alive. It
// never mutates the pid file.
func runStatus(pidFile, name string) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		fmt.Printf("%s: disconnected\n", name)
		return
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		fmt.Printf("%s: disconnected\n", name)
		return
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		fmt.Printf("%s: disconnected\n", name)
		return
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		fmt.Printf("%s: disconnected\n", name)
		return
	}
	fmt.Printf("%s: connected (pid %d)\n", name, pid)
}

// runDisconnect stops the daemon referenced by <dataDir>/<name>.pid and
// removes the pid file, mirroring the previous .aux4-level
// `kill $(cat "$dataDir/$name.pid") 2>/dev/null || true; rm -f "$dataDir/$name.pid"`
// shell pipeline, but without ever building a shell command line out of
// dataDir/name: name is validated with the exact same rule connect/status
// use (validateName), the pid path is built with filepath.Join in Go, and
// the pid file's content is parsed as an integer here rather than handed to
// a shell's `$(cat ...)` substitution. Matches the prior behavior for valid
// input - a missing pid file, an unreadable/non-numeric pid, or a pid that is
// already gone are all treated the same way the old `|| true` swallowed
// them: no error, pid file removed, "Disconnected <name>" printed, exit 0.
func runDisconnect(dataDir, name string) {
	if err := validateName(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// name is already validated above (no "/", no "..", no control chars), so
	// joining it with dataDir cannot escape dataDir.
	disconnectPidFile(filepath.Join(dataDir, name+".pid"))

	fmt.Printf("Disconnected '%s'\n", name)
}

// disconnectPidFile does the actual work split out of runDisconnect so it is
// testable without the validateName/os.Exit control flow: best-effort kill
// whatever pid is recorded at pidFile, then remove the file. A missing file,
// unreadable/non-numeric content, or a pid that is already gone are all
// silently tolerated, same as the old shell pipeline's `kill ... || true`.
func disconnectPidFile(pidFile string) {
	if data, err := os.ReadFile(pidFile); err == nil {
		var pid int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil && pid > 0 {
			// Best effort, same as the old `kill ... 2>/dev/null || true`: a
			// pid that is already gone, or that this process has no
			// permission to signal, is not an error here.
			_ = killPID(pid)
		}
	}
	_ = os.Remove(pidFile)
}
