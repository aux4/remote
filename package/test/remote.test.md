# aux4/remote end-to-end

Exercises the full relay flow in one scenario — relay server, two machine connections, an allowed
exec, a denied exec, an offline machine, and disconnect — since each command only makes sense with
the others already running. Requires `aux4/cloud-remote` to be installed locally (the relay server
lives in that companion package).

```file:server-config.yaml
config:
  relay:
    port: 18921
    machines:
      machine-a: token-a
      machine-b: token-b
```

```file:machine-a-config.yaml
config:
  machine-a:
    name: machine-a
    server: ws://127.0.0.1:18921/ws
    token: token-a
    expose:
      allow:
        - "echo *"
```

```afterAll
aux4 aux4 remote disconnect --name machine-a --dataDir ./.data-a 2>/dev/null
aux4 api stop 2>/dev/null
pkill -f "aux4-remote client" 2>/dev/null
pkill -f "aux4-cloud-remote hook" 2>/dev/null
rm -rf .aux4-cloud-remote .data-a server.log
true
```

## relay and machine lifecycle

### should start the relay server

```timeout
20000
```

```execute
nohup aux4 aux4 cloud-remote server start --configFile server-config.yaml --config relay >server.log 2>&1 &
for i in $(seq 1 60); do curl -s -o /dev/null http://127.0.0.1:18921/ && break; sleep 0.25; done
curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:18921/
```

```expect
404
```

### should connect machine-a to the relay

```timeout
10000
```

```execute
aux4 aux4 remote connect --configFile machine-a-config.yaml --config machine-a --dataDir ./.data-a
sleep 1
aux4 aux4 remote status --name machine-a --dataDir ./.data-a
```

```expect:partial
connected
```

### should list machine-a as connected to the relay

```timeout
10000
```

```execute
aux4 aux4 remote list --server ws://127.0.0.1:18921/ws --token token-a
```

```expect
machine-a
```

### should run an allowed command on machine-a and stream its output

```timeout
10000
```

```execute
aux4 remote machine-a echo hello from the relay --server ws://127.0.0.1:18921/ws --token token-a
```

```expect
hello from the relay
```

### should pass single-dash flags through --command

```timeout
10000
```

```execute
aux4 remote --command "machine-a echo -n no-newline" --server ws://127.0.0.1:18921/ws --token token-a
```

```expect
no-newline
```

### should deny a command not in the expose allow list

```timeout
10000
```

```execute
aux4 remote --command "machine-a rm -rf /tmp/should-not-run" --server ws://127.0.0.1:18921/ws --token token-a
```

```error:partial
not permitted
```

### should report an offline machine

```timeout
10000
```

```execute
aux4 remote machine-z echo hi --server ws://127.0.0.1:18921/ws --token token-a
```

```error:partial
is offline
```

### should disconnect machine-a

```timeout
10000
```

```execute
aux4 aux4 remote disconnect --name machine-a --dataDir ./.data-a
sleep 1
aux4 aux4 remote status --name machine-a --dataDir ./.data-a
```

```expect:partial
machine-a: disconnected
```

### should report machine-a offline once disconnected

```timeout
10000
```

```execute
aux4 remote machine-a echo hi --server ws://127.0.0.1:18921/ws --token token-a
```

```error:partial
is offline
```
