package main

// client.go is the machine-side daemon: it dials the relay server, registers
// under a machine name, and executes incoming "exec" messages as real argv
// (never a shell string) subject to the machine's local `expose` policy. It
// reconnects with backoff if the relay connection drops.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

func runClient(name, serverURL, exposeJSON string) {
	policy := parseExpose(exposeJSON)
	wsURL := buildWSURL(serverURL)

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

	var writeMu sync.Mutex
	done := make(chan struct{})

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

		go runExec(conn, &writeMu, policy, msg)
	}
}

func runExec(conn *websocket.Conn, writeMu *sync.Mutex, policy *exposePolicy, msg execMessage) {
	send := func(m execMessage) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.WriteJSON(m)
	}

	if len(msg.Args) == 0 {
		send(execMessage{Action: "exit", ID: msg.ID, Code: 1, Error: "empty command"})
		return
	}

	cmdLine := strings.Join(msg.Args, " ")
	if !isExposed(policy, cmdLine) {
		send(execMessage{Action: "exit", ID: msg.ID, Code: 126, Error: fmt.Sprintf("command %q is not permitted by this machine's expose policy", cmdLine)})
		return
	}

	cmd := exec.Command(msg.Args[0], msg.Args[1:]...)
	cmd.Env = append(os.Environ(), "AUX4_SECURITY="+toAux4Security(policy))

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
	stream := func(r io.Reader, name string) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			send(execMessage{Action: "output", ID: msg.ID, Stream: name, Data: scanner.Text() + "\n"})
		}
	}
	wg.Add(2)
	go stream(stdout, "stdout")
	go stream(stderr, "stderr")
	wg.Wait()

	code := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}

	send(execMessage{Action: "exit", ID: msg.ID, Code: code})
}

// buildWSURL normalizes a server URL the user configured (which may be
// ws://host:port or ws://host:port/ws) so both client and caller dial the
// same endpoint.
func buildWSURL(server string) string {
	u, err := url.Parse(server)
	if err != nil {
		return server
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/ws"
	}
	return u.String()
}
