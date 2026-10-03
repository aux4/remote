#### Description

Runs a command on a remote machine through an `aux4/remote` relay server and streams its output back.
The first positional argument is the target machine's name (as it registered with `aux4 aux4 remote
connect`); every argument after it is the command and its arguments, passed through untouched — no
shell is involved on either side, so quoting rules are the same as running the command locally.

`aux4 remote` exits with the same exit code the remote command exited with. If the command is denied
by the target machine's `expose` policy, the machine is offline, or the relay cannot be reached, the
command exits non-zero and prints an error to stderr.

**Single-dash flags**: aux4's own CLI parser treats a bare `-x` token as one of *its* options, so a
command like `aux4 remote build-box ls -la /tmp` loses everything from `-la` onward. For any command
that needs single-dash flags, use `--command` instead and put the machine name and the whole command
in one shell-quoted string: `aux4 remote --command "build-box ls -la /tmp"`. Long `--flags` on the
remote command do not have this problem and work fine positionally.

- **`--server`** and **`--token`** identify the relay server to connect to and the bearer token to
  authenticate with. They are typically supplied via `--configFile`/`--config` pointing at the same
  config file used for `aux4 aux4 remote connect`.
- Standard output and standard error from the remote command are streamed back as they are produced,
  not buffered until completion.

#### Usage

```bash
aux4 remote <machine> <command> [args...] [--server <url>] [--token <token>]
aux4 remote --command "<machine> <command> [args...]" [--server <url>] [--token <token>]
```

--command  Alternative to the positional form, as one shell-quoted string (needed for single-dash flags)
--server   Relay server websocket URL (e.g. `ws://relay.example.com:8420/ws`)
--token    Bearer token for the relay server

#### Example

```bash
aux4 remote build-box uname -a --config build-box --configFile config.yaml
```

```text
Linux build-box 6.8.0 x86_64 GNU/Linux
```

```bash
aux4 remote build-box rm -rf /important --config build-box --configFile config.yaml
```

```text
Error: command "rm -rf /important" is not permitted by this machine's expose policy
```
