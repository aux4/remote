# Release Notes

## 0.2.0

- **Authentication switched to aux4 SSO.** Dropped the per-machine shared-secret `token`
  (config.yaml/CLI/man pages). Every connection now authenticates with the caller's aux4 access token
  (`AUX4_ACCESS_TOKEN` env, else `aux4 aux4 token` — the same helper and refresh logic every other aux4
  CLI client uses). Machine names are scoped per aux4 account on the relay side
  (`aux4/cloud-remote` 0.3.0+); two different users can register the same machine name without
  conflict, and neither can see or run commands on the other's machines.
- Binaries built with `-ldflags "-s -w" -trimpath` (smaller, no debug symbols).
- Internal: a `call-auto` dispatch replaces the previous `if(command==) && A || B` .aux4 pattern, which
  could run both branches when A legitimately failed.

## 0.1.0

- Initial release.
- Machine daemon (`aux4 aux4 remote connect|disconnect|status`) with reconnect and a fail-closed
  `expose` policy (allow/ask/deny globs; `ask` treated as deny; empty config runs nothing).
- Caller command (`aux4 remote <machine> <cmd...>`, plus `--command` for single-dash flags) and
  `aux4 aux4 remote list`.
- Commands run as real `argv`, never through a shell string.
- Connects to a relay server provided by the companion `aux4/cloud-remote` package.
