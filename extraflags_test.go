package main

import (
	"reflect"
	"testing"
)

func TestForwardedFlags_SkipsReservedAndEmpty(t *testing.T) {
	json := `{"server":"ws://x","args":["m","todo"],"command":"","config":"true","configFile":"c.yaml","aux4HomeDir":"/h","configDir":"/c","packageDir":"/p","status":"open"}`
	got := forwardedFlags(json)
	want := []string{"--status", "open"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestForwardedFlags_SortedAndMultipleFlags(t *testing.T) {
	json := `{"zeta":"1","alpha":"2"}`
	got := forwardedFlags(json)
	want := []string{"--alpha", "2", "--zeta", "1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestForwardedFlags_RepeatsArrayValues(t *testing.T) {
	json := `{"tag":["a","b","c"]}`
	got := forwardedFlags(json)
	want := []string{"--tag", "a", "--tag", "b", "--tag", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestForwardedFlags_NonStringScalars(t *testing.T) {
	json := `{"limit":20,"follow":true}`
	got := forwardedFlags(json)
	want := []string{"--follow", "true", "--limit", "20"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestForwardedFlags_EmptyInput(t *testing.T) {
	if got := forwardedFlags(""); got != nil {
		t.Fatalf("expected nil for empty input, got %v", got)
	}
	if got := forwardedFlags("{}"); got != nil {
		t.Fatalf("expected nil for an empty object, got %v", got)
	}
}

// TestForwardedFlags_SkipsRuntimeInjectedEventResponseStdin reproduces the
// exact shape aux4 cloud injects into every invocation on a command VM:
// object-valued "event"/"response" params (each itself carrying a nested
// "params" object and a "stdin" key that is null), plus a top-level "stdin".
// None of it is something the caller typed, and all of it must never reach
// the command line sent to the remote machine - a prior version of
// forwardedFlags rendered these as "--event map[...]" etc, which both leaked
// internal data and broke the expose policy match (the policy saw "aux4
// version --event map[...] --response map[...]" instead of "aux4 version").
func TestForwardedFlags_SkipsRuntimeInjectedEventResponseStdin(t *testing.T) {
	json := `{
		"server": "wss://relay.example.com/ws",
		"args": ["local-mac", "aux4", "version"],
		"command": "",
		"event": {"params": {"server": "wss://relay.example.com/ws"}, "stdin": null},
		"response": {"params": {"server": "wss://relay.example.com/ws"}, "stdin": null},
		"stdin": null
	}`
	got := forwardedFlags(json)
	if len(got) != 0 {
		t.Fatalf("expected nothing to be forwarded from runtime-injected event/response/stdin, got %v", got)
	}
}

// TestForwardedFlags_RefusesObjectAndNilValuesByShapeNotName proves the rule
// is shape-based, not a blocklist of known key names - an arbitrary,
// previously-unseen object-valued or nil-valued param must never be
// forwarded either, since the next runtime-injected field might not be named
// "event" or "response".
func TestForwardedFlags_RefusesObjectAndNilValuesByShapeNotName(t *testing.T) {
	json := `{
		"someFutureRuntimeField": {"nested": "object"},
		"anotherOne": null,
		"status": "open"
	}`
	got := forwardedFlags(json)
	want := []string{"--status", "open"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v - object/nil values must never be forwarded regardless of key name", got, want)
	}
}

// TestForwardedFlags_ArrayWithNonScalarElementIsDropped proves an array
// containing even one non-scalar element is refused wholesale rather than
// partially rendered, since a partial render could still leak structured data
// for that element.
func TestForwardedFlags_ArrayWithNonScalarElementIsDropped(t *testing.T) {
	json := `{"tag": ["a", {"nested": "object"}, "c"], "status": "open"}`
	got := forwardedFlags(json)
	want := []string{"--status", "open"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v - an array with any non-scalar element must be dropped entirely", got, want)
	}
}

// TestForwardedFlags_PolicyMatchSeesOnlyTheCommandLine is the regression this
// bug was actually caught by: building the full argv aux4-remote would send
// (machine + command + forwarded flags) and confirming the expose policy
// match string is exactly "aux4 version", with nothing from event/response
// leaking into it.
// TestForwardedFlags_NeverForwardsSecurity proves a caller cannot use
// --security (or an alias/casing of it) to smuggle an AUX4_SECURITY override
// onto the remote command line, which would otherwise let it loosen the
// defense-in-depth policy the target machine's own `expose` config sets for
// the nested `aux4` invocation it runs.
func TestForwardedFlags_NeverForwardsSecurity(t *testing.T) {
	json := `{
		"security": "{\"allow\":[\"*\"]}",
		"Security": "{\"allow\":[\"*\"]}",
		"SECURITY": "{\"allow\":[\"*\"]}",
		"no-security": "true",
		"noSecurity": "true",
		"status": "open"
	}`
	got := forwardedFlags(json)
	want := []string{"--status", "open"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v - security flags/aliases must never be forwarded", got, want)
	}
}

// TestForwardedFlags_NeverForwardsDotNotationSecurity proves a dot-notation
// --security.allow/ask/deny value (which arrives as a nested object under the
// "security" key) is refused the same way, both by the reserved-name entries
// and, independently, because it is a non-scalar value.
func TestForwardedFlags_NeverForwardsDotNotationSecurity(t *testing.T) {
	json := `{"security": {"allow": ["*"]}, "status": "open"}`
	got := forwardedFlags(json)
	want := []string{"--status", "open"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v - dot-notation security.* must never be forwarded", got, want)
	}
}

func TestForwardedFlags_PolicyMatchSeesOnlyTheCommandLine(t *testing.T) {
	extraParamsJSON := `{
		"server": "wss://relay.example.com/ws",
		"event": {"params": {"server": "wss://relay.example.com/ws"}, "stdin": null},
		"response": {"params": {"server": "wss://relay.example.com/ws"}, "stdin": null},
		"stdin": null
	}`
	argv := append([]string{"aux4", "version"}, forwardedFlags(extraParamsJSON)...)
	cmdLine := ""
	for i, a := range argv {
		if i > 0 {
			cmdLine += " "
		}
		cmdLine += a
	}
	if cmdLine != "aux4 version" {
		t.Fatalf("expected the policy-matched command line to be exactly \"aux4 version\", got %q", cmdLine)
	}

	policy := parseExpose(`{"allow":["aux4 version"]}`)
	if !isExposed(policy, cmdLine) {
		t.Fatal("expected \"aux4 version\" to still be allowed once runtime-injected params are stripped")
	}
}
