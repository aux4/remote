package main

// validate.go centralizes validation of user-supplied identifiers that are
// later used to build filesystem paths (pid/log files under a machine's
// dataDir) or sent as a websocket header (X-Machine-Name). Keeping this in
// one place means both the long-lived daemon ("client") and the short-lived
// "connect" launcher enforce the identical rule, rather than risking the two
// drifting apart.

import (
	"fmt"
	"regexp"
	"strings"
)

// validNamePattern allows only characters that are safe in a filename on
// every platform this package ships for and that cannot be misread as a path
// separator or a shell/header control sequence.
var validNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// validateName rejects anything that is not a plain, single-segment
// identifier: empty, containing a path separator (excluded by the character
// class itself), containing "..", or containing any character outside
// letters/digits/dot/underscore/dash.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("machine name must not be empty")
	}
	if !validNamePattern.MatchString(name) {
		return fmt.Errorf("machine name %q is invalid: only letters, digits, '.', '_', and '-' are permitted", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("machine name %q is invalid: must not contain '..'", name)
	}
	return nil
}
