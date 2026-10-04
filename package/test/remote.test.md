# aux4/remote end-to-end

Exercises the full relay flow in one scenario — relay server, two machine connections, an allowed
exec, a denied exec, an offline machine, cross-principal isolation, and disconnect — since each
command only makes sense with the others already running. Requires `aux4/cloud-remote` to be
installed locally (the relay server lives in that companion package). Authentication is aux4 SSO
(`AUX4_ACCESS_TOKEN`); a fake local SSO stands in for a real login.

A machine only ever runs `aux4 <args>` — never an arbitrary binary. Every command exercised below is
a real aux4 command (`aux4 aux4 version`, etc); `echo hi` appears once, deliberately, to prove that
even naming it in the expose policy's allow list does not make it runnable — the daemon still only
ever execs `aux4`, so it becomes `aux4 echo hi` and aux4 reports "command not found".

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
        - "aux4 version*"
        - "aux4 pkger list*"
        - "echo hi"
```

```file:policy-config.yaml
config:
  machine-broad-deny:
    name: machine-broad-deny
    server: ws://127.0.0.1:18921/ws
    expose:
      allow:
        - "todo *"
      deny:
        - "todo rm*"
  machine-empty-expose:
    name: machine-empty-expose
    server: ws://127.0.0.1:18921/ws
    expose: {}
  machine-ask:
    name: machine-ask
    server: ws://127.0.0.1:18921/ws
    expose:
      allow:
        - "aux4 version"
      ask:
        - "aux4 pkger*"
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
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote disconnect --name machine-broad-deny --dataDir ./.data-broad-deny 2>/dev/null
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote disconnect --name machine-empty-expose --dataDir ./.data-empty-expose 2>/dev/null
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote disconnect --name machine-ask --dataDir ./.data-ask 2>/dev/null
aux4 api stop 2>/dev/null
pkill -f "aux4-remote client" 2>/dev/null
pkill -f sso-mock.js 2>/dev/null
rm -rf .aux4-cloud-remote .data-a .data-broad-deny .data-empty-expose .data-ask server.log sso-mock.log
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
AUX4_ACCESS_TOKEN=token-b aux4 remote machine-a aux4 version --server ws://127.0.0.1:18921/ws
```

```error:partial
is offline
```

### should run an allowed aux4 command on machine-a and stream its output

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-a aux4 version --server ws://127.0.0.1:18921/ws
```

```expect:partial
aux4 *
```

### should exit this local process with the remote command's exact exit code

`aux4 aux4 version` succeeds, so the remote command's exit code (0) must be this local `aux4 remote`
process's own exit code - not swallowed or remapped.

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-a aux4 version --server ws://127.0.0.1:18921/ws >/dev/null 2>&1
echo "exit=$?"
```

```expect
exit=0
```

### should write the remote command's stdout only to local stdout

Redirecting stderr away (`2>/dev/null`) must not affect stdout - proving the two streams are kept
separate end to end, not merged into one before being written locally.

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-a aux4 version --server ws://127.0.0.1:18921/ws 2>/dev/null
```

```expect:partial
aux4 *
```

```error
```

### should write the remote command's stderr only to local stderr, never to stdout

`echo hi` becomes `aux4 echo hi` on machine-a (see above), which fails with "Command not found" on
stderr and nothing on stdout. Redirecting stdout away (`1>/dev/null`) must still leave the error
message intact on stderr, and - crucially - redirecting stdout away must not lose or truncate it,
proving it was never written to stdout in the first place.

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-a echo hi --server ws://127.0.0.1:18921/ws 1>/dev/null
```

```expect
```

```error:partial
Command not found: echo
```

### should propagate a "command not found" exit code (127) exactly

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-a echo hi --server ws://127.0.0.1:18921/ws >/dev/null 2>&1
echo "exit=$?"
```

```expect
exit=127
```

### should propagate the expose-policy denial exit code (126) exactly

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote --command "machine-a aux4 pkger uninstall aux4/remote" --server ws://127.0.0.1:18921/ws >/dev/null 2>&1
echo "exit=$?"
```

```expect
exit=126
```

### should forward an extra --flag to the remote aux4 command, same as aux4/cloud

Any `--flag` other than `--server`/`--command` is sent along as a named parameter to the remote aux4
command, so most flags never need the shell-quoted `--command` form.

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-a aux4 pkger list --filter remote --server ws://127.0.0.1:18921/ws
```

```expect:partial
aux4/remote
```

### should pass single-dash flags through --command

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote --command "machine-a aux4 version -v" --server ws://127.0.0.1:18921/ws
```

```expect:partial
aux4 *
```

### should never run anything other than aux4, even when the expose policy names it

`echo hi` is in machine-a's expose allow list, but the daemon only ever execs `aux4` — it never treats
a caller's first argument as a binary to run. So this becomes `aux4 echo hi` on machine-a, and aux4
itself reports the command does not exist. Nothing outside aux4 ever runs.

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-a echo hi --server ws://127.0.0.1:18921/ws
```

```error:partial
Command not found: echo
```

### should deny a command not in the expose allow list

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote --command "machine-a aux4 pkger uninstall aux4/remote" --server ws://127.0.0.1:18921/ws
```

```error:partial
is not permitted
```

### should report an offline machine

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-z aux4 version --server ws://127.0.0.1:18921/ws
```

```error:partial
is offline
```

### should reject an invalid token outright

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=not-a-real-token aux4 remote machine-a aux4 version --server ws://127.0.0.1:18921/ws
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
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-a aux4 version --server ws://127.0.0.1:18921/ws
```

```error:partial
is offline
```

### should connect three more machines to exercise expose policy edge cases

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote connect --configFile policy-config.yaml --config machine-broad-deny --dataDir ./.data-broad-deny
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote connect --configFile policy-config.yaml --config machine-empty-expose --dataDir ./.data-empty-expose
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote connect --configFile policy-config.yaml --config machine-ask --dataDir ./.data-ask
sleep 1
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote list --server ws://127.0.0.1:18921/ws
```

```expect:partial
machine-broad-deny
```

### should allow a command matched only by a broad allow glob

A `deny` glob is more specific than the `allow` glob it overrides, so it wins even though `allow`
would otherwise have matched too.

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-broad-deny todo list --server ws://127.0.0.1:18921/ws
```

```expect
No todos found.
```

### should deny a command matched by the more specific deny glob

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-broad-deny todo rm 1 --server ws://127.0.0.1:18921/ws
```

```error:partial
is not permitted
```

### should deny every command when the expose policy is empty

An empty `expose` (no `allow`/`ask`/`deny` at all) fails closed: this machine runs nothing, even a
command as harmless as `aux4 version`.

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-empty-expose aux4 version --server ws://127.0.0.1:18921/ws
```

```error:partial
is not permitted
```

### should allow a command matched by allow on the ask-configured machine

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-ask aux4 version --server ws://127.0.0.1:18921/ws
```

```expect:partial
aux4 *
```

### should deny a command matched only by ask, since the daemon cannot prompt unattended

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 remote machine-ask aux4 pkger list --server ws://127.0.0.1:18921/ws
```

```error:partial
is not permitted
```

### should disconnect the policy edge-case machines

```timeout
10000
```

```execute
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote disconnect --name machine-broad-deny --dataDir ./.data-broad-deny
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote disconnect --name machine-empty-expose --dataDir ./.data-empty-expose
AUX4_ACCESS_TOKEN=token-a aux4 aux4 remote disconnect --name machine-ask --dataDir ./.data-ask
```

```expect
Disconnected 'machine-broad-deny'
Disconnected 'machine-empty-expose'
Disconnected 'machine-ask'
```
