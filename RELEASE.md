# Release Notes

## 0.3.5

- **The `expose` policy is now enforced by aux4 core itself, not re-implemented in this daemon.**
  The daemon used to carry a second glob-based allow/ask/deny evaluator (security.go) that had to be
  kept in sync with aux4 core's own command-exposure policy by hand - a source of drift risk (and the
  exact mechanism that made `aux4 remote <machine> aux4` behave inconsistently: the daemon's own
  pre-check and aux4 core's listing filter could reach different answers about the same policy). That
  evaluator is gone; the daemon now renders the machine's `expose` config straight into aux4 core's
  own `AUX4_SECURITY` environment variable and always execs through it, letting core make every
  allow/deny decision. Two corrections are still applied when rendering `expose` into `AUX4_SECURITY`,
  to keep `aux4/remote`'s fail-closed convention despite aux4 core's looser defaults: an empty or
  missing `allow` list renders as an explicit `{"deny":["*"]}` (core's own default for "no allow
  patterns" is "allow everything", the opposite of what this means here), and every `ask` pattern is
  folded into `deny` (the daemon is unattended, and an unanswered `ask` prompt would otherwise write
  ANSI confirmation-prompt noise into the command's own output stream before defaulting to "no"
  anyway).
- **`aux4 remote <machine> aux4` (bare discovery, no further arguments) now works correctly for free.**
  Previously the daemon special-cased this call to always bypass its own policy check, which then hit
  a second bug: aux4 core's own default for an empty `allow` list (used for "deny everything" expose
  configs) is "allow everything", so a machine configured to expose *nothing* could leak its *entire*
  unfiltered command listing through discovery. With enforcement now fully delegated to core, the
  listing comes back correctly filtered by whatever the policy actually allows - no special-casing
  needed, and no leak: a pattern that reaches a descendant (e.g. `"aux4 version"`) naturally makes the
  `aux4` group itself reachable too, exactly like a normal aux4 install under `AUX4_SECURITY`.
- **Version guard: refuses to start against a local aux4 older than 5.2.9.** `AUX4_SECURITY`
  enforcement only exists in aux4 core from that version onward - an older binary on PATH would
  silently ignore it and run every exec'd command unrestricted. `aux4 aux4 remote connect` and the
  daemon it starts now check the local `aux4` version first and refuse to run (with a clear error)
  against anything older, rather than connect a machine whose `expose` policy would never actually be
  checked.
- **Exit codes changed for denied/hidden commands.** A command the `expose` policy does not permit is
  now reported by the nested `aux4` process itself as "Command not found" (exit `127`), the same as a
  genuinely nonexistent command, since enforcement is now entirely aux4 core's. Exit `126` is reserved
  for the one thing this daemon still rejects itself: an exec whose arguments have the
  crafted-whitespace shape `validateArgs` exists to catch.

## 0.3.4

- **Security fix: the daemon no longer hands its own aux4 credentials to commands it runs
  on a caller's behalf.** `runExec` used to build the exec'd `aux4` process's environment
  from this daemon's own `os.Environ()` plus `AUX4_SECURITY` - if this process authenticated
  via `AUX4_ACCESS_TOKEN` (or ends up with an `AUX4_REFRESH_TOKEN`/`AUX4_ID_TOKEN` set), that
  credential was inherited by every command a remote caller asked it to run. Token-like
  variables (`AUX4_ACCESS_TOKEN*`, `AUX4_REFRESH_TOKEN*`, `AUX4_ID_TOKEN*`) are now stripped
  before exec; `AUX4_SECURITY` is unaffected since the exec'd command needs it for its own
  nested policy enforcement.
- **Reconnect now backs off exponentially with jitter** (2s doubling to a 60s cap, +/-30%
  jitter) instead of a flat 3-5s retry - avoids hammering a relay that is down or
  recovering, especially from many machines reconnecting at once. The backoff resets after
  a successful handshake, so a machine that has been stable for a while reconnects quickly
  again after a single drop.
- **A handshake rejected outright with 401/403 is now treated as permanent**, not retried
  forever: the daemon prints a clear error and exits non-zero instead of looping on a token
  that cannot become valid on its own. A plain network failure (relay unreachable, DNS,
  timeout) still retries with backoff as before.

## 0.3.3

Correctness fixes so `aux4 remote <machine> <cmd>` behaves exactly like running `<cmd>`
locally: the local process exits with the remote command's exact exit code, and the
remote command's stdout/stderr are streamed to the matching local stream, kept separate.

- **Verified exit-code propagation end to end**, including non-sentinel codes (a plain
  `exit 3` from the remote command arrives unchanged), the daemon's own sentinel codes
  (124 timeout, 126 denied-by-expose-policy, 127 command-not-found via the local `aux4`
  CLI), and the success path (0). `.test.md` now asserts this with `echo "exit=$?"` after
  redirecting output away, rather than only checking stderr text.
- **Verified stdout/stderr stream separation end to end**: redirecting one away
  (`1>/dev/null` or `2>/dev/null`) in a test no longer affects the other, proving the two
  are genuinely independent streams rather than merged before being written locally. See
  the new scenarios in `remote.test.md`.
- **`exitGracePeriod`'s role is now accurately documented.** It only absorbs ordinary
  arrival-order jitter between a trailing "output" and "exit" - it is not what prevents
  output loss (see the matching `aux4/cloud-remote` 0.4.3 fix, which is the real fix for a
  relay-side race that could silently drop output).
- Added a Go unit test (`TestRunExec_TagsStdoutAndStderrSeparatelyAndPropagatesExitCode`)
  proving each `output` message is tagged with the pipe it actually came from and the
  final `exit` code is the child's real, unmodified exit code - even for a code that isn't
  one of the daemon's own sentinels.

## 0.3.2

Security review fixes - no intended change in normal behavior.

- **`ws://` is now rejected unless the server host is loopback.** `wss://` is always
  accepted; `ws://` is only accepted when the configured server resolves to
  `localhost`/`127.0.0.1`/`::1`. Anywhere else, `ws://` is cleartext and exposed every
  command and its output to the relay operator and the network path - `connect`,
  `aux4 remote`, and `aux4 aux4 remote list` all reject a non-loopback `ws://` URL with a
  clear error pointing at `wss://` instead of silently accepting it.
- **Closed a whitespace-based expose-policy bypass.** Policy matching and the nested
  `aux4` process's own `AUX4_SECURITY` check both evaluate `strings.Join(args, " ")`; a
  crafted arg list (e.g. an arg with a trailing space) could make that joined string no
  longer match a `deny` pattern written for the equivalent, normally-spaced command while
  the underlying argv still carried the same effective arguments. Any exec whose args
  contain an empty element, leading/trailing whitespace, a control character, or doubled
  internal whitespace is now rejected outright (exit code 126) before the expose policy is
  even evaluated, rather than silently "normalized".
- **Bounded resource use per machine.** A connected machine now runs at most 8 execs
  concurrently (configurable in code as `maxConcurrentExecs`); anything beyond that is
  rejected immediately with a clear message instead of queued. Each exec also has a
  deadline - 10 minutes by default, overridable via `AUX4_REMOTE_EXEC_TIMEOUT` (a Go
  duration string, e.g. `30s`, `2h`) - and the whole process group (not just the direct
  child) is killed on timeout or when the relay connection drops, so nothing the command
  spawned is left running unsupervised. Incoming websocket frames are capped at 8MB
  (`conn.SetReadLimit`), and command output is streamed with a growing read buffer instead
  of `bufio.Scanner`'s fixed maximum line size, so a single very long line is streamed in
  full rather than silently truncated.
- **`--security` can no longer be forwarded to the remote command line.** Flag-forwarding
  (`forwardedFlags`) now also reserves `security`/`Security`/`SECURITY`/`no-security`/
  `noSecurity`/`security.allow`/`security.ask`/`security.deny`, so a caller cannot use
  `--security` to override the *target machine's* own `expose`-derived `AUX4_SECURITY`
  policy for the nested `aux4` invocation it runs.
- **Hardened `connect`'s file handling.** `aux4 aux4 remote connect` no longer
  interpolates `dataDir`/`name` into raw shell strings (`mkdir -p ${dataDir}`, etc.) - a
  new `aux4-remote connect` subcommand does the file handling itself: it validates `name`
  against `^[A-Za-z0-9._-]+$` (no `..`, no path separator) before touching the filesystem,
  creates `dataDir` as `0700`, and writes the pid/log files as `0600`. The daemon
  (`aux4-remote client`) validates `name` the same way on its own side too, since it is
  also sent as the `X-Machine-Name` websocket header.

## 0.3.1

- **Fix: flag forwarding could leak runtime-injected object params onto the remote command line.**
  Introduced in 0.3.0, this showed up running `aux4 remote` from inside `aux4 cloud`, which injects
  object-valued `event`/`response` (and `stdin`) params into every invocation it makes. Those are now
  never forwarded: `forwardedFlags` only forwards scalar values (string/number/bool) or arrays of
  scalars - a map/object or `nil` value is refused by shape, not by name, so an unknown future
  runtime-injected object param is refused the same way. `event`, `response`, and `stdin` were also
  added to the reserved-name list as defense in depth. An array containing even one non-scalar element
  is dropped entirely rather than partially rendered.

## 0.3.0

- **Breaking: a remote machine now only ever runs `aux4` commands, never an arbitrary binary.** The
  daemon always resolves `aux4` from `PATH` and execs it with the caller's arguments as its own
  arguments (`aux4 <args...>`) — it no longer treats the caller's first argument as the program to run.
  - `aux4 remote <machine> <args...>` now sends `<args...>` as arguments TO `aux4` on `<machine>`
    (e.g. `aux4 remote build-box todo list` runs `aux4 todo list`; `aux4 remote build-box aux4 version`
    runs `aux4 aux4 version`). Previously `<args...>` named any binary to exec directly.
  - `expose` allow/ask/deny patterns are unchanged in shape (glob patterns matched against the
    space-joined arguments), but now describe an **aux4 command line without a leading `aux4`** (e.g.
    `"todo list*"`, `"aux4 version"`) instead of an arbitrary shell command line. Existing `expose`
    configs written for shell commands (e.g. `"echo *"`, `"npm run build"`) no longer match anything
    meaningful and should be rewritten against aux4 commands.
  - The denial message changed to `aux4 command "<cmdline>" is not permitted by this machine's expose
    policy` (previously `command "<cmdline>" is not permitted...`).
  - Naming a non-aux4 program in `expose.allow` (e.g. `"echo hi"`) no longer makes it runnable — it is
    still only ever passed to `aux4`, which reports "command not found".
- **Flag forwarding, mirroring `aux4 cloud <vm> <command...>`.** Any `--flag` on `aux4 remote` other
  than `--server`/`--command` is now forwarded to the remote aux4 command as its own named
  `--flag value` (e.g. `aux4 remote build-box todo list --status open` runs `aux4 todo list --status
  open` on `build-box`). Applies to the positional form only; `--command "..."` is unaffected since it
  is already a complete, explicit command line.

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
