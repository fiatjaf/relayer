rss-bridge, a relay that creates virtual nostr profiles for each rss feed
=========================================================================

  - a nostr relay implementation based on relayer.
  - doesn't accept any events, only emits them.
  - does so by manually reading and parsing rss feeds.

![](screenshot.png)

running
-------

grab a binary from the releases page and run it with the following environment variable:

    SECRET=just-a-random-string-to-be-used-when-generating-the-virtual-private-keys

it will create a local database file to store the currently known rss feed urls.

compiling
---------

if you know Go you already know this:

    go install github.com/fiatjaf/relayer/rss-bridge

or something like that.
