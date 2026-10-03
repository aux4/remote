package main

import "strings"

// splitCommandLine tokenizes a shell-style command line (the machine name
// followed by the command and its arguments) the same way a POSIX shell
// would split argv, supporting single quotes, double quotes, and backslash
// escapes. It exists so `aux4 remote <machine> --command "ls -la /tmp"` can
// carry single-dash flags, which aux4's own CLI argument parser would
// otherwise consume as its own options if passed as separate positional
// tokens.
func splitCommandLine(s string) []string {
	var result []string
	var current strings.Builder
	hasToken := false

	inSingle := false
	inDouble := false
	escapeNext := false

	flush := func() {
		if hasToken {
			result = append(result, current.String())
			current.Reset()
			hasToken = false
		}
	}

	for _, r := range s {
		switch {
		case escapeNext:
			current.WriteRune(r)
			hasToken = true
			escapeNext = false
		case r == '\\' && !inSingle:
			escapeNext = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			hasToken = true
		case r == '"' && !inSingle:
			inDouble = !inDouble
			hasToken = true
		case (r == ' ' || r == '\t') && !inSingle && !inDouble:
			flush()
		default:
			current.WriteRune(r)
			hasToken = true
		}
	}
	flush()

	return result
}
