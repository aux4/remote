package main

// execMessage is the websocket message shape exchanged between a caller, the
// relay server (aux4/cloud-remote), and a connected machine: an "exec"
// request, streamed "output" chunks, and a final "exit". It mirrors the same
// struct used on the relay side (aux4/cloud-remote), kept in sync by hand
// since the two packages are independent Go modules.
type execMessage struct {
	Action string   `json:"action"`
	Target string   `json:"target,omitempty"`
	From   string   `json:"from,omitempty"`
	ID     string   `json:"id,omitempty"`
	Args   []string `json:"args,omitempty"`
	Stream string   `json:"stream,omitempty"`
	Data   string   `json:"data,omitempty"`
	Code   int      `json:"code,omitempty"`
	Error  string   `json:"error,omitempty"`
	// Token is this caller's bearer token, sent on every "hello"/"exec"/
	// "list" message - the relay is stateless for callers and re-validates
	// the token (deriving the principal from it) per message instead of
	// looking the caller up by connectionId. Never sent by a machine.
	Token string `json:"token,omitempty"`
	// Machines is the reply payload for a "list" request.
	Machines []string `json:"machines,omitempty"`
}
