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
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(buildWSURL(serverURL), header)
	return conn, err
}

// runCall executes args on machine and exits the process with the remote
// exit code (never returns on success).
func runCall(serverURL, token, machine string, args []string) {
	conn, err := dialCaller(serverURL, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot reach relay server: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	id := uuid.NewString()
	if err := conn.WriteJSON(execMessage{Action: "exec", Target: machine, ID: id, Args: args}); err != nil {
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

type listReply struct {
	Action   string   `json:"action"`
	Machines []string `json:"machines"`
}

// runList prints the currently connected machine names, one per line.
func runList(serverURL, token string) {
	conn, err := dialCaller(serverURL, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot reach relay server: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	if err := conn.WriteJSON(execMessage{Action: "list"}); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to request machine list: %v\n", err)
		os.Exit(1)
	}

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: no response from relay server: %v\n", err)
		os.Exit(1)
	}

	var reply listReply
	if err := json.Unmarshal(data, &reply); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response from relay server\n")
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

