package main

// This file used to re-implement aux4 core's allow/ask/deny command-exposure
// policy evaluator from scratch (a second glob matcher kept in sync with
// aux4.dev/aux4/engine/security by hand). It no longer does: the nested
// `aux4` process this daemon always execs (see runExec in client.go) already
// enforces that exact policy itself when AUX4_SECURITY is set in its
// environment - duplicating that logic here only risked the two diverging.
// Instead, buildSecurityEnv below renders the machine's `expose` config
// straight into the JSON shape AUX4_SECURITY expects, fixes up the two places
// aux4 core's own defaults are looser than aux4/remote's fail-closed
// convention, and lets core do 100% of the actual allow/deny/specificity
// evaluation - including for `aux4 remote <machine> aux4` (the bare
// discovery call with no further arguments): core's own listing command
// filters its output by this same policy, and a pattern that reaches a
// descendant (e.g. "aux4 version") naturally makes the "aux4" group itself
// reachable too, so a machine's allowed commands are discoverable without
// aux4/remote needing any special-case logic of its own for that call.
//
// Patterns are written exactly as a caller's msg.Args would look once
// prefixed with the literal "aux4" this daemon always execs as argv[1] (e.g.
// `"aux4 version"` to permit `aux4 remote <machine> aux4 version`, or
// `"todo list*"` to permit `aux4 remote <machine> todo list ...`) - this is
// identical to what aux4 core's own AUX4_SECURITY enforcement
// (resolveSecurityPolicy/enforceSecurity in aux4 core's security.go) matches
// against, since the nested process's own argv is exactly msg.Args.
//
// No shell is ever involved in running the command (see runExec in
// client.go), so there is no way for a caller to smuggle a second command
// into msg.Args that would change what the matched path represents - each
// element of msg.Args becomes exactly one argv entry to the child process.

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type exposeJSON struct {
	Allow []string `json:"allow,omitempty"`
	Ask   []string `json:"ask,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

func clean(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// buildSecurityEnv renders a machine's `expose` config (a JSON object string,
// or empty) into the AUX4_SECURITY value the nested `aux4` process enforces.
// It applies the two corrections needed to keep aux4/remote's fail-closed
// convention intact despite aux4 core's own, looser defaults:
//
//   - Empty, missing, or unparsable `expose`/`allow` is rendered as an
//     explicit deny-all (`{"deny":["*"]}`), never as an empty `allow` list.
//     aux4 core treats "no allow patterns configured" as "nothing has been
//     restricted - allow everything" (its own sensible default for a normal
//     interactive install), which is the opposite of what aux4/remote means
//     by "an `expose` policy with nothing in its `allow` list" - this daemon
//     must stay fail-closed even though the enforcement itself now happens
//     one level down.
//   - `ask` entries are folded into `deny` rather than passed through as
//     `ask`. This daemon runs unattended: if aux4 core ever receives an `ask`
//     pattern with no controlling terminal on stdin, it writes an interactive
//     confirmation prompt (including ANSI cursor-control sequences) to the
//     command's own stdout/stderr before falling back to "no" - which this
//     daemon would otherwise stream straight back to the caller as if it were
//     the command's real output. Treating `ask` as `deny` up front gets the
//     same safe (denied) outcome without ever producing that prompt noise.
func buildSecurityEnv(exposeConfig string) string {
	denyAll := `{"deny":["*"]}`

	exposeConfig = strings.TrimSpace(exposeConfig)
	if exposeConfig == "" {
		return denyAll
	}

	var ej exposeJSON
	if err := json.Unmarshal([]byte(exposeConfig), &ej); err != nil {
		return denyAll
	}

	allow := clean(ej.Allow)
	if len(allow) == 0 {
		return denyAll
	}

	// `ask` is folded into `deny` and never forwarded as `ask` itself - see
	// the doc comment above for why (an unattended confirmation prompt would
	// leak into the command's own output stream).
	deny := append(clean(ej.Deny), clean(ej.Ask)...)

	b, err := json.Marshal(exposeJSON{Allow: allow, Deny: deny})
	if err != nil {
		return denyAll
	}
	return string(b)
}

// validateArgs rejects an args list whose space-joined representation does
// not round-trip back to exactly those arguments when split on single
// spaces. The nested aux4 process's own AUX4_SECURITY check matches against
// strings.Join(args, " ") (via its real argv), so if that join is ambiguous -
// an empty arg, an arg with leading/trailing whitespace, or doubled/irregular
// internal whitespace - a caller could shape args so the effective command
// differs from the string representation a `deny` pattern was written to
// describe, while the underlying argv (passed through to exec.Command
// untouched, never rejoined) still carries the same arguments to the child
// process. For example `["todo ", "rm"]` joins to `"todo  rm"` (double
// space), which `deny: ["todo rm*"]` (single space) does not match, even
// though the intent of that pattern is clearly to block this command. Rather
// than try to "normalize" (collapse) whitespace - which would just move the
// ambiguity elsewhere - this rejects the exec outright with a clear message.
func validateArgs(args []string) error {
	for _, a := range args {
		if a == "" {
			return fmt.Errorf("empty argument is not permitted")
		}
		if strings.TrimSpace(a) != a {
			return fmt.Errorf("argument %q has leading or trailing whitespace, which is not permitted", a)
		}
		for _, r := range a {
			if r < 0x20 || r == 0x7f {
				return fmt.Errorf("argument %q contains a control character, which is not permitted", a)
			}
		}
	}
	cmdLine := strings.Join(args, " ")
	if strings.Join(strings.Fields(cmdLine), " ") != cmdLine {
		return fmt.Errorf("arguments contain irregular whitespace, which is not permitted")
	}
	return nil
}

// aux4SecurityMinVersion is the lowest local aux4 core version known to
// enforce AUX4_SECURITY at all (see project_core_security_policy in the
// shared knowledge base: the command-exposure policy feature shipped as part
// of aux4/aux4 5.2.9/5.2.10). An older aux4 binary on PATH would silently
// ignore the env var this daemon depends on entirely and run every exec'd
// command unrestricted - checkAux4SecuritySupported refuses to let the
// daemon start at all against such a binary, rather than serve under a false
// sense of enforcement.
var aux4SecurityMinVersion = [3]int{5, 2, 9}

var aux4VersionPattern = regexp.MustCompile(`\baux4\s+(\d+)\.(\d+)\.(\d+)\b`)

// checkAux4SecuritySupported runs the local `aux4` binary (the same one
// runExec always execs commands through) and confirms its version is new
// enough to enforce AUX4_SECURITY. It fails closed: if the version can't be
// determined at all (the binary is missing, the output format changed,
// etc.), that is treated the same as "too old" - this daemon must never
// assume enforcement is in place without having confirmed it.
func checkAux4SecuritySupported() error {
	out, err := exec.Command("aux4", "aux4", "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot determine the local aux4 version (required to confirm AUX4_SECURITY enforcement, minimum %s): %w", versionString(aux4SecurityMinVersion), err)
	}
	v, ok := parseAux4Version(string(out))
	if !ok {
		return fmt.Errorf("cannot parse the local aux4 version from %q (required to confirm AUX4_SECURITY enforcement, minimum %s)", strings.TrimSpace(string(out)), versionString(aux4SecurityMinVersion))
	}
	if compareVersions(v, aux4SecurityMinVersion) < 0 {
		return fmt.Errorf("local aux4 %s does not enforce AUX4_SECURITY (minimum %s) - upgrade aux4 on this machine before connecting it to a relay, or every exec'd command would run unrestricted regardless of this machine's `expose` policy", versionString(v), versionString(aux4SecurityMinVersion))
	}
	return nil
}

func parseAux4Version(output string) (v [3]int, ok bool) {
	m := aux4VersionPattern.FindStringSubmatch(output)
	if m == nil {
		return v, false
	}
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func compareVersions(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionString(v [3]int) string {
	return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
}
