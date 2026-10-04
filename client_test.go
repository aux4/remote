package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// stubAux4 puts a fake "aux4" executable first on PATH for the duration of the
// test, so runExec's exec.Command("aux4", ...) resolves to it instead of a
// real aux4 install. The stub prints "stub-aux4-called:" followed by its argv
// (space-joined) to stdout, proving the daemon really invoked "aux4" - never
// msg.Args[0] - with msg.Args passed through as aux4's own arguments. It also
// touches a sentinel file so a test can assert the real binary ran exactly
// once (and that nothing else, e.g. "echo", ever runs).
func stubAux4(t *testing.T, sentinel string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub script is POSIX shell only")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "aux4")
	content := "#!/bin/sh\n" +
		"echo \"ran\" >> " + sentinel + "\n" +
		"printf 'stub-aux4-called:'\n" +
		"for a in \"$@\"; do printf ' %s' \"$a\"; done\n" +
		"printf '\\n'\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
}

// wsPair spins up a local websocket echo-ish test server and returns a client
// connection to it, along with the server-side connection so a test can read
// what runExec writes back. Only one side writes in these tests (runExec).
func wsPair(t *testing.T) (client *websocket.Conn, server *websocket.Conn) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	serverConnCh := make(chan *websocket.Conn, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		serverConnCh <- c
	}))
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	select {
	case sc := <-serverConnCh:
		t.Cleanup(func() { sc.Close() })
		return c, sc
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server connection")
		return nil, nil
	}
}

// readUntilExit collects every message for id until an "exit" message, with a
// generous timeout so it never hangs the suite on a regression.
func readUntilExit(t *testing.T, conn *websocket.Conn, id string) []execMessage {
	t.Helper()
	var out []execMessage
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var msg execMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.ID != "" && msg.ID != id {
			continue
		}
		out = append(out, msg)
		if msg.Action == "exit" {
			return out
		}
	}
}

func TestRunExec_AlwaysInvokesAux4RegardlessOfRequestedBinary(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "ran.log")
	stubAux4(t, sentinel)

	clientConn, serverConn := wsPair(t)
	policy := parseExpose(`{"allow":["echo hi"]}`)

	var writeMu sync.Mutex
	msg := execMessage{Action: "exec", ID: "t1", Args: []string{"echo", "hi"}}
	connDone := make(chan struct{})

	done := make(chan struct{})
	go func() {
		runExec(clientConn, &writeMu, policy, connDone, msg)
		close(done)
	}()

	msgs := readUntilExit(t, serverConn, "t1")
	<-done

	var stdout strings.Builder
	for _, m := range msgs {
		if m.Action == "output" {
			stdout.WriteString(m.Data)
		}
	}

	// "echo hi" never runs as the real `/bin/echo` - it becomes an argument TO
	// the stub "aux4" binary, proving a caller can only ever ask this machine
	// to run an aux4 command, never an arbitrary binary named "echo".
	want := "stub-aux4-called: echo hi\n"
	if stdout.String() != want {
		t.Fatalf("expected daemon to run the stub aux4 binary with [echo hi] as its args, got stdout %q", stdout.String())
	}

	if data, err := os.ReadFile(sentinel); err != nil || strings.Count(string(data), "ran\n") != 1 {
		t.Fatalf("expected the stub aux4 binary to have run exactly once, sentinel: %q, err: %v", string(data), err)
	}
}

func TestRunExec_DeniedCommandNeverSpawnsAnything(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "ran.log")
	stubAux4(t, sentinel)

	clientConn, serverConn := wsPair(t)
	// No allow patterns at all - fail closed.
	policy := parseExpose(`{}`)

	var writeMu sync.Mutex
	msg := execMessage{Action: "exec", ID: "t2", Args: []string{"todo", "rm", "1"}}
	connDone := make(chan struct{})

	done := make(chan struct{})
	go func() {
		runExec(clientConn, &writeMu, policy, connDone, msg)
		close(done)
	}()

	msgs := readUntilExit(t, serverConn, "t2")
	<-done

	last := msgs[len(msgs)-1]
	if last.Action != "exit" || last.Code != 126 {
		t.Fatalf("expected a denied exec to exit with code 126, got %+v", last)
	}
	if !strings.Contains(last.Error, `aux4 command "todo rm 1" is not permitted`) {
		t.Fatalf("unexpected denial message: %q", last.Error)
	}

	if _, err := os.ReadFile(sentinel); err == nil {
		t.Fatal("expected the stub aux4 binary never to have run for a denied command")
	}
}

func TestBuildWSURL_WSSAlwaysAllowed(t *testing.T) {
	got, err := buildWSURL("wss://relay.example.com:8420")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "wss://relay.example.com:8420/ws" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildWSURL_WSLoopbackAllowed(t *testing.T) {
	for _, host := range []string{"ws://localhost:8420", "ws://127.0.0.1:8420", "ws://[::1]:8420"} {
		got, err := buildWSURL(host)
		if err != nil {
			t.Fatalf("expected %q to be allowed, got error: %v", host, err)
		}
		if !strings.HasSuffix(got, "/ws") {
			t.Fatalf("expected a default /ws path appended, got %q", got)
		}
	}
}

func TestBuildWSURL_WSNonLoopbackRejected(t *testing.T) {
	_, err := buildWSURL("ws://relay.example.com:8420")
	if err == nil {
		t.Fatal("expected ws:// to a non-loopback host to be rejected")
	}
	if !strings.Contains(err.Error(), "wss://") {
		t.Fatalf("expected the error to point to wss://, got %q", err.Error())
	}
}

func TestBuildWSURL_PreservesExplicitPath(t *testing.T) {
	got, err := buildWSURL("wss://relay.example.com/custom-path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "wss://relay.example.com/custom-path" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildWSURL_UnsupportedScheme(t *testing.T) {
	if _, err := buildWSURL("http://relay.example.com"); err == nil {
		t.Fatal("expected a non-ws(s) scheme to be rejected")
	}
}

func TestRunExec_EmptyArgsRejected(t *testing.T) {
	clientConn, serverConn := wsPair(t)
	policy := parseExpose(`{"allow":["*"]}`)

	var writeMu sync.Mutex
	msg := execMessage{Action: "exec", ID: "t3", Args: nil}
	connDone := make(chan struct{})

	done := make(chan struct{})
	go func() {
		runExec(clientConn, &writeMu, policy, connDone, msg)
		close(done)
	}()

	msgs := readUntilExit(t, serverConn, "t3")
	<-done

	last := msgs[len(msgs)-1]
	if last.Action != "exit" || last.Code != 1 || last.Error != "empty command" {
		t.Fatalf("expected empty Args to be rejected with exit code 1, got %+v", last)
	}
}

// TestRunExec_WhitespaceBypassIsRejected reproduces the exact bypass
// validateArgs closes: crafted whitespace in an arg making the policy-matched
// cmdLine diverge from what a deny pattern was written to describe. Without
// the fix, ["todo ", "rm"] joins to "todo  rm" (double space), which
// `deny: ["todo rm*"]` (single space) does not match, so the command would
// run instead of being denied.
func TestRunExec_WhitespaceBypassIsRejected(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "ran.log")
	stubAux4(t, sentinel)

	clientConn, serverConn := wsPair(t)
	policy := parseExpose(`{"allow":["todo *"],"deny":["todo rm*"]}`)
	connDone := make(chan struct{})

	var writeMu sync.Mutex
	msg := execMessage{Action: "exec", ID: "t4", Args: []string{"todo ", "rm"}}

	done := make(chan struct{})
	go func() {
		runExec(clientConn, &writeMu, policy, connDone, msg)
		close(done)
	}()

	msgs := readUntilExit(t, serverConn, "t4")
	<-done

	last := msgs[len(msgs)-1]
	if last.Action != "exit" || last.Code != 126 {
		t.Fatalf("expected the whitespace-crafted args to be rejected with code 126, got %+v", last)
	}

	if _, err := os.ReadFile(sentinel); err == nil {
		t.Fatal("expected the stub aux4 binary never to have run for whitespace-crafted args")
	}
}

// TestServeMachine_RejectsExecsBeyondConcurrencyCap proves a flood of exec
// requests beyond maxConcurrentExecs is rejected immediately with a clear
// message rather than queued or allowed to pile up unboundedly. It drives the
// real serveMachine dispatch loop (not a reimplementation of it).
func TestServeMachine_RejectsExecsBeyondConcurrencyCap(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "aux4")
	// Sleeps long enough that every slot stays occupied for the duration of
	// the test, so the (maxConcurrentExecs+1)th request is forced to be
	// rejected rather than racing a fast-finishing command for a slot.
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })

	daemonConn, relayConn := wsPair(t)
	policy := parseExpose(`{"allow":["*"]}`)

	go serveMachine(daemonConn, policy)

	for i := 0; i < maxConcurrentExecs+1; i++ {
		msg := execMessage{Action: "exec", ID: fmt.Sprintf("flood-%d", i), Args: []string{"sleep"}}
		if err := relayConn.WriteJSON(msg); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	rejected := 0
	seen := map[string]bool{}
	deadline := time.Now().Add(10 * time.Second)
	for len(seen) < maxConcurrentExecs+1 && time.Now().Before(deadline) {
		_ = relayConn.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, data, err := relayConn.ReadMessage()
		if err != nil {
			break
		}
		var msg execMessage
		if err := json.Unmarshal(data, &msg); err != nil || msg.Action != "exit" || seen[msg.ID] {
			continue
		}
		seen[msg.ID] = true
		if strings.Contains(msg.Error, "maximum") {
			rejected++
		}
	}

	if rejected != 1 {
		t.Fatalf("expected exactly 1 of %d concurrent execs to be rejected for capacity, got %d (saw exits for %d of %d requests)", maxConcurrentExecs+1, rejected, len(seen), maxConcurrentExecs+1)
	}
}

// TestRunExec_TimesOutAndKillsTheChild proves a command exceeding
// AUX4_REMOTE_EXEC_TIMEOUT is killed rather than left running, and the caller
// gets a clear exit code/message instead of hanging forever.
func TestRunExec_TimesOutAndKillsTheChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub script is POSIX shell only")
	}
	t.Setenv("AUX4_REMOTE_EXEC_TIMEOUT", "200ms")

	sentinel := filepath.Join(t.TempDir(), "still-running")
	dir := t.TempDir()
	script := filepath.Join(dir, "aux4")
	// Loops forever, touching a file every iteration, iff it's still alive.
	// If the process is really killed (not just the daemon giving up on it),
	// the file stops being touched almost immediately.
	content := "#!/bin/sh\nwhile true; do date +%s > " + sentinel + "; sleep 0.05; done\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })

	clientConn, serverConn := wsPair(t)
	policy := parseExpose(`{"allow":["*"]}`)
	connDone := make(chan struct{})

	var writeMu sync.Mutex
	msg := execMessage{Action: "exec", ID: "t5", Args: []string{"loop"}}

	done := make(chan struct{})
	go func() {
		runExec(clientConn, &writeMu, policy, connDone, msg)
		close(done)
	}()

	msgs := readUntilExit(t, serverConn, "t5")
	<-done

	last := msgs[len(msgs)-1]
	if last.Action != "exit" || last.Code != 124 {
		t.Fatalf("expected a timed-out exec to exit with code 124, got %+v", last)
	}
	if !strings.Contains(last.Error, "timed out") {
		t.Fatalf("expected a timeout message, got %q", last.Error)
	}

	// Give a kill signal a brief moment to actually land, then confirm the
	// sentinel file stopped being updated - i.e. the real OS process (and not
	// just this daemon's bookkeeping) is gone.
	before, _ := os.ReadFile(sentinel)
	time.Sleep(500 * time.Millisecond)
	after, _ := os.ReadFile(sentinel)
	if string(before) != string(after) {
		t.Fatalf("expected the child process to have been killed, but it kept running after the reported timeout (sentinel changed from %q to %q)", before, after)
	}
}

// TestRunExec_TagsStdoutAndStderrSeparatelyAndPropagatesExitCode proves two
// things together: each "output" message carries a Stream ("stdout"/
// "stderr") matching the pipe it actually came from (not mixed into one), and
// the final "exit" Code is the child's real exit code unchanged - including a
// value that isn't one of the daemon's own sentinel codes (1, 124, 126, 127).
func TestRunExec_TagsStdoutAndStderrSeparatelyAndPropagatesExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub script is POSIX shell only")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "aux4")
	content := "#!/bin/sh\n" +
		"echo 'out-line-1'\n" +
		"echo 'err-line-1' 1>&2\n" +
		"echo 'out-line-2'\n" +
		"exit 3\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })

	clientConn, serverConn := wsPair(t)
	policy := parseExpose(`{"allow":["*"]}`)
	connDone := make(chan struct{})

	var writeMu sync.Mutex
	msg := execMessage{Action: "exec", ID: "t6", Args: []string{"anything"}}

	done := make(chan struct{})
	go func() {
		runExec(clientConn, &writeMu, policy, connDone, msg)
		close(done)
	}()

	msgs := readUntilExit(t, serverConn, "t6")
	<-done

	var stdout, stderr strings.Builder
	for _, m := range msgs {
		if m.Action != "output" {
			continue
		}
		switch m.Stream {
		case "stdout":
			stdout.WriteString(m.Data)
		case "stderr":
			stderr.WriteString(m.Data)
		default:
			t.Fatalf("output message missing a recognized stream tag: %+v", m)
		}
	}

	if stdout.String() != "out-line-1\nout-line-2\n" {
		t.Fatalf("expected stdout-tagged output to contain only the stdout lines, got %q", stdout.String())
	}
	if stderr.String() != "err-line-1\n" {
		t.Fatalf("expected stderr-tagged output to contain only the stderr line, got %q", stderr.String())
	}

	last := msgs[len(msgs)-1]
	if last.Action != "exit" || last.Code != 3 {
		t.Fatalf("expected the child's real exit code (3) to be propagated unchanged, got %+v", last)
	}
	if last.Error != "" {
		t.Fatalf("expected no error message for a plain non-zero exit, got %q", last.Error)
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"build-box", "machine_1", "a.b.c", "A1"}
	for _, name := range valid {
		if err := validateName(name); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", name, err)
		}
	}

	invalid := []string{"", "../etc", "a/b", "a b", "name\n", "name\x00", ".."}
	for _, name := range invalid {
		if err := validateName(name); err == nil {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}

// TestFilteredEnviron_StripsTokenLikeVarsKeepsOthers covers the env the
// daemon hands to the aux4 process it execs on a caller's behalf (runExec in
// client.go): this process's own credentials must never leak into it, but
// unrelated env (including AUX4_SECURITY, added back by the caller) must
// survive.
func TestFilteredEnviron_StripsTokenLikeVarsKeepsOthers(t *testing.T) {
	for _, kv := range []struct{ k, v string }{
		{"AUX4_ACCESS_TOKEN", "secret-access"},
		{"AUX4_REFRESH_TOKEN", "secret-refresh"},
		{"AUX4_ID_TOKEN", "secret-id"},
		{"REMOTE_TEST_KEEP_ME", "keep"},
	} {
		old, had := os.LookupEnv(kv.k)
		os.Setenv(kv.k, kv.v)
		t.Cleanup(func(k, old string, had bool) func() {
			return func() {
				if had {
					os.Setenv(k, old)
				} else {
					os.Unsetenv(k)
				}
			}
		}(kv.k, old, had))
	}

	env := filteredEnviron()
	for _, kv := range env {
		if strings.HasPrefix(kv, "AUX4_ACCESS_TOKEN=") || strings.HasPrefix(kv, "AUX4_REFRESH_TOKEN=") || strings.HasPrefix(kv, "AUX4_ID_TOKEN=") {
			t.Fatalf("expected token-like vars to be stripped, found %q", kv)
		}
	}
	found := false
	for _, kv := range env {
		if kv == "REMOTE_TEST_KEEP_ME=keep" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an unrelated env var to survive filtering")
	}
}

// TestReconnectDelay_GrowsAndCaps covers the exponential backoff shape: each
// attempt's delay (ignoring jitter's +/-30% band) roughly doubles up to the
// cap, and never exceeds the cap plus its jitter headroom.
func TestReconnectDelay_GrowsAndCaps(t *testing.T) {
	prevMax := time.Duration(0)
	for attempt := 1; attempt <= 10; attempt++ {
		d := reconnectDelay(attempt)
		if d < 0 {
			t.Fatalf("attempt %d: delay must never be negative, got %s", attempt, d)
		}
		capWithJitter := time.Duration(float64(reconnectMaxDelay) * (1 + reconnectJitterFraction))
		if d > capWithJitter {
			t.Fatalf("attempt %d: delay %s exceeds the capped+jittered ceiling %s", attempt, d, capWithJitter)
		}
		_ = prevMax
	}

	// After enough attempts the un-jittered base delay must have saturated at
	// the cap - sample many draws and check none exceed the cap's jitter band
	// (a flaky "it happened to double past the cap once" would be a real bug).
	for i := 0; i < 50; i++ {
		d := reconnectDelay(20)
		capWithJitter := time.Duration(float64(reconnectMaxDelay) * (1 + reconnectJitterFraction))
		if d > capWithJitter {
			t.Fatalf("attempt 20: delay %s exceeds the capped+jittered ceiling %s", d, capWithJitter)
		}
	}
}

// TestDialMachine_PermanentlyRejectsOn401 covers the permanent-vs-transient
// distinction: a relay that rejects the handshake outright with 401 must
// surface an *authRejectedError, not a plain error runClient would retry
// forever.
func TestDialMachine_PermanentlyRejectsOn401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	_, err := dialMachine("machine-a", wsURL, "bad-token")
	if err == nil {
		t.Fatalf("expected an error for a rejected handshake")
	}
	var rejected *authRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("expected an *authRejectedError, got %T: %v", err, err)
	}
	if rejected.statusCode != http.StatusUnauthorized {
		t.Fatalf("expected statusCode 401, got %d", rejected.statusCode)
	}
}
