#### Description

Asks the relay server for the names of machines **you** have registered (via `aux4 aux4 remote
connect` under your own aux4 login) that are currently connected. Machines registered by a different
aux4 account never appear, even on the same relay. Useful for checking a machine is reachable before
calling `aux4 remote <machine> ...`.

#### Usage

```bash
aux4 aux4 remote list --server <url>
```

--server  Relay server websocket URL (e.g. `ws://relay.example.com:8420/ws`)

#### Example

```bash
aux4 aux4 remote list --server ws://relay.example.com:8420/ws
```

```text
build-box
laptop
```

```bash
aux4 aux4 remote list --server ws://relay.example.com:8420/ws
```

```text
No machines connected
```
