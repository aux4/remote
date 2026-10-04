# aux4 remote status

Exercises `aux4 aux4 remote status` in isolation — it only ever reads a local pid file, so none of
these scenarios need a relay server or a machine daemon running.

## a machine that was never connected

### should report disconnected when no pid file exists

```execute
aux4 aux4 remote status --name never-connected --dataDir ./.data-status-1
```

```expect
never-connected: disconnected
```

## a stale pid file

```beforeAll
mkdir -p ./.data-status-2
```

```afterAll
rm -rf ./.data-status-2
```

### should report disconnected when the pid file points to a dead process

```file:.data-status-2/stale-machine.pid
999999999
```

```execute
aux4 aux4 remote status --name stale-machine --dataDir ./.data-status-2
```

```expect
stale-machine: disconnected
```

### should report disconnected when the pid file is empty garbage

```file:.data-status-2/garbage-machine.pid
not-a-pid
```

```execute
aux4 aux4 remote status --name garbage-machine --dataDir ./.data-status-2
```

```expect
garbage-machine: disconnected
```

## a live process

```beforeAll
mkdir -p ./.data-status-3
nohup sleep 60 >/dev/null 2>&1 &
echo $! > ./.data-status-3/live-machine.pid
```

```afterAll
kill $(cat ./.data-status-3/live-machine.pid) 2>/dev/null || true
rm -rf ./.data-status-3
```

### should report connected with the process pid

```execute
PID=$(cat ./.data-status-3/live-machine.pid)
aux4 aux4 remote status --name live-machine --dataDir ./.data-status-3
```

```expect:regex
live-machine: connected \(pid \d+\)
```
