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
