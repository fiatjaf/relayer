# multi-instance

Several relay processes sharing one SQLite database, kept in sync with Redis
pub/sub so that a subscription on any instance receives the events published to
any other instance.

Each process implements `relayer.Notifier`: `Notify` publishes accepted events
on a Redis channel and `Notifications` subscribes to it. The framework then
delivers only what comes back from Redis, so every instance — including the one
the event was published to — sees each event exactly once.

## Running

Start Redis, then a few instances on different ports:

```
go build -o multi-instance .
PORT=7447 ./multi-instance &
PORT=7448 ./multi-instance &
PORT=7449 ./multi-instance &
```

Subscribe on one and publish on another:

```
nak req -k 1 --stream ws://localhost:7448 &
nak event -k 1 -c 'hello' ws://localhost:7447
```

Environment:

| variable | default |
| --- | --- |
| `SQLITE_DATABASE` | `relay.db` |
| `REDIS_URL` | `redis://127.0.0.1:6379` |
| `REDIS_CHANNEL` | `nostr:events` |
| `PORT` | `7447` |
