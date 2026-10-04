package main

// This file implements a glob-based allow/ask/deny policy evaluator, adapted
// from aux4's own core command-exposure policy (aux4.dev/aux4/engine/security)
// so this standalone Go module does not need a cross-repo dependency. The
// matching semantics (most-specific-wins, deny beats ask beats allow on a
// specificity tie) are intentionally identical.
//
// aux4/remote adds one rule on top: an `expose` config with no `allow` entries
// is NOT "allow everything" (as it would be in aux4 core) — it is "deny
// everything". The daemon is non-interactive, so `ask` is also treated as
// deny. See isExposed below.
//
// Patterns are matched against the aux4 command line: msg.Args space-joined,
// WITHOUT a leading "aux4" (e.g. allow `["todo list*", "aux4 version"]` to
// permit `aux4 remote <machine> todo list ...` and `aux4 remote <machine>
// aux4 version`). This is exactly the "path as typed after aux4" that aux4
// core's own AUX4_SECURITY enforcement matches against (see
// aux4.dev/aux4/engine/security and resolveSecurityPolicy/enforceSecurity in
// aux4 core's security.go), so toAux4Security below can hand the same
// patterns straight through as defense in depth: the nested `aux4` process
// evaluates the identical path and arrives at the identical decision.
//
// No shell is ever involved in running the command (see runExec in
// client.go), so there is no way for a caller to smuggle a second command
// into msg.Args that would change what cmdLine below represents — each
// element of msg.Args becomes exactly one argv entry to the child process.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

type decision int

const (
	decisionAllow decision = iota
	decisionAsk
	decisionDeny
)

// exposePolicy is a parsed allow/ask/deny glob list for one machine's
// `expose` configuration.
type exposePolicy struct {
	allow []string
	ask   []string
	deny  []string

	mu    sync.Mutex
	cache map[string]*regexp.Regexp
}

type exposeJSON struct {
	Allow []string `json:"allow"`
	Ask   []string `json:"ask"`
	Deny  []string `json:"deny"`
}

// parseExpose builds a policy from the `expose` section of a machine config.
// value may be nil, a JSON string, or an already-decoded map (as produced by
// aux4's config/param resolution). A nil/empty value yields a nil policy.
func parseExpose(value any) *exposePolicy {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil
		}
		var ej exposeJSON
		if err := json.Unmarshal([]byte(s), &ej); err != nil {
			return nil
		}
		return &exposePolicy{allow: clean(ej.Allow), ask: clean(ej.Ask), deny: clean(ej.Deny)}
	case map[string]interface{}:
		return &exposePolicy{
			allow: toStrings(v["allow"]),
			ask:   toStrings(v["ask"]),
			deny:  toStrings(v["deny"]),
		}
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		return parseExpose(string(b))
	}
}

func toStrings(v any) []string {
	switch t := v.(type) {
	case []string:
		return clean(t)
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return clean(out)
	case string:
		return clean([]string{t})
	}
	return nil
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

// isExposed reports whether cmdLine (the space-joined argv the target is
// about to run) is allowed to run, fail-closed: a nil policy, or a policy
// with no allow patterns, denies everything. `ask` never runs commands
// unattended, so it is treated the same as `deny`.
func isExposed(p *exposePolicy, cmdLine string) bool {
	if p == nil || len(p.allow) == 0 {
		return false
	}
	return p.evaluate(cmdLine) == decisionAllow
}

func (p *exposePolicy) evaluate(path string) decision {
	path = strings.TrimSpace(path)

	bestScore := -1
	best := decisionDeny

	consider := func(patterns []string, d decision) {
		for _, pat := range patterns {
			if !p.match(pat, path) {
				continue
			}
			s := specificity(pat)
			if s > bestScore || (s == bestScore && priority(d) > priority(best)) {
				bestScore = s
				best = d
			}
		}
	}

	consider(p.allow, decisionAllow)
	consider(p.ask, decisionAsk)
	consider(p.deny, decisionDeny)

	if bestScore >= 0 {
		return best
	}
	return decisionDeny
}

func priority(d decision) int {
	switch d {
	case decisionDeny:
		return 2
	case decisionAsk:
		return 1
	default:
		return 0
	}
}

func specificity(pattern string) int {
	if pattern == "*" {
		return 0
	}
	score := len(strings.ReplaceAll(pattern, "*", ""))
	if !strings.Contains(pattern, "*") {
		score += 1000
	}
	return score
}

func (p *exposePolicy) match(pattern, path string) bool {
	re := p.compile(pattern)
	return re.MatchString(path)
}

func (p *exposePolicy) compile(pattern string) *regexp.Regexp {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cache == nil {
		p.cache = make(map[string]*regexp.Regexp)
	}
	if re, ok := p.cache[pattern]; ok {
		return re
	}
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		if r == '*' {
			b.WriteString(".*")
		} else {
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	re := regexp.MustCompile(b.String())
	p.cache[pattern] = re
	return re
}

// validateArgs rejects an args list whose space-joined representation does
// not round-trip back to exactly those arguments when split on single
// spaces. Policy matching (isExposed, and the nested aux4 process's own
// AUX4_SECURITY check) both operate on strings.Join(args, " "), so if that
// join is ambiguous - an empty arg, an arg with leading/trailing whitespace,
// or doubled/irregular internal whitespace - a caller can shape args so the
// resulting cmdLine no longer matches a pattern that was written to describe
// the equivalent, "normally spaced" command, while the underlying argv
// (passed through to exec.Command untouched, never rejoined) can still carry
// the same effective arguments to the child process. For example
// `["todo ", "rm"]` joins to `"todo  rm"` (double space), which `deny:
// ["todo rm*"]` (single space) does not match, even though the intent of
// that pattern is clearly to block this command. Rather than try to
// "normalize" (collapse) whitespace - which would just move the ambiguity
// elsewhere - this rejects the exec outright with a clear message.
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

// toAux4Security renders the policy as the JSON shape aux4 core's AUX4_SECURITY
// env var expects, so a remote command that itself shells out to `aux4` gets
// the same exposure policy applied one level down.
func toAux4Security(p *exposePolicy) string {
	if p == nil {
		return `{"deny":["*"]}`
	}
	b, err := json.Marshal(exposeJSON{Allow: p.allow, Ask: p.ask, Deny: p.deny})
	if err != nil {
		return `{"deny":["*"]}`
	}
	return string(b)
}
