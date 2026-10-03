#### Description

Stops the `aux4/remote` daemon started by `aux4 aux4 remote connect` for the given machine name,
using the pid file at `<dataDir>/<name>.pid`, then removes the pid file. Exits quietly (code 0) even
if the daemon was already stopped or never started.

#### Usage

```bash
aux4 aux4 remote disconnect --name <name> [--dataDir <dir>]
```

--name      The machine name used when connecting
--dataDir   Directory where the connection's pid and log file are stored (default: `~/.aux4.config/remote`)

#### Example

```bash
aux4 aux4 remote disconnect --name build-box
```

```text
Disconnected build-box
```
