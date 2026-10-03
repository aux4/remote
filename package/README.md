# aux4/remote

Run aux4 (or any shell) commands on another machine through an [`aux4/cloud-remote`](https://hub.aux4.io)
relay server, without opening inbound ports on either side. Machines dial out to the relay and
register under a name; any caller logged in to aux4 can run a command on a machine **it registered
itself** and stream its output back — authentication and ownership are both your aux4 login, not a
separate per-machine secret.

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
        - "echo *"
        - "uname *"
        - "npm run build"
```

```bash
aux4 aux4 remote connect --configFile config.yaml --config build-box
```

From `laptop` (or anywhere, as long as you are logged in to the same aux4 account `build-box` connected
under):

```bash
aux4 remote build-box uname -a
aux4 remote build-box npm run build --server ws://relay.example.com:8420/ws
```

The exit code of `aux4 remote` is the exit code of the command on the remote machine.

**Note:** aux4's own CLI parser consumes a bare `-x` token as one of its own options, so
`aux4 remote build-box ls -la /tmp` loses everything from `-la` onward. For commands with
single-dash flags, use `--command` with the machine name and command in one shell-quoted string:
`aux4 remote --command "build-box ls -la /tmp"`.

## Configuration

```yaml
config:
  # this machine's identity and what it is willing to run
  build-box:
    name: build-box               # unique among machines YOU have registered (see Authentication below)
    server: ws://relay:8420/ws    # the relay server's websocket URL (see aux4/cloud-remote)
    expose:                       # allow/ask/deny glob patterns matched against "arg0 arg1 ..."
      allow:
        - "echo *"
        - "npm run build"
      deny:
        - "rm *"
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

- The relay server ([`aux4/cloud-remote`](https://hub.aux4.io)) validates every connection's access
  token against aux4 SSO, keeps a registry of which connection belongs to which (principal, machine
  name) pair, and routes exec requests and their output between callers and machines. It can run
  standalone wherever it is downloaded, or be deployed to an aux4 cloud VM.
- A **machine** connects out to the relay (`aux4 aux4 remote connect`) and authenticates with its
  owner's aux4 access token and the machine's name.
- A **caller** (`aux4 remote <machine> <cmd...>`) opens its own connection to the relay with its own
  access token, asks the relay to route an exec request to the named machine (only machines registered
  under the SAME aux4 account are visible), and streams back `stdout`/`stderr` as the command runs,
  finishing with the command's real exit code.
- The machine never runs anything through a shell — each command is executed as its own process
  (`argv[0]`, `argv[1:]...`), so a malicious or malformed remote argument cannot break out into shell
  syntax.

## Exposure Policy (`expose`)

Every machine decides for itself what it will run. The `expose` config is matched against the
space-joined command the caller sent (e.g. `npm run build`), using the same `allow` / `ask` / `deny`
glob semantics as aux4 core's command-exposure policy — the most specific matching pattern wins, and
on a tie `deny` beats `ask` beats `allow`.

Two rules specific to `aux4/remote`:

- **Fail closed.** An empty or missing `expose.allow` list means the machine runs nothing at all —
  there is no "allow everything" default here, unlike aux4 core's own policy.
- **`ask` is `deny`.** The daemon runs unattended with nobody to answer a prompt, so any command that
  would otherwise prompt for confirmation is refused instead.

A denied command never spawns a process; the caller gets a non-zero exit code and an error message
explaining the command was not permitted.

If the command a caller runs is itself an `aux4` invocation, the machine also sets `AUX4_SECURITY` in
its environment to the same policy, so a nested `aux4` command is bound by the same exposure rules.

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

- Commands run with real `argv`, never through a shell string — remote input cannot inject shell
  syntax.
- Access tokens are never logged.
- The relay operator can see every command and its output in transit. Use `wss://` (TLS) in anything
  but a fully trusted private network — `ws://` is in cleartext.
- A machine with an empty `expose` config is safe by default: it is connected but will run nothing.
