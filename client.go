package main

// client.go is the machine-side daemon: it dials the relay server, registers
// under a machine name, and executes incoming "exec" messages by running the
// "aux4" binary resolved from PATH with msg.Args as ITS arguments (never a
// shell string, and never msg.Args[0] as the binary to exec) - this machine
// only ever runs aux4 commands, subject to the machine's local `expose`
// policy. It reconnects with backoff if the relay connection drops.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// maxConcurrentExecs bounds how many "exec" requests this machine runs at
// once. Without a cap, a relay (or anyone who can reach it) could flood a
// connected machine with exec requests and exhaust its process/file-descriptor
// table. Excess requests are rejected immediately with a clear message rather
// than queued, so a caller sees why its command did not run instead of
// hanging indefinitely.
const maxConcurrentExecs = 8

// defaultExecTimeout bounds how long a single exec is allowed to run before
// the daemon kills it (and its whole process group, so any children it
// spawned die too) and reports a timeout. Overridable via the
// AUX4_REMOTE_EXEC_TIMEOUT environment variable (a Go duration string, e.g.
// "30s", "2h").
const defaultExecTimeout = 10 * time.Minute

// execTimeout resolves the per-exec deadline from AUX4_REMOTE_EXEC_TIMEOUT
// (when set to a valid positive Go duration), falling back to
// defaultExecTimeout otherwise.
func execTimeout() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("AUX4_REMOTE_EXEC_TIMEOUT")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return defaultExecTimeout
}

// maxWSMessageBytes bounds the size of any single websocket frame this
// daemon will read, so a malicious or buggy relay cannot force unbounded
// memory growth by sending an oversized message.
const maxWSMessageBytes = 8 * 1024 * 1024

func runClient(name, serverURL, exposeJSON string) {
	if err := validateName(name); err != nil {
		fmt.Fprintf(os.Stderr, "remote: %v\n", err)
		os.Exit(1)
	}

	policy := parseExpose(exposeJSON)
	wsURL, err := buildWSURL(serverURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "remote: %v\n", err)
		os.Exit(1)
	}

	for {
		token, err := accessToken()
		if err != nil {
			fmt.Fprintf(os.Stderr, "remote: %v, retrying...\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		conn, err := dialMachine(name, wsURL, token)
		if err != nil {
			fmt.Fprintf(os.Stderr, "remote: connect failed: %v, retrying...\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		fmt.Printf("remote: connected to %s as %q\n", wsURL, name)
		serveMachine(conn, policy)
		fmt.Fprintln(os.Stderr, "remote: disconnected, reconnecting...")
		time.Sleep(3 * time.Second)
	}
}

// dialMachine fetches a fresh access token for every (re)connect attempt, so
// a long-lived daemon picks up a refreshed token automatically instead of
// authenticating forever with the token it started with.
func dialMachine(name, serverURL, token string) (*websocket.Conn, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	header.Set("X-Machine-Name", name)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(serverURL, header)
	return conn, err
}

// serveMachine reads messages off conn until the connection breaks. It
// returns when the connection is no longer usable so the caller can
// reconnect.
func serveMachine(conn *websocket.Conn, policy *exposePolicy) {
	defer conn.Close()
	// A malicious or buggy relay should not be able to force unbounded memory
	// growth by sending an oversized frame.
	conn.SetReadLimit(maxWSMessageBytes)

	var writeMu sync.Mutex
	done := make(chan struct{})
	// Bounds how many "exec" requests run at once, so a flood of exec
	// messages cannot exhaust this machine's process/file-descriptor table.
	sem := make(chan struct{}, maxConcurrentExecs)

	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				writeMu.Lock()
				err := conn.WriteMessage(websocket.PingMessage, nil)
				writeMu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			close(done)
			return
		}

		var msg execMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Action != "exec" {
			continue
		}

		select {
		case sem <- struct{}{}:
			go func(m execMessage) {
				defer func() { <-sem }()
				runExec(conn, &writeMu, policy, done, m)
			}(msg)
		default:
			// At capacity - reject immediately rather than queue, so the
			// caller sees a clear reason instead of a silent hang.
			writeMu.Lock()
			_ = conn.WriteJSON(execMessage{Action: "exit", ID: msg.ID, From: msg.From, Code: 1,
				Error: fmt.Sprintf("this machine is already running the maximum of %d concurrent commands; try again shortly", maxConcurrentExecs)})
			writeMu.Unlock()
		}
	}
}

func runExec(conn *websocket.Conn, writeMu *sync.Mutex, policy *exposePolicy, connDone <-chan struct{}, msg execMessage) {
	// The relay routes a machine's "output"/"exit" replies straight back to
	// the caller's connectionId rather than tracking a pending-exec entry of
	// its own; it gets that connectionId by trusting it echoed back exactly
	// as this machine received it on the "exec" message (From).
	send := func(m execMessage) {
		m.From = msg.From
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.WriteJSON(m)
	}

	if len(msg.Args) == 0 {
		send(execMessage{Action: "exit", ID: msg.ID, Code: 1, Error: "empty command"})
		return
	}

	// Reject args whose space-joined representation does not round-trip back
	// to the same argv - this closes a bypass where crafted whitespace (a
	// trailing space, an empty element, doubled internal whitespace) makes
	// the string used for policy matching diverge from what the argv the
	// policy was written to describe actually means. See validateArgs.
	if err := validateArgs(msg.Args); err != nil {
		send(execMessage{Action: "exit", ID: msg.ID, Code: 126, Error: err.Error()})
		return
	}

	cmdLine := strings.Join(msg.Args, " ")
	if !isExposed(policy, cmdLine) {
		send(execMessage{Action: "exit", ID: msg.ID, Code: 126, Error: fmt.Sprintf("aux4 command %q is not permitted by this machine's expose policy", cmdLine)})
		return
	}

	// The exec is bounded by a deadline (default 10 minutes, overridable via
	// AUX4_REMOTE_EXEC_TIMEOUT) and is also torn down if the relay connection
	// drops mid-run, so neither a runaway command nor an abandoned one (the
	// caller disconnected) can keep running - or keep this machine's exec
	// slot occupied - forever.
	connCtx, connCancel := context.WithCancel(context.Background())
	defer connCancel()
	go func() {
		select {
		case <-connDone:
			connCancel()
		case <-connCtx.Done():
		}
	}()
	ctx, cancel := context.WithTimeout(connCtx, execTimeout())
	defer cancel()

	// Always run the "aux4" binary resolved from PATH, never msg.Args[0] - a
	// caller only ever supplies arguments TO aux4, never a binary to exec
	// directly. This makes it impossible to run anything other than an aux4
	// command through this path, regardless of what the expose policy allows.
	cmd := exec.CommandContext(ctx, "aux4", msg.Args...)
	cmd.Env = append(os.Environ(), "AUX4_SECURITY="+toAux4Security(policy))
	// Run the child in its own process group and kill the whole group (not
	// just the direct child) on timeout or disconnect, so anything the aux4
	// command itself spawned is also torn down - never left running
	// unsupervised after this machine stops tracking it.
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	cmd.WaitDelay = 5 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		send(execMessage{Action: "exit", ID: msg.ID, Code: 1, Error: err.Error()})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		send(execMessage{Action: "exit", ID: msg.ID, Code: 1, Error: err.Error()})
		return
	}

	if err := cmd.Start(); err != nil {
		send(execMessage{Action: "exit", ID: msg.ID, Code: 1, Error: err.Error()})
		return
	}

	var wg sync.WaitGroup
	// Read with a growing buffer instead of bufio.Scanner's fixed maximum
	// token size, so a single line longer than any fixed cap is streamed in
	// full rather than silently truncating the command's output and leaving
	// scanner.Err() (ErrTooLong) unchecked. A real read error on the pipe is
	// also surfaced to the caller instead of just stopping silently.
	stream := func(r io.Reader, name string) {
		defer wg.Done()
		reader := bufio.NewReaderSize(r, 64*1024)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				send(execMessage{Action: "output", ID: msg.ID, Stream: name, Data: line})
			}
			if err != nil {
				if err != io.EOF {
					send(execMessage{Action: "output", ID: msg.ID, Stream: name, Data: fmt.Sprintf("[remote: error reading %s: %v]\n", name, err)})
				}
				return
			}
		}
	}
	wg.Add(2)
	go stream(stdout, "stdout")
	go stream(stderr, "stderr")
	wg.Wait()

	code := 0
	waitErr := cmd.Wait()
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}

	errMsg := ""
	if ctx.Err() == context.DeadlineExceeded {
		errMsg = fmt.Sprintf("command timed out after %s and was killed", execTimeout())
		code = 124
	}

	send(execMessage{Action: "exit", ID: msg.ID, Code: code, Error: errMsg})
}

// buildWSURL normalizes a server URL the user configured (which may be
// ws://host:port or ws://host:port/ws) so both client and caller dial the
// same endpoint. ws:// (cleartext) is only permitted to a loopback host
// (localhost/127.0.0.1/::1) - anywhere else, a relay operator or anyone on
// the network path could read every command and its output (including
// access tokens never appearing in it, but full command output), so a
// non-loopback server URL must use wss:// (TLS).
func buildWSURL(server string) (string, error) {
	u, err := url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("invalid server URL %q: %w", server, err)
	}
	switch u.Scheme {
	case "wss":
		// Always fine - encrypted in transit regardless of host.
	case "ws":
		if !isLoopbackHost(u.Hostname()) {
			return "", fmt.Errorf("ws:// is only permitted when the server is a loopback host (localhost/127.0.0.1/::1); use wss:// for %q", u.Hostname())
		}
	default:
		return "", fmt.Errorf("unsupported server URL scheme %q: use ws:// (loopback only) or wss://", u.Scheme)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/ws"
	}
	return u.String(), nil
}

// isLoopbackHost reports whether host (as parsed from a URL, so IPv6
// addresses arrive without their brackets) refers to the local machine.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
