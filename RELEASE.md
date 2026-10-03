# Release Notes

## 0.1.0

- Initial release.
- Machine daemon (`aux4 aux4 remote connect|disconnect|status`) with reconnect and a fail-closed
  `expose` policy (allow/ask/deny globs; `ask` treated as deny; empty config runs nothing).
- Caller command (`aux4 remote <machine> <cmd...>`, plus `--command` for single-dash flags) and
  `aux4 aux4 remote list`.
- Commands run as real `argv`, never through a shell string.
- Connects to a relay server provided by the companion `aux4/cloud-remote` package.
