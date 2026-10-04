package main

// token.go resolves the aux4 SSO access token this client authenticates
// with, replacing the old per-machine shared secret: the relay now validates
// the SAME token every other aux4 client would use against aux4 SSO. Reuses
// the convention already used by other aux4 Go clients (see packages/cloud's
// getAccessToken): `aux4 aux4 token` is aux4-pkger's own helper, which
// already honors AUX4_ACCESS_TOKEN (e.g. for CI) and transparently refreshes
// an expired token from the stored refresh token in ~/.aux4.config -
// reimplementing that here would just drift from it. Never log the token
// this returns.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// tokenLikeEnvPrefixes names environment variables that must never reach a
// command this daemon execs on a caller's behalf (runExec in client.go): this
// process's own aux4 credentials, which a remote caller never supplied and
// has no business inheriting just because its command happened to run here.
// Prefix-matched (not exact) so a related var (AUX4_ACCESS_TOKEN_EXPIRY, a
// future AUX4_REFRESH_TOKEN, etc.) is caught without needing its own entry.
var tokenLikeEnvPrefixes = []string{
	"AUX4_ACCESS_TOKEN",
	"AUX4_REFRESH_TOKEN",
	"AUX4_ID_TOKEN",
}

// filteredEnviron returns this process's own environment with every
// token-like variable (see tokenLikeEnvPrefixes) removed - the starting point
// for an exec'd command's env, which the caller then adds AUX4_SECURITY back
// onto. AUX4_SECURITY itself is never in this list: the exec'd command needs
// it to enforce its own nested policy.
func filteredEnviron() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name := kv
		if idx := strings.IndexByte(kv, '='); idx >= 0 {
			name = kv[:idx]
		}
		if isTokenLikeEnvName(name) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func isTokenLikeEnvName(name string) bool {
	for _, prefix := range tokenLikeEnvPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// accessToken resolves a current aux4 SSO access token. AUX4_ACCESS_TOKEN
// wins outright when set (CI / explicit override, and avoids a subprocess on
// a hot path like reconnect); otherwise it shells out to `aux4 aux4 token`,
// which is also what resolves and refreshes the token on a cloud VM (the
// platform injects AUX4_ACCESS_TOKEN into the command's environment there).
func accessToken() (string, error) {
	if token := strings.TrimSpace(os.Getenv("AUX4_ACCESS_TOKEN")); token != "" {
		return token, nil
	}

	out, err := exec.Command("aux4", "aux4", "token").Output()
	if err != nil {
		return "", fmt.Errorf("not logged in to aux4 - run: aux4 aux4 login")
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("not logged in to aux4 - run: aux4 aux4 login")
	}
	return token, nil
}
