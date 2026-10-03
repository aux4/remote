#### Description

Asks the relay server for the names of machines currently connected (registered via
`aux4 aux4 remote connect`). Useful for checking a machine is reachable before calling
`aux4 remote <machine> ...`.

#### Usage

```bash
aux4 aux4 remote list --server <url> --token <token>
```

--server  Relay server websocket URL (e.g. `ws://relay.example.com:8420/ws`)
--token   Any valid bearer token configured on the relay server

#### Example

```bash
aux4 aux4 remote list --server ws://relay.example.com:8420/ws --token mytoken
```

```text
build-box
laptop
```

```bash
aux4 aux4 remote list --server ws://relay.example.com:8420/ws --token mytoken
```

```text
No machines connected
```
