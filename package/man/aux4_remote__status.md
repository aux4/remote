#### Description

Reports whether the local `aux4/remote` daemon for the given machine name is currently running, by
checking the pid recorded at `<dataDir>/<name>.pid`. This only checks the local process — it does not
ask the relay server whether the connection is still registered there (use `aux4 aux4 remote list`
from another machine for that).

#### Usage

```bash
aux4 aux4 remote status --name <name> [--dataDir <dir>]
```

--name      The machine name used when connecting
--dataDir   Directory where the connection's pid and log file are stored (default: `~/.aux4.config/remote`)

#### Example

```bash
aux4 aux4 remote status --name build-box
```

```text
build-box: connected (pid 4821)
```
