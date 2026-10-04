# aux4/remote

Run **aux4 commands** — never arbitrary binaries — on another machine through an
[`aux4/cloud-remote`](https://hub.aux4.io) relay server, without opening inbound ports on either side.
Machines dial out to the relay and register under a name; any caller logged in to aux4 can run an aux4
command on a machine **it registered itself** and stream its output back — authentication and
ownership are both your aux4 login, not a separate per-machine secret.

**A connected machine only ever executes `aux4 <args...>`.** The daemon always resolves and runs the
`aux4` binary itself; a caller can only supply arguments *to* `aux4`, never choose a different program
to run. This holds even if a machine's `expose` policy is misconfigured to name something that looks
like a shell command.

## Installation

```bash
aux4 aux4 pkger install aux4/remote
```

## Quick Start

Start the relay server somewhere both sides can reach — see
[`aux4/cloud-remote`](https://hub.aux4.io) for that side:

```bash
aux4 aux4 pkger install aux4/cloud-remote
aux4 aux4 cloud-remote server start --configFile server-config.yaml --config relay
```

Log in once (if you have not already):

```bash
aux4 aux4 login
```

On `build-box` (the machine that will run commands):

```yaml
# config.yaml
config:
  build-box:
    name: build-box
    server: ws://relay.example.com:8420/ws
    expose:
      allow:
        - "aux4 version"
        - "todo list*"
        - "aux4 pkger list*"
```

```bash
aux4 aux4 remote connect --configFile config.yaml --config build-box
```

From `laptop` (or anywhere, as long as you are logged in to the same aux4 account `build-box` connected
under):

```bash
aux4 remote build-box aux4 version
aux4 remote build-box todo list --server ws://relay.example.com:8420/ws
```

The first run executes `aux4 aux4 version` on `build-box`; the second executes `aux4 todo list` there.
The exit code of `aux4 remote` is the exit code of that `aux4` invocation on the remote machine.

**Note:** aux4's own CLI parser consumes a bare `-x` token as one of its own options, so
`aux4 remote build-box aux4 pkger list -v` could lose arguments after the single-dash flag. For
commands with single-dash flags, use `--command` with the machine name and the whole aux4 command in
one shell-quoted string: `aux4 remote --command "build-box aux4 pkger list -v"`.

### Flag Forwarding

Like [`aux4 cloud <vm> <command...>`](https://hub.aux4.io), any `--flag` on `aux4 remote` that is not
one of its own (`--server`, `--command`) is forwarded to the remote aux4 command as its own named flag:

```bash
aux4 remote build-box todo list --status open --server ws://relay.example.com:8420/ws
```

runs `aux4 todo list --status open` on `build-box`. This applies only to the positional form — an
explicit `--command "..."` string is already complete and is sent exactly as given.

**Differences from `aux4 cloud`:** `aux4 cloud` sends flag values to its backend as structured data, so
even single-dash flags and values with shell metacharacters reach the remote command unmodified, and it
also forwards piped stdin. `aux4/remote` forwards flags as plain argv strings over the relay instead, so
a single-dash flag can still be misread by aux4's own CLI parser before reaching `aux4 remote` — use
`--command` for those — and stdin is not forwarded.

## Configuration

```yaml
config:
  # this machine's identity and what it is willing to run
  build-box:
    name: build-box               # letters/digits/./_/- only, no ".." (unique among YOUR machines, see Authentication below)
    server: wss://relay:8420/ws   # the relay server's websocket URL (see aux4/cloud-remote); ws:// only to localhost/127.0.0.1/::1
    expose:                       # allow/ask/deny glob patterns matched against the aux4 command line
      allow:                      # (the arguments after the machine name, space-joined, no leading "aux4")
        - "aux4 version"
        - "todo list*"
      deny:
        - "todo rm*"
```

## Authentication

Every connection — a machine registering, or a caller running a command — authenticates with your
aux4 SSO access token, the same one every other aux4 CLI command uses:

- If `AUX4_ACCESS_TOKEN` is set, it is used directly (useful for CI, and for a machine running
  unattended where `aux4 aux4 login`'s interactive flow is not an option).
- Otherwise, `aux4 aux4 token` resolves (and transparently refreshes) your current login from
  `~/.aux4.config` — the exact same helper other aux4 CLI tooling uses.
- On an aux4 cloud VM, the platform injects `AUX4_ACCESS_TOKEN` into the command's environment
  automatically — no extra setup needed there.

**Machine names are scoped to your own aux4 account.** Two different aux4 users can both register a
machine named `build-box` without conflict, and neither can list or run commands on the other's
machines — the relay authorizes `aux4 aux4 remote list` and `aux4 remote <machine> ...` purely by
whose login registered the machine, never by anything the caller claims about itself.

## How It Works

- The relay server ([`aux4/cloud-remote`](https://hub.aux4.io)) validates every access token against
  aux4 SSO and routes exec requests and their output between callers and machines. It can run
  standalone wherever it is downloaded, or be deployed to an aux4 cloud VM.
- A **machine** connects out to the relay (`aux4 aux4 remote connect`) and authenticates with its
  owner's aux4 access token and the machine's name; the relay keeps a registry entry for it (which
  connection belongs to which (principal, machine name) pair).
- A **caller** (`aux4 remote <machine> <aux4 args...>`) opens its own connection to the relay and sends
  its own access token again in every message it sends (`hello`/`exec`/`list`) rather than relying on a
  registry entry of its own — the relay validates the token fresh each time and authorizes the request
  against the principal it resolves to. This asks the relay to route an exec request to the named
  machine (only machines registered under the SAME aux4 account are visible), and streams back
  `stdout`/`stderr` as the command runs, finishing with the command's real exit code.
- **The machine only ever runs `aux4`.** It never runs anything through a shell, and it never treats a
  caller's arguments as a binary to exec — it always resolves `aux4` from `PATH` and runs it with the
  caller's arguments as ITS arguments (`aux4 <args...>`). A caller cannot make a connected machine run
  any program other than `aux4`, no matter what it sends or what the `expose` policy below allows.

## Exit Codes and Output Streams

`aux4 remote` exits with **exactly** the remote `aux4` invocation's exit code — it is never swallowed,
remapped, or normalized to `0`/`1`. The remote command's `stdout` and `stderr` are streamed back as two
independent, tagged streams and written to the matching local stream (never merged): redirecting one
away locally (`2>/dev/null`, `1>/dev/null`) does not affect the other.

A few exit codes are produced by `aux4/remote` itself rather than passed through from the remote
command, when the command never actually ran:

| Code | Meaning |
|------|---------|
| `0`–`125` | The remote `aux4` command's own exit code, passed through unchanged (`0` is success). |
| `124` | The command exceeded its execution deadline (10 minutes by default, `AUX4_REMOTE_EXEC_TIMEOUT`) and was killed. |
| `126` | The command was denied by the target machine's `expose` policy (or the exec's arguments were rejected outright — see Security Notes). |
| `127` | The remote `aux4` itself reported "command not found" for the given arguments — the same code a shell uses for an unresolvable command. |
| `1` | A local or relay-side failure before the command ever ran: the relay is unreachable, the access token is invalid, or the target machine is offline. |

## Exposure Policy (`expose`)

Every machine decides for itself which aux4 commands it will run. The `expose` config is matched
against the aux4 command line the caller sent — the arguments after the machine name, space-joined,
without a leading `aux4` (e.g. `"todo list"` or `"aux4 version"`) — using the same `allow` / `ask` /
`deny` glob semantics as aux4 core's command-exposure policy — the most specific matching pattern wins,
and on a tie `deny` beats `ask` beats `allow`.

Two rules specific to `aux4/remote`:

- **Fail closed.** An empty or missing `expose.allow` list means the machine runs nothing at all —
  there is no "allow everything" default here, unlike aux4 core's own policy.
- **`ask` is `deny`.** The daemon runs unattended with nobody to answer a prompt, so any command that
  would otherwise prompt for confirmation is refused instead.

A denied command never spawns a process; the caller gets a non-zero exit code and an error message
explaining the command was not permitted.

The machine also sets `AUX4_SECURITY` in the executed `aux4` process's environment to the same `allow`/
`ask`/`deny` patterns, so aux4 core enforces the identical policy one level down as defense in depth —
even if the machine's own policy check were ever bypassed, the `aux4` process itself would still refuse
the command.

## Management Commands

```bash
aux4 aux4 remote connect --configFile config.yaml --config <name>   # connect this machine to the relay
aux4 aux4 remote disconnect --name <name>                            # disconnect this machine
aux4 aux4 remote status --name <name>                                # is this machine's connection up?
aux4 aux4 remote list --server <url>                                 # list machines you have registered
```

`connect` backgrounds the daemon itself (`nohup` + a pid file under `--dataDir`, default
`~/.aux4.config/remote`); `disconnect` and `status` read the same pid file.

## Security Notes

- A machine only ever runs `aux4` — never a shell, and never a binary named by the caller. A caller's
  arguments become aux4's own arguments, nothing more; there is no way to run an arbitrary program
  through `aux4/remote`.
- Commands run with real `argv`, never through a shell string — remote input cannot inject shell
  syntax.
- Access tokens are never logged.
- **`ws://` is only accepted to a loopback host.** The relay operator can see every command and its
  output in transit, so `wss://` (TLS) is required for anything but a fully trusted, purely local
  setup. `ws://relay.example.com/ws` is rejected outright; `ws://localhost:8420/ws`,
  `ws://127.0.0.1:8420/ws`, and `ws://[::1]:8420/ws` are still accepted since nothing leaves the
  machine.
- A machine with an empty `expose` config is safe by default: it is connected but will run nothing.
- `expose` patterns describe aux4 command lines, not shell commands. Naming something that looks like a
  non-aux4 program (e.g. `"echo hi"`) in `allow` does not make it runnable — it only changes which
  arguments are allowed to reach `aux4`, and `aux4 echo hi` simply fails with "command not found".
- **Argument whitespace is validated, not just pattern-matched.** `expose` matching joins a command's
  arguments with single spaces, so a crafted argument (an empty element, leading/trailing whitespace, a
  control character, or doubled internal whitespace) could otherwise make that joined string diverge
  from what a `deny` pattern was written to describe while the underlying arguments still reached
  `aux4`. Any exec whose arguments have this shape is rejected outright (exit code 126) before the
  `expose` policy is even evaluated.
- **Resource use per machine is bounded.** At most 8 commands run concurrently on a machine; anything
  beyond that is rejected immediately rather than queued. Every command also has a timeout — 10 minutes
  by default, overridable with the `AUX4_REMOTE_EXEC_TIMEOUT` environment variable (e.g. `30s`, `2h`) —
  and is killed, along with anything it spawned, if it runs past that timeout or if the connection to
  the relay drops.
- **`--security` cannot be forwarded to the remote command.** [Flag forwarding](#flag-forwarding) never
  sends a `security`/`no-security` flag (or any dot-notation `security.*` value) to the remote machine —
  a caller cannot use it to override the target machine's own `expose`-derived `AUX4_SECURITY` policy.
- `aux4 aux4 remote connect` validates the machine `name` (letters, digits, `.`, `_`, `-` only — no `..`
  and no path separator) before touching the filesystem, creates its `dataDir` as `0700`, and writes the
  pid/log files as `0600`, so another local user cannot read command output or tamper with the
  connection's pid file.
