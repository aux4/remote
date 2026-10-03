package main

import (
	"encoding/json"
	"fmt"
	"os"
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
		// call-auto <server> <argsJSON> <commandLine>  — picks the positional
		// (argsJSON) or --command (commandLine) form depending on which one
		// is actually set, so the .aux4 layer never has to branch between two
		// separate invocations (a conditional "A || B" there risks running
		// both when A legitimately exits non-zero).
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote call-auto <server> <argsJSON> <commandLine>")
			os.Exit(1)
		}
		server, argsJSON, commandLine := args[0], args[1], args[2]
		var argv []string
		if strings.TrimSpace(commandLine) != "" {
			argv = splitCommandLine(commandLine)
		} else if err := json.Unmarshal([]byte(argsJSON), &argv); err != nil {
			argv = nil
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
