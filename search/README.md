# Search Relay

Uses ElasticSearch storage backend for all queries, with some basic full text search support.

Index some events:

```
bzip2 -cd nostr-wellorder-early-1m-v1.jsonl.bz2 | \
  jq -c '["EVENT", .]' | \
  awk 'length($0)<131072' | \
  websocat -n -B 200000 ws://127.0.0.1:7447
```

Do a search:

```
echo '["REQ", "asdf", {"search": "steve", "kinds": [0]}]' | websocat -n ws://127.0.0.1:7447
```


## Customize

Currently the indexing is very basic:  It will index the `contents` field for all events where kind != 4.
Some additional mapping and pre-processing could add better support for different content types.
See comments in `storage/elasticsearch/elasticsearch.go`.

