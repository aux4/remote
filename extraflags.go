package main

// extraflags.go mirrors aux4/cloud's UX for `aux4 cloud <vm> <command...>`: any
// `--flag value` on the outer `aux4 remote` invocation that is not one of
// remote's own flags is forwarded to the remote aux4 command as a named
// `--flag value` parameter, so most commands do not need the shell-quoted
// --command form at all. This only applies to the positional args form - a
// caller who already used --command supplied one fully explicit, shell-quoted
// command line and extra forwarding would duplicate or conflict with it.

import (
	"encoding/json"
	"fmt"
	"sort"
)

// reservedParams are remote's own command flags, plus the aux4 built-ins
// value(*) always includes, none of which are meant for the remote machine.
// "event"/"response"/"stdin" are runtime-injected by aux4 cloud on every
// invocation there (object-valued, carrying the whole invocation's params and
// stdin) - never something a caller typed and never safe to forward. They are
// listed here as defense in depth on top of the scalar-only rule below, which
// is what actually makes them unforwardable (their values are objects/nil,
// not scalars).
//
// "security" (and its dashed/alias variants) is aux4 core's own runtime
// security-policy flag/config key (see aux4/security.go,
// resolveSecurityPolicy): --security lets a caller set the AUX4_SECURITY
// policy for the invocation it is making. It must never be forwarded to the
// remote command line - doing so would let a caller override the *remote
// machine's* expose policy for its own nested `aux4` invocation. The nested
// `aux4` process is what actually enforces that policy (see buildSecurityEnv
// in security.go, which renders the machine's own `expose` config into
// AUX4_SECURITY and sets it last in the child's environment specifically so
// nothing else - including a forwarded flag - can override it); this
// blocklist entry is what stops a forwarded `--security` from ever reaching
// the remote command line in the first place. Listed here even though a
// dot-notation `--security.allow ...` value would already arrive as a
// non-scalar map (and so be refused by shape below) - this is
// belt-and-suspenders against a flat/string form ever being accepted by a
// future change to how aux4 core exposes this flag.
var reservedParams = map[string]bool{
	"args":           true,
	"command":        true,
	"server":         true,
	"config":         true,
	"configFile":     true,
	"aux4HomeDir":    true,
	"configDir":      true,
	"packageDir":     true,
	"event":          true,
	"response":       true,
	"stdin":          true,
	"security":       true,
	"Security":       true,
	"SECURITY":       true,
	"no-security":    true,
	"noSecurity":     true,
	"security.allow": true,
	"security.ask":   true,
	"security.deny":  true,
}

// forwardedFlags decodes a value(*) JSON object and renders every
// non-reserved, scalar-valued entry as "--key value" pairs (repeated once per
// element for an array of scalars, matching aux4's own param(name**)
// convention), sorted by key for deterministic output.
//
// value(*) is not limited to what the caller actually typed - it also
// carries whatever the runtime injects into every invocation (e.g. aux4
// cloud's "event"/"response"/"stdin" object params, which describe the
// invocation itself, not a command flag). There is no reliable way from
// value(*) alone to tell "the caller typed --foo" apart from "the runtime
// injected foo", so forwarding is restricted to scalar values (string,
// number, bool) and arrays of scalars ONLY: a map/object or nil value is
// never forwarded, no matter its key. This is deliberately not just a
// reserved-name blocklist - an unknown future runtime-injected object param
// is refused by shape, not by name.
func forwardedFlags(paramsJSON string) []string {
	if paramsJSON == "" {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(paramsJSON), &m); err != nil {
		return nil
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		if reservedParams[k] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []string
	for _, k := range keys {
		out = append(out, flagTokens(k, m[k])...)
	}
	return out
}

// isScalar reports whether v is a JSON string, number, or bool - the only
// shapes forwarded as a flag value. Maps, slices of non-scalars, and nil are
// never scalar.
func isScalar(v interface{}) bool {
	switch v.(type) {
	case string, float64, bool:
		return true
	default:
		return false
	}
}

func flagTokens(key string, value interface{}) []string {
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{"--" + key, v}
	case float64, bool:
		return []string{"--" + key, fmt.Sprintf("%v", v)}
	case []interface{}:
		var out []string
		for _, e := range v {
			if !isScalar(e) {
				// A single non-scalar element (e.g. an object in the array)
				// makes the whole value untrustworthy to render as repeated
				// flags - skip the entire key rather than forward a partial,
				// confusing result.
				return nil
			}
		}
		for _, e := range v {
			s := fmt.Sprintf("%v", e)
			if s == "" {
				continue
			}
			out = append(out, "--"+key, s)
		}
		return out
	default:
		// nil, map[string]interface{}, or anything else unrecognized - never
		// forwarded, regardless of key name.
		return nil
	}
}
