# aux4 remote connect

Exercises `aux4 aux4 remote connect` in isolation: machine-name validation and the
permissions it creates its dataDir/pid/log files with. `server` points at an address
nothing is listening on, so the background daemon just sits retrying - these tests only
care what `connect` itself does before and around starting it, not the relay handshake
(covered end-to-end in `remote.test.md`).

```afterAll
aux4 aux4 remote disconnect --name valid-name --dataDir ./.data-connect-valid 2>/dev/null
rm -rf ./.data-connect-valid ./.data-connect-invalid-1 ./.data-connect-invalid-2 ./.data-connect-ws-rejected
```

## an invalid machine name

### should reject a name containing a path separator

```execute
aux4 aux4 remote connect --name "../escape" --server ws://127.0.0.1:19999/ws --dataDir ./.data-connect-invalid-1
```

```error:partial
invalid
```

### should not create the dataDir for a rejected name

```execute
test -d ./.data-connect-invalid-1 && echo "created" || echo "not created"
```

```expect
not created
```

### should reject a name containing internal whitespace

```execute
aux4 aux4 remote connect --name "has space" --server ws://127.0.0.1:19999/ws --dataDir ./.data-connect-invalid-2
```

```error:partial
invalid
```

## a non-loopback ws:// server URL

### should reject it before creating anything

```execute
aux4 aux4 remote connect --name reject-ws --server ws://relay.example.com/ws --dataDir ./.data-connect-ws-rejected
```

```error:partial
wss://
```

### should not create the dataDir for the rejected URL

```execute
test -d ./.data-connect-ws-rejected && echo "created" || echo "not created"
```

```expect
not created
```

## a valid machine name

```timeout
10000
```

### should create dataDir as 0700 and the pid/log files as 0600

```execute
aux4 aux4 remote connect --name valid-name --server ws://127.0.0.1:19999/ws --dataDir ./.data-connect-valid
sleep 1
stat -f "%Sp" ./.data-connect-valid 2>/dev/null || stat -c "%A" ./.data-connect-valid
stat -f "%Sp" ./.data-connect-valid/valid-name.pid 2>/dev/null || stat -c "%A" ./.data-connect-valid/valid-name.pid
stat -f "%Sp" ./.data-connect-valid/valid-name.log 2>/dev/null || stat -c "%A" ./.data-connect-valid/valid-name.log
```

```expect:partial
drwx------
-rw-------
-rw-------
```
