# Release Notes

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
