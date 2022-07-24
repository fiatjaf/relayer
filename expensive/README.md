expensive-relay, a sybil-free corner of nostr
=============================================

  - a nostr relay implementation based on relayer.
  - uses postgres, which I think must be over version 12 since it uses generated columns.
  - requires users to manually register themselves to be able to publish events and pay a fee. this should prevent spam.
  - aside from that it's basically the same thing as relayer basic.

running
-------

this requires a recent CLN version with Commando.

grab a binary from the releases page and run it with the following environment variables:

    POSTGRESQL_DATABASE=postgresql://...
    CLN_NODE_ID=02fed8723...
    CLN_HOST=127.0.0.1:9735
    CLN_RUNE=...
    TICKET_PRICE_SATS=500

adjust the values above accordingly.

compiling
---------

if you know Go you already know this:

    go install github.com/fiatjaf/relayer/expensive

or something like that.
