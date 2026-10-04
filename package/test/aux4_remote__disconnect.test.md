# aux4 remote disconnect

Exercises `aux4 aux4 remote disconnect` in isolation — disconnecting only kills whatever pid is
recorded locally (if any) and removes the pid file, so none of these scenarios need a relay server
or a real machine daemon.

## a machine that was never connected

### should be a no-op and not error

```execute
aux4 aux4 remote disconnect --name never-connected --dataDir ./.data-disconnect-1
```

```expect
Disconnected 'never-connected'
```

## a machine with a live daemon process

```beforeAll
mkdir -p ./.data-disconnect-2
nohup sleep 60 >/dev/null 2>&1 &
echo $! > ./.data-disconnect-2/live-machine.pid
```

```afterAll
rm -rf ./.data-disconnect-2
```

### should stop the process and remove the pid file

```execute
aux4 aux4 remote disconnect --name live-machine --dataDir ./.data-disconnect-2
```

```expect
Disconnected 'live-machine'
```

### should report disconnected afterwards

```execute
aux4 aux4 remote status --name live-machine --dataDir ./.data-disconnect-2
```

```expect
live-machine: disconnected
```

## a machine with a stale pid file

```beforeAll
mkdir -p ./.data-disconnect-3
echo 999999999 > ./.data-disconnect-3/stale-machine.pid
```

```afterAll
rm -rf ./.data-disconnect-3
```

### should not error even though the process is already gone

```execute
aux4 aux4 remote disconnect --name stale-machine --dataDir ./.data-disconnect-3
```

```expect
Disconnected 'stale-machine'
```

### should remove the stale pid file

```execute
test -f ./.data-disconnect-3/stale-machine.pid && echo "still there" || echo "removed"
```

```expect
removed
```
