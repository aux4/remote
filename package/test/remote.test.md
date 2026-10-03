# aux4/remote end-to-end

Exercises the full relay flow in one scenario — relay server, two machine connections, an allowed
exec, a denied exec, an offline machine, cross-principal isolation, and disconnect — since each
command only makes sense with the others already running. Requires `aux4/cloud-remote` to be
installed locally (the relay server lives in that companion package). Authentication is aux4 SSO
(`AUX4_ACCESS_TOKEN`); a fake local SSO stands in for a real login.

```file:server-config.yaml
config:
  relay:
    port: 18921
```

```file:machine-a-config.yaml
config:
  machine-a:
    name: machine-a
    server: ws://127.0.0.1:18921/ws
    expose:
      allow:
        - "echo *"
```

```file:sso-mock.js
// Tiny fake SSO: GET /userinfo accepts "Bearer token-a" -> user-a and
// "Bearer token-b" -> user-b; anything else is 401.
const http = require("http");
const USERS = { "token-a": { sub: "user-a", email: "a@example.com" }, "token-b": { sub: "user-b", email: "b@example.com" } };
const server = http.createServer((req, res) => {
  const auth = req.headers["authorization"] || "";
  const token = auth.startsWith("Bearer ") ? auth.slice(7) : "";
  const user = USERS[token];
  if (!user) {
    res.writeHead(401);
    res.end();
    return;
  }
  res.writeHead(200, { "Content-Type": "application/json" });
  res.end(JSON.stringify(user));
});
server.listen(18928, () => console.log("sso-mock listening"));
```

```afterAll
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote disconnect --name machine-a --dataDir ./.data-a 2>/dev/null
aux4 api stop 2>/dev/null
pkill -f "aux4-remote client" 2>/dev/null
pkill -f sso-mock.js 2>/dev/null
rm -rf .aux4-cloud-remote .data-a server.log sso-mock.log
true
```

## relay and machine lifecycle

### should start the relay server

```timeout
20000
```

```execute
nohup node sso-mock.js >sso-mock.log 2>&1 &
sleep 1
nohup aux4 aux4 cloud-remote server start --configFile server-config.yaml --config relay --ssoUrl http://127.0.0.1:18928 >server.log 2>&1 &
for i in $(seq 1 60); do curl -s -o /dev/null http://127.0.0.1:18921/ && break; sleep 0.25; done
curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:18921/
```

```expect
404
```

### should connect machine-a to the relay as user-a

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote connect --configFile machine-a-config.yaml --config machine-a --dataDir ./.data-a
sleep 1
aux4 aux4 remote status --name machine-a --dataDir ./.data-a
```

```expect:partial
connected
```

### should list machine-a as connected to the relay for user-a

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote list --server ws://127.0.0.1:18921/ws
```

```expect
machine-a
```

### user-b must not see user-a's machine

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-b aux4 aux4 remote list --server ws://127.0.0.1:18921/ws
```

```expect
No machines connected
```

### user-b must not be able to exec on user-a's machine

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-b aux4 remote machine-a echo hi --server ws://127.0.0.1:18921/ws
```

```error:partial
is offline
```

### should run an allowed command on machine-a and stream its output

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-a echo hello from the relay --server ws://127.0.0.1:18921/ws
```

```expect
hello from the relay
```

### should pass single-dash flags through --command

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote --command "machine-a echo -n no-newline" --server ws://127.0.0.1:18921/ws
```

```expect
no-newline
```

### should deny a command not in the expose allow list

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote --command "machine-a rm -rf /tmp/should-not-run" --server ws://127.0.0.1:18921/ws
```

```error:partial
not permitted
```

### should report an offline machine

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-z echo hi --server ws://127.0.0.1:18921/ws
```

```error:partial
is offline
```

### should reject an invalid token outright

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=not-a-real-token aux4 remote machine-a echo hi --server ws://127.0.0.1:18921/ws
```

```error:partial
Error
```

### should disconnect machine-a

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote disconnect --name machine-a --dataDir ./.data-a
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
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-a echo hi --server ws://127.0.0.1:18921/ws
```

```error:partial
is offline
```
