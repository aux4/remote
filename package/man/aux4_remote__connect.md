#### Description

Connects this machine to a relay server so other machines can run commands on it through
`aux4 remote`. Starts the `aux4/remote` daemon in the background (via `nohup`) and writes its pid to
`<dataDir>/<name>.pid`; its stdout/stderr go to `<dataDir>/<name>.log`.

The daemon dials the relay server, authenticates with `name` and your aux4 login (`AUX4_ACCESS_TOKEN`
if set, else `aux4 aux4 token` — the same helper every aux4 CLI command uses, including its automatic
refresh), and reconnects automatically (with a short backoff) if the connection drops, fetching a
fresh token on every (re)connect attempt. It never accepts inbound connections itself.

- **`name`** is this machine's name, unique among the machines registered under YOUR aux4 account (two
  different aux4 users can both use the same name without conflict) — the relay authenticates the
  connection by your access token, not by a name/secret pair.
- **`expose`** is this machine's own policy for which commands it is willing to run, independent of
  anything the relay or a caller can override. It is a JSON object with `allow`/`ask`/`deny` glob
  pattern arrays, matched against the space-joined command a caller sends (e.g. `"npm run build"`).
  An empty or missing `expose` (the default, `{}`) exposes **nothing** — this is fail-closed, unlike
  aux4 core's own command-exposure policy. `ask` is treated the same as `deny`, since the daemon has
  nobody to answer a confirmation prompt.

#### Usage

```bash
aux4 aux4 remote connect --name <name> --server <url> [--expose <json>] [--dataDir <dir>]
```

--name      This machine's name, as other machines will address it
--server    Relay server websocket URL (e.g. `ws://relay.example.com:8420/ws`)
--expose    Allow/ask/deny glob patterns for commands this machine will run (JSON object, default: `{}`)
--dataDir   Directory to store the connection's pid and log file (default: `~/.aux4.config/remote`)

#### Example

Configuration file:

```yaml
config:
  build-box:
    name: build-box
    server: ws://relay.example.com:8420/ws
    expose:
      allow:
        - "npm run build"
        - "uname *"
```

```bash
aux4 aux4 remote connect --configFile config.yaml --config build-box
```

```text
Connecting build-box to ws://relay.example.com:8420/ws (logs: /Users/me/.aux4.config/remote/build-box.log)
```
