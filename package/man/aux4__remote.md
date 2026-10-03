#### Description

Entry point for managing `aux4/remote`: connecting this machine to a relay server, checking its
connection status, and listing which machines are currently connected. The relay server itself is the
companion `aux4/cloud-remote` package (`aux4 aux4 cloud-remote server start`). See
`aux4 aux4 man aux4:remote:<command>` for each subcommand.

#### Usage

```bash
aux4 aux4 remote <connect|disconnect|status|list> ...
```

#### Example

```bash
aux4 aux4 remote connect --configFile config.yaml --config build-box
aux4 aux4 remote status --name build-box
aux4 aux4 remote list --server ws://relay:8420/ws --token mytoken
```
