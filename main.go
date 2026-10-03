package main

import (
	"encoding/json"
	"fmt"
	"os"
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
		// client <name> <server> <token> <exposeJSON>
		if len(args) < 4 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote client <name> <server> <token> <exposeJSON>")
			os.Exit(1)
		}
		runClient(args[0], args[1], args[2], args[3])

	case "call":
		// call <server> <token> <machine> <args...>
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote call <server> <token> <machine> <cmd...>")
			os.Exit(1)
		}
		runCall(args[0], args[1], args[2], args[3:])

	case "call-line":
		// call-line <server> <token> <machine> <commandLine>  — commandLine is a
		// shell-quoted string (machine + command), for callers whose command
		// needs single-dash flags that aux4's own arg parser would otherwise
		// swallow as its own options.
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote call-line <server> <token> <commandLine>")
			os.Exit(1)
		}
		argv := splitCommandLine(args[2])
		if len(argv) == 0 {
			fmt.Fprintln(os.Stderr, "Error: no target machine specified")
			os.Exit(1)
		}
		if len(argv) < 2 {
			fmt.Fprintln(os.Stderr, "Error: no command specified")
			os.Exit(1)
		}
		runCall(args[0], args[1], argv[0], argv[1:])

	case "call-json":
		// call-json <server> <token> <argsJSON>  — argsJSON is ["machine","cmd","arg1",...]
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote call-json <server> <token> <argsJSON>")
			os.Exit(1)
		}
		var argv []string
		if err := json.Unmarshal([]byte(args[2]), &argv); err != nil || len(argv) == 0 {
			fmt.Fprintln(os.Stderr, "Error: no target machine specified")
			os.Exit(1)
		}
		if len(argv) < 2 {
			fmt.Fprintln(os.Stderr, "Error: no command specified")
			os.Exit(1)
		}
		runCall(args[0], args[1], argv[0], argv[1:])

	case "status":
		// status <pidFile> <name>
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote status <pidFile> <name>")
			os.Exit(1)
		}
		runStatus(args[0], args[1])

	case "machine-list":
		// machine-list <server> <token>
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: aux4-remote machine-list <server> <token>")
			os.Exit(1)
		}
		runList(args[0], args[1])

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
