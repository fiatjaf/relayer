Nostr Relay Framework -- use it to implement your own custom relay.

There is an example/reference implementation at [basic](/examples/basic/). Binaries for that are also available under [Releases](
https://github.com/fiatjaf/relayer/releases).

<a href="https://godoc.org/github.com/fiatjaf/relayer"><img src="https://img.shields.io/badge/api-reference-blue.svg?style=flat-square" alt="GoDoc"></a>

## Supported NIPs

These are handled by the framework itself, so every relay built on it gets them:

| NIP | |
| --- | --- |
| [01](https://github.com/nostr-protocol/nips/blob/master/01.md) | Basic protocol flow (`EVENT`, `REQ`, `CLOSE`, `EOSE`, `OK`, `CLOSED`, `NOTICE`), including what used to be NIPs 12, 15, 16, 20 and 33 |
| [09](https://github.com/nostr-protocol/nips/blob/master/09.md) | Event deletion request |
| [11](https://github.com/nostr-protocol/nips/blob/master/11.md) | Relay information document |
| [67](https://github.com/nostr-protocol/nips/blob/master/67.md) | EOSE completeness hint |
| [77](https://github.com/nostr-protocol/nips/blob/master/77.md) | Negentropy syncing (`NEG-OPEN`, `NEG-MSG`, `NEG-CLOSE`) |

These depend on what your relay implements:

| NIP | requires |
| --- | --- |
| [42](https://github.com/nostr-protocol/nips/blob/master/42.md) | `Auther` — authentication of clients to relays |
| [17](https://github.com/nostr-protocol/nips/blob/master/17.md), [59](https://github.com/nostr-protocol/nips/blob/master/59.md) | `Auther` — gift-wrapped events are only served to the pubkey they are addressed to |
| [45](https://github.com/nostr-protocol/nips/blob/master/45.md) | a storage implementing `EventCounter` — `COUNT` |

The default NIP-11 document reports exactly this set. A relay implementing
`Informationer` replaces the document wholesale and is responsible for its own
`supported_nips`.

Note that the numbers 12, 15, 16, 20 and 33 still appear in that document for
the benefit of older clients; those NIPs were merged into NIP-01 and no longer
exist on their own.

## Running several instances on one storage

Live subscriptions are tracked in memory, so out of the box an event published
to one instance is only pushed to the clients of that instance. To run several
instances (behind a load balancer, say) against one shared storage, implement
`Notifier` on your relay or on your storage:

```go
type Notifier interface {
	Notify(context.Context, *nostr.Event) error
	Notifications(context.Context) (<-chan *nostr.Event, error)
}
```

`Notify` is called for every accepted event; `Notifications` must deliver the
events accepted by every instance, this one included. With a `Notifier` present,
events reach local subscribers only through `Notifications`, so there is a single
delivery path and no duplicates.

`Notifier` is `eventstore.Notifier`, and the `postgresql` storage implements it
with `LISTEN`/`NOTIFY`, so a relay on PostgreSQL gets this for free. For any other
storage the transport is up to you — Redis pub/sub, NATS, anything that carries
events from `Notify` on one instance to `Notifications` on all of them; see
[multi-instance](/examples/multi-instance/) for SQLite kept in sync over Redis.
