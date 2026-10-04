#### Description

Runs an **aux4 command** on a remote machine through an `aux4/remote` relay server and streams its
output back. The first positional argument is the target machine's name (as it registered with
`aux4 aux4 remote connect`); every argument after it is passed as arguments **to `aux4` itself** on
that machine — the daemon always runs `aux4 <args...>`, never a shell string and never an arbitrary
binary. There is no way to make the remote machine run anything other than an aux4 command.

- `aux4 remote build-box todo list` runs `aux4 todo list` on `build-box`.
- `aux4 remote build-box aux4 version` runs `aux4 aux4 version` on `build-box`.

Like `aux4 cloud <vm> <command...>`, any `--flag` on the outer `aux4 remote` invocation that is not one
of its own flags (`--server`, `--command`) is forwarded to the remote aux4 command as its own named
`--flag value`: `aux4 remote build-box todo list --status open` runs `aux4 todo list --status open` on
`build-box`. This only applies to the positional form — an explicit `--command "..."` string is already
complete and is sent exactly as given.

`aux4 remote` exits with **exactly** the remote `aux4` invocation's exit code — never swallowed or
remapped. Its `stdout`/`stderr` are two independent, tagged streams written to the matching local
stream (never merged); redirecting one away locally does not affect the other. A few codes come from
`aux4/remote` itself, when the command never actually ran:

| Code | Meaning |
|------|---------|
| `0`–`125` | The remote command's own exit code, passed through unchanged. |
| `124` | Execution deadline exceeded (`AUX4_REMOTE_EXEC_TIMEOUT`, default 10 minutes); the command was killed. |
| `126` | The exec's own arguments were rejected outright before anything ran (crafted/irregular whitespace). This is the only case `aux4/remote` itself denies. |
| `127` | The remote `aux4` reported "command not found" — either the arguments don't resolve to a real command, **or** the target machine's `expose` policy does not permit it. `expose` is enforced by aux4 core itself via `AUX4_SECURITY`, which reports a denied command the same way as a nonexistent one. |
| `1` | Relay unreachable, invalid access token, or the target machine is offline — the command never ran. |

**Single-dash flags**: aux4's own CLI parser treats a bare `-x` token as one of *its* options, so a
command like `aux4 remote build-box aux4 pkger list -v` could lose arguments after a single-dash flag.
For any command that needs single-dash flags, use `--command` instead and put the machine name and the
whole aux4 command in one shell-quoted string: `aux4 remote --command "build-box aux4 pkger list -v"`.
Long `--flags` on the remote command do not have this problem and are forwarded automatically, whether
given positionally or via `--command`.

**Difference from `aux4 cloud`:** `aux4 cloud` sends flag values to its backend as structured data, so
even single-dash flags and values containing shell metacharacters reach the remote command unmodified.
`aux4/remote` instead forwards flags as plain argv strings over the relay, so a single-dash flag can
still be misread by aux4's own CLI parser before it ever reaches `aux4 remote` — `--command` is the
escape hatch for that case. `aux4 cloud` also forwards piped stdin to the remote command; `aux4/remote`
does not forward stdin.

- **Discovery**: `aux4 remote <machine> aux4` — the bare `aux4` call with no further arguments — runs
  the nested `aux4 aux4` command listing, filtered by the target machine's `expose` policy the same way
  aux4 core filters it for any locally restricted install: an `allow` pattern that reaches a descendant
  (e.g. `"aux4 version"`) also makes the `aux4` group itself reachable, so the listing shows exactly the
  subcommands that pattern allows, with no dedicated discovery command or special `allow` entry needed.
  If `expose` has no `allow` entries at all, the group is unreachable like anything else the policy
  denies (exit `127`, "Command not found: aux4"); if it has entries but none resolve to an actual
  subcommand (e.g. `allow: ["aux4"]` alone), the group is reachable but the listing is empty (exit `0`,
  no output).
- **`--server`** identifies the relay server to connect to; it is typically supplied via
  `--configFile`/`--config` pointing at the same config file used for `aux4 aux4 remote connect`.
  Authentication is your aux4 login (`AUX4_ACCESS_TOKEN` if set, else `aux4 aux4 token`) — there is no
  separate token to pass. Only machines registered under the same aux4 account are reachable. `wss://`
  is required for anything but a loopback host; `ws://` to a non-loopback host is rejected outright.
- Standard output and standard error from the remote `aux4` invocation are streamed back as they are
  produced, not buffered until completion.
- A target machine runs at most 8 commands at once and applies a 10-minute default timeout per command
  (overridable there via `AUX4_REMOTE_EXEC_TIMEOUT`); a command beyond the concurrency limit or past its
  timeout exits non-zero with a clear message instead of hanging.
- `--security` is never forwarded to the remote command, even via flag forwarding — it cannot be used to
  override the target machine's own `expose`-derived security policy.

#### Usage

```bash
aux4 remote <machine> <aux4 args...> [--<flag> <value>...] [--server <url>]
aux4 remote --command "<machine> <aux4 args...>" [--server <url>]
```

--command  Alternative to the positional form, as one shell-quoted string (needed for single-dash flags)
--server   Relay server websocket URL (e.g. `ws://relay.example.com:8420/ws`)
--<flag>   Any other flag is forwarded to the remote aux4 command as `--<flag> <value>` (positional form only)

#### Example

```bash
aux4 remote build-box aux4 version --config build-box --configFile config.yaml
```

```text
   aux4 5.2.10
   2026 aux4. aux4 is created and maintained by the aux4 community.
   https://aux4.io
```

Forwarding an extra flag to the remote command:

```bash
aux4 remote build-box todo list --status open --config build-box --configFile config.yaml
```

This runs `aux4 todo list --status open` on `build-box`.

```bash
aux4 remote build-box aux4 pkger uninstall aux4/important --config build-box --configFile config.yaml
```

```text
Command not found: aux4
```

(`build-box`'s `expose` policy does not permit `aux4 pkger uninstall aux4/important`; aux4 core reports
a denied command the same way it reports one that does not exist — see Exit Codes above.)
