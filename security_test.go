package main

import (
	"strings"
	"testing"
)

func TestIsExposed_NilPolicyDeniesEverything(t *testing.T) {
	if isExposed(nil, "todo list") {
		t.Fatal("expected nil policy to deny everything")
	}
}

func TestIsExposed_EmptyAllowDeniesEverything(t *testing.T) {
	p := parseExpose(map[string]interface{}{})
	if isExposed(p, "todo list") {
		t.Fatal("expected policy with no allow patterns to deny everything")
	}
}

func TestIsExposed_AllowsExactMatch(t *testing.T) {
	p := parseExpose(`{"allow":["aux4 version"]}`)
	if !isExposed(p, "aux4 version") {
		t.Fatal("expected \"aux4 version\" to be allowed")
	}
	if isExposed(p, "aux4 aux4 version") {
		t.Fatal("expected a different command line not to be allowed")
	}
}

func TestIsExposed_AllowsGlob(t *testing.T) {
	p := parseExpose(`{"allow":["todo list*"]}`)
	if !isExposed(p, "todo list") {
		t.Fatal("expected \"todo list\" to match \"todo list*\"")
	}
	if !isExposed(p, "todo list --all") {
		t.Fatal("expected \"todo list --all\" to match \"todo list*\"")
	}
	if isExposed(p, "todo rm") {
		t.Fatal("expected \"todo rm\" not to match \"todo list*\"")
	}
}

func TestIsExposed_MostSpecificWins(t *testing.T) {
	p := parseExpose(`{"allow":["todo *"],"deny":["todo rm*"]}`)
	if !isExposed(p, "todo list") {
		t.Fatal("expected \"todo list\" to be allowed by the broader allow pattern")
	}
	if isExposed(p, "todo rm 1") {
		t.Fatal("expected \"todo rm 1\" to be denied by the more specific deny pattern")
	}
}

func TestIsExposed_DenyBeatsAllowOnTie(t *testing.T) {
	p := parseExpose(`{"allow":["todo list"],"deny":["todo list"]}`)
	if isExposed(p, "todo list") {
		t.Fatal("expected deny to win a specificity tie against allow")
	}
}

func TestIsExposed_AskIsTreatedAsDeny(t *testing.T) {
	p := parseExpose(`{"allow":["todo *"],"ask":["todo rm*"]}`)
	if isExposed(p, "todo rm 1") {
		t.Fatal("expected ask to be treated as deny (daemon is non-interactive)")
	}
}

func TestToAux4Security_NilPolicy(t *testing.T) {
	got := toAux4Security(nil)
	if got != `{"deny":["*"]}` {
		t.Fatalf("expected a deny-everything policy for nil, got %q", got)
	}
}

func TestToAux4Security_PassesThroughSamePatterns(t *testing.T) {
	p := parseExpose(`{"allow":["todo list*","aux4 version"],"deny":["todo rm*"]}`)
	got := toAux4Security(p)
	// The rendered JSON must carry the exact same pattern strings remote itself
	// matches msg.Args against, so a nested `aux4` process (core's
	// enforceSecurity, which evaluates the path as typed after `aux4`,
	// space-joined) reaches the identical decision as this machine already did.
	for _, want := range []string{"todo list*", "aux4 version", "todo rm*"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected AUX4_SECURITY %q to contain pattern %q", got, want)
		}
	}
}

func TestValidateArgs_AcceptsCleanArgs(t *testing.T) {
	if err := validateArgs([]string{"todo", "rm", "1"}); err != nil {
		t.Fatalf("expected clean args to be accepted, got %v", err)
	}
}

func TestValidateArgs_RejectsEmptyArg(t *testing.T) {
	if err := validateArgs([]string{"todo", "", "1"}); err == nil {
		t.Fatal("expected an empty argument to be rejected")
	}
}

func TestValidateArgs_RejectsLeadingOrTrailingWhitespace(t *testing.T) {
	if err := validateArgs([]string{"todo ", "rm"}); err == nil {
		t.Fatal("expected a trailing-space argument to be rejected")
	}
	if err := validateArgs([]string{" todo", "rm"}); err == nil {
		t.Fatal("expected a leading-space argument to be rejected")
	}
}

func TestValidateArgs_RejectsControlCharacters(t *testing.T) {
	if err := validateArgs([]string{"todo", "rm\n1"}); err == nil {
		t.Fatal("expected a newline inside an argument to be rejected")
	}
	if err := validateArgs([]string{"todo", "rm\x001"}); err == nil {
		t.Fatal("expected a NUL byte inside an argument to be rejected")
	}
}

func TestValidateArgs_RejectsIrregularInternalWhitespace(t *testing.T) {
	// A single argument containing a double space would join identically to
	// two separate single-spaced arguments, which is exactly the ambiguity
	// that must be refused.
	if err := validateArgs([]string{"todo", "rm  1"}); err == nil {
		t.Fatal("expected doubled internal whitespace within one argument to be rejected")
	}
}

// TestValidateArgs_ClosesTheWhitespaceBypass is the exact bypass scenario:
// args shaped so their joined cmdLine no longer matches the deny pattern
// written to describe the equivalent, normally-spaced command.
func TestValidateArgs_ClosesTheWhitespaceBypass(t *testing.T) {
	args := []string{"todo ", "rm"}
	cmdLine := strings.Join(args, " ")

	policy := parseExpose(`{"allow":["todo *"],"deny":["todo rm*"]}`)
	if !isExposed(policy, cmdLine) {
		t.Fatalf("expected %q to document the underlying bug: the broad allow glob matches the double-spaced cmdLine while the more specific deny glob (single space) does not, so isExposed alone would let this through", cmdLine)
	}

	if err := validateArgs(args); err == nil {
		t.Fatal("expected validateArgs to reject the whitespace-crafted args that evade the deny glob")
	}
}

func TestIsExposed_CmdLineHasNoAux4Prefix(t *testing.T) {
	// Patterns are matched against msg.Args joined WITHOUT a leading "aux4" -
	// this mirrors exactly what aux4 core matches AUX4_SECURITY against for the
	// nested `aux4` invocation this daemon always runs.
	p := parseExpose(`{"allow":["aux4 version"]}`)
	if isExposed(p, "version") {
		t.Fatal("pattern \"aux4 version\" should not match bare \"version\"")
	}
	if !isExposed(p, "aux4 version") {
		t.Fatal("pattern \"aux4 version\" should match a nested aux4 invocation's own args")
	}
}
