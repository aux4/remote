package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubAux4Version puts a fake "aux4" on PATH whose `aux4 version` subcommand
// prints versionLine, for the duration of the test, so
// checkAux4SecuritySupported resolves to it instead of whatever real aux4 is
// actually installed on the machine running this test suite.
func stubAux4Version(t *testing.T, versionLine string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub script is POSIX shell only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "aux4")
	content := "#!/bin/sh\necho '" + versionLine + "'\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
}

func decodeSecurity(t *testing.T, s string) exposeJSON {
	t.Helper()
	var ej exposeJSON
	if err := json.Unmarshal([]byte(s), &ej); err != nil {
		t.Fatalf("buildSecurityEnv produced invalid JSON %q: %v", s, err)
	}
	return ej
}

func TestBuildSecurityEnv_EmptyConfigDeniesEverything(t *testing.T) {
	got := buildSecurityEnv("")
	if got != `{"deny":["*"]}` {
		t.Fatalf("expected an empty expose config to render as deny-all, got %q", got)
	}
}

func TestBuildSecurityEnv_UnparsableConfigDeniesEverything(t *testing.T) {
	got := buildSecurityEnv("not json")
	if got != `{"deny":["*"]}` {
		t.Fatalf("expected an unparsable expose config to fail closed as deny-all, got %q", got)
	}
}

func TestBuildSecurityEnv_EmptyAllowDeniesEverything(t *testing.T) {
	// aux4 core itself treats {} / an empty allow list as "nothing has been
	// restricted - allow everything" (its own sensible interactive default).
	// aux4/remote's fail-closed convention is the opposite: no allow entries
	// means nothing is exposed, so this must render as an explicit deny-all,
	// never as an empty "allow" key that core would read as unrestricted.
	for _, cfg := range []string{`{}`, `{"allow":[]}`, `{"deny":["todo rm*"]}`} {
		got := buildSecurityEnv(cfg)
		if got != `{"deny":["*"]}` {
			t.Fatalf("expected expose config %q with no allow entries to render as deny-all, got %q", cfg, got)
		}
	}
}

func TestBuildSecurityEnv_PassesThroughAllowAndDeny(t *testing.T) {
	got := decodeSecurity(t, buildSecurityEnv(`{"allow":["aux4 version","todo list*"],"deny":["todo rm*"]}`))
	if !equalSet(got.Allow, []string{"aux4 version", "todo list*"}) {
		t.Fatalf("expected allow patterns to pass through unchanged, got %v", got.Allow)
	}
	if !equalSet(got.Deny, []string{"todo rm*"}) {
		t.Fatalf("expected deny patterns to pass through unchanged, got %v", got.Deny)
	}
}

func TestBuildSecurityEnv_FoldsAskIntoDeny(t *testing.T) {
	// The daemon is non-interactive: if `ask` were forwarded as `ask`, a
	// nested aux4 with no controlling terminal would write an interactive
	// confirmation prompt (including ANSI control sequences) to the exec'd
	// command's own stdout/stderr before defaulting to "no" - this daemon
	// streams that straight back to the caller, so it must never happen.
	// Folding `ask` into `deny` up front gets the same safe outcome silently.
	got := decodeSecurity(t, buildSecurityEnv(`{"allow":["aux4 version","aux4 pkger*"],"ask":["aux4 pkger*"]}`))
	if len(got.Ask) != 0 {
		t.Fatalf("expected no \"ask\" key in the rendered AUX4_SECURITY, got %v", got.Ask)
	}
	if !contains(got.Deny, "aux4 pkger*") {
		t.Fatalf("expected the \"ask\" pattern to be folded into \"deny\", got deny=%v", got.Deny)
	}
}

func TestBuildSecurityEnv_NeverEmitsAskKey(t *testing.T) {
	raw := buildSecurityEnv(`{"allow":["aux4 version"],"ask":["aux4 pkger*"]}`)
	if strings.Contains(raw, `"ask"`) {
		t.Fatalf("expected the rendered AUX4_SECURITY to never contain an \"ask\" key, got %q", raw)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func equalSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, s := range b {
		if !contains(a, s) {
			return false
		}
	}
	return true
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
	if err := validateArgs(args); err == nil {
		t.Fatal("expected validateArgs to reject the whitespace-crafted args that could evade a deny glob matched downstream")
	}
}

func TestParseAux4Version_ParsesStandardOutput(t *testing.T) {
	v, ok := parseAux4Version("\n   aux4 5.2.10\n   2026 aux4. aux4 is created and maintained by the aux4 community.\n")
	if !ok {
		t.Fatal("expected to parse a version out of standard `aux4 aux4 version` output")
	}
	if v != [3]int{5, 2, 10} {
		t.Fatalf("expected [5 2 10], got %v", v)
	}
}

func TestParseAux4Version_RejectsUnrecognizedOutput(t *testing.T) {
	if _, ok := parseAux4Version("not a version string"); ok {
		t.Fatal("expected unrecognized output to fail to parse")
	}
}

func TestCheckAux4SecuritySupported_AcceptsAtOrAboveMinimum(t *testing.T) {
	stubAux4Version(t, "   aux4 5.2.9")
	if err := checkAux4SecuritySupported(); err != nil {
		t.Fatalf("expected the minimum supported version to pass, got %v", err)
	}

	stubAux4Version(t, "   aux4 5.3.0")
	if err := checkAux4SecuritySupported(); err != nil {
		t.Fatalf("expected a newer version to pass, got %v", err)
	}
}

func TestCheckAux4SecuritySupported_RejectsOlderVersion(t *testing.T) {
	// A local aux4 older than this daemon's minimum would silently ignore
	// AUX4_SECURITY and run every exec'd command unrestricted - the daemon
	// must refuse to start rather than serve under a false sense of
	// enforcement.
	stubAux4Version(t, "   aux4 5.2.8")
	err := checkAux4SecuritySupported()
	if err == nil {
		t.Fatal("expected an older-than-minimum aux4 version to be rejected")
	}
	if !strings.Contains(err.Error(), "5.2.8") || !strings.Contains(err.Error(), "5.2.9") {
		t.Fatalf("expected the error to name both the found and required versions, got %q", err.Error())
	}
}

func TestCheckAux4SecuritySupported_RejectsUnparsableVersion(t *testing.T) {
	// Fails closed: if the version can't be determined at all, that is NOT
	// treated as "assume it's fine".
	stubAux4Version(t, "not a version string")
	if err := checkAux4SecuritySupported(); err == nil {
		t.Fatal("expected unparsable version output to be rejected, not assumed supported")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b [3]int
		want int
	}{
		{[3]int{5, 2, 9}, [3]int{5, 2, 9}, 0},
		{[3]int{5, 2, 8}, [3]int{5, 2, 9}, -1},
		{[3]int{5, 2, 10}, [3]int{5, 2, 9}, 1},
		{[3]int{5, 1, 99}, [3]int{5, 2, 0}, -1},
		{[3]int{6, 0, 0}, [3]int{5, 2, 9}, 1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Fatalf("compareVersions(%v, %v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
