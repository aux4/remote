package main

// call.go is the caller side: a short-lived websocket connection used by
// `aux4 remote <machine> <cmd...>` to run one command on a target machine and
// stream its output back, and by `aux4 aux4 remote list` to ask the relay
// which machines are currently connected.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const callTimeout = 120 * time.Second
// exitGracePeriod absorbs ordinary arrival-order jitter between "output" and
// "exit" (two independent messages, each pushed to the caller by its own
// relay-side handler invocation - see cloud-remote's server.go package
// comment). It does NOT need to cover a relay losing track of the exec
// entirely: the relay deliberately keeps a completed exec's pending-record
// around (rather than deleting it the moment "exit" is processed) so a
// late-processed "output" can still be routed correctly however long it
// takes - see the "exit" case in cloud-remote's handleMessage. This window
// only needs to be long enough for an output push that's already in flight
// to land.
const exitGracePeriod = 400 * time.Millisecond

// drainTrailingOutput reads any further messages for id that arrive within a
// short grace window and prints their output, to absorb the relay's
// output-before-exit ordering race (see runCall).
func drainTrailingOutput(conn *websocket.Conn, id string) {
	deadline := time.Now().Add(exitGracePeriod)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		_ = conn.SetReadDeadline(deadline)
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg execMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.ID != "" && msg.ID != id {
			continue
		}
		if msg.Action != "output" {
			continue
		}
		if msg.Stream == "stderr" {
			fmt.Fprint(os.Stderr, msg.Data)
		} else {
			fmt.Fprint(os.Stdout, msg.Data)
		}
	}
}

func dialCaller(serverURL, token string) (*websocket.Conn, error) {
	wsURL, err := buildWSURL(serverURL)
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(maxWSMessageBytes)
	if err := awaitReady(conn, token); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// awaitReady blocks until the relay confirms token is valid. A caller is
// stateless on the relay - "hello" carries the SAME bearer token sent on the
// websocket handshake again in the message body, which the relay validates
// per message instead of looking this connection up in its registry (a
// $connect-written registration can land on a different concurrent relay
// instance and never become visible to the message handler, which used to
// make this poll for up to 8s and still often time out). The relay replies
// "ready" on success or an "exit" carrying an error on an invalid token.
func awaitReady(conn *websocket.Conn, token string) error {
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteJSON(execMessage{Action: "hello", Token: token}); err != nil {
		return fmt.Errorf("cannot reach relay server: %w", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("no response from relay server: %w", err)
	}
	var msg execMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("unexpected response from relay server while connecting")
	}
	switch msg.Action {
	case "ready":
		return nil
	case "exit":
		if msg.Error != "" {
			return fmt.Errorf("%s", msg.Error)
		}
		return fmt.Errorf("relay server rejected the connection")
	default:
		return fmt.Errorf("unexpected response from relay server while connecting")
	}
}

// runCall executes args on machine and exits the process with the remote
// exit code (never returns on success).
func runCall(serverURL, machine string, args []string) {
	token, err := accessToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	conn, err := dialCaller(serverURL, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot reach relay server: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	id := uuid.NewString()
	if err := conn.WriteJSON(execMessage{Action: "exec", Target: machine, ID: id, Args: args, Token: token}); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to send command: %v\n", err)
		os.Exit(1)
	}

	deadline := time.Now().Add(callTimeout)
	for {
		_ = conn.SetReadDeadline(deadline)
		_, data, err := conn.ReadMessage()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: lost connection to relay server: %v\n", err)
			os.Exit(1)
		}

		var msg execMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.ID != "" && msg.ID != id {
			continue
		}

		switch msg.Action {
		case "output":
			if msg.Stream == "stderr" {
				fmt.Fprint(os.Stderr, msg.Data)
			} else {
				fmt.Fprint(os.Stdout, msg.Data)
			}
		case "exit":
			// The relay dispatches "output" and "exit" as independent
			// messages, each handled by its own short-lived process on the
			// relay side, so arrival order relative to each other is not
			// guaranteed even though the target sent them in order. Give any
			// trailing "output" a brief grace window to arrive before acting
			// on "exit", instead of exiting immediately and risking losing
			// the tail of the command's output.
			drainTrailingOutput(conn, id)
			if msg.Error != "" {
				fmt.Fprintf(os.Stderr, "Error: %s\n", msg.Error)
			}
			os.Exit(msg.Code)
		}
	}
}

// runList prints the currently connected machine names, one per line.
func runList(serverURL string) {
	token, err := accessToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	conn, err := dialCaller(serverURL, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot reach relay server: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	if err := conn.WriteJSON(execMessage{Action: "list", Token: token}); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to request machine list: %v\n", err)
		os.Exit(1)
	}

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: no response from relay server: %v\n", err)
		os.Exit(1)
	}

	var reply execMessage
	if err := json.Unmarshal(data, &reply); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response from relay server\n")
		os.Exit(1)
	}
	if reply.Action == "exit" {
		if reply.Error != "" {
			fmt.Fprintf(os.Stderr, "Error: %s\n", reply.Error)
		} else {
			fmt.Fprintln(os.Stderr, "Error: relay server rejected the request")
		}
		os.Exit(1)
	}

	if len(reply.Machines) == 0 {
		fmt.Println("No machines connected")
		return
	}
	for _, m := range reply.Machines {
		fmt.Println(m)
	}
}
