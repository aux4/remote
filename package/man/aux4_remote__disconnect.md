#### Description

Stops the `aux4/remote` daemon started by `aux4 aux4 remote connect` for the given machine name,
using the pid file at `<dataDir>/<name>.pid`, then removes the pid file. Exits quietly (code 0) even
if the daemon was already stopped or never started, or if the pid file's content is stale or
unreadable.

- **`name` validation** - `name` is validated with the exact same rule `connect`/`status` use: only
  letters, digits, `.`, `_`, and `-` are permitted, and it must not contain `..`. A rejected name
  prints an error to stderr and exits non-zero without touching `dataDir` at all.
- **Scoped signal** - the daemon's pid is signaled directly (not its whole process group), bounding
  the effect to that one process even in the (normally impossible, but not provably so once a pid
  file is involved) case where the recorded pid has been reused by something else.

#### Usage

```bash
aux4 aux4 remote disconnect --name <name> [--dataDir <dir>]
```

--name      The machine name used when connecting. Must match `^[A-Za-z0-9._-]+$` and must not contain `..`
--dataDir   Directory where the connection's pid and log file are stored (default: `~/.aux4.config/remote`)

#### Example

```bash
aux4 aux4 remote disconnect --name build-box
```

```text
Disconnected 'build-box'
```
