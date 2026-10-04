#### Description

Connects this machine to a relay server so other machines can run commands on it through
`aux4 remote`. Starts the `aux4/remote` daemon in the background (via `nohup`) and writes its pid to
`<dataDir>/<name>.pid`; its stdout/stderr go to `<dataDir>/<name>.log`.

The daemon dials the relay server, authenticates with `name` and your aux4 login (`AUX4_ACCESS_TOKEN`
if set, else `aux4 aux4 token` — the same helper every aux4 CLI command uses, including its automatic
refresh), and reconnects automatically if the connection drops, fetching a fresh token on every
(re)connect attempt. It never accepts inbound connections itself.

- **Reconnect backoff**: a dropped connection is retried with exponential backoff (starting at 2s,
  doubling up to a 60s cap, with up to ±30% random jitter so many machines reconnecting at once do
  not hammer the relay in lockstep). The backoff resets to the start after a successful handshake, so
  a machine that reconnects cleanly after a single drop is not penalized by earlier failures. If the
  relay rejects the handshake outright with an authentication/authorization error (401/403 — a token
  that will not become valid on its own), the daemon prints a clear error and exits non-zero instead
  of retrying forever; a plain network failure (relay unreachable, DNS, timeout) still retries with
  backoff.
- **Credential isolation**: a command this machine runs on a caller's behalf never inherits this
  daemon's own `AUX4_ACCESS_TOKEN`/`AUX4_REFRESH_TOKEN`/`AUX4_ID_TOKEN` — those are stripped from the
  executed `aux4` process's environment before it starts, regardless of what environment this daemon
  itself was started with.

- **`name`** is this machine's name, unique among the machines registered under YOUR aux4 account (two
  different aux4 users can both use the same name without conflict) — the relay authenticates the
  connection by your access token, not by a name/secret pair. It must match `^[A-Za-z0-9._-]+$` (letters,
  digits, `.`, `_`, `-` only, and never `..`) since it is used to build this connection's pid/log file
  names; an invalid name is rejected before anything is created.
- **`expose`** is this machine's own policy for which **aux4 commands** it is willing to run,
  independent of anything the relay or a caller can override. A caller can never ask this machine to
  run anything other than an aux4 command — the daemon always execs `aux4 <args...>` — so `expose`
  only ever needs to describe aux4 command lines. It is a JSON object with `allow`/`ask`/`deny` glob
  pattern arrays, matched against the aux4 command line a caller sends: the arguments after the
  machine name, space-joined, *without* a leading `aux4` (e.g. `"todo list*"` or `"aux4 version"`).
  An empty or missing `expose` (the default, `{}`) exposes **nothing** — this is fail-closed, unlike
  aux4 core's own command-exposure policy. `ask` is treated the same as `deny`, since the daemon has
  nobody to answer a confirmation prompt. The same patterns are also passed to the executed `aux4`
  process as `AUX4_SECURITY`, so aux4 core enforces the identical policy one level down as defense in
  depth.

#### Usage

```bash
aux4 aux4 remote connect --name <name> --server <url> [--expose <json>] [--dataDir <dir>]
```

--name      This machine's name, as other machines will address it (`^[A-Za-z0-9._-]+$` only)
--server    Relay server websocket URL. `wss://` for anything but localhost; `ws://` is only accepted to a loopback host (e.g. `ws://127.0.0.1:8420/ws` or `wss://relay.example.com:8420/ws`)
--expose    Allow/ask/deny glob patterns for the aux4 command lines this machine will run (JSON object, default: `{}`)
--dataDir   Directory to store the connection's pid and log file (default: `~/.aux4.config/remote`); created as `0700`, with the pid/log files written as `0600`

#### Example

Configuration file:

```yaml
config:
  build-box:
    name: build-box
    server: ws://relay.example.com:8420/ws
    expose:
      allow:
        - "aux4 pkger list*"
        - "todo list*"
```

```bash
aux4 aux4 remote connect --configFile config.yaml --config build-box
```

```text
Connecting build-box to ws://relay.example.com:8420/ws (logs: /Users/me/.aux4.config/remote/build-box.log)
```
