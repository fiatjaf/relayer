```
bzip2 -cd nostr-wellorder-early-1m-v1.jsonl.bz2 | \
  jq -c '["EVENT", .]' | \
  awk 'length($0)<131072' | \
  websocat -n -B 200000 ws://127.0.0.1:7447
```

todo:

* index `content_search` field
* support search queries
* some kind of ranking signal (based on pubkey)
* better config for ES: adjust bulk indexer settings, use custom mapping?
