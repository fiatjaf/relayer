module github.com/fiatjaf/relayer

go 1.18

require (
	github.com/PuerkitoBio/goquery v1.8.0
	github.com/cockroachdb/pebble v0.0.0-20220723153705-3fc374e4dc66
	github.com/fiatjaf/go-nostr v0.7.3
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/websocket v1.4.2
	github.com/grokify/html-strip-tags-go v0.0.1
	github.com/jb55/lnsocket/go v0.0.0-20220315220004-e1e6b88a0bfc
	github.com/jmoiron/sqlx v1.3.1
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/mmcdole/gofeed v1.1.3
	github.com/rif/cache2go v1.0.0
	github.com/rs/cors v1.7.0
	github.com/rs/zerolog v1.20.0
	github.com/stevelacy/daz v0.1.4
	github.com/tidwall/gjson v1.14.1
)

require (
	github.com/DataDog/zstd v1.4.5 // indirect
	github.com/HdrHistogram/hdrhistogram-go v1.1.2 // indirect
	github.com/aead/siphash v1.0.1 // indirect
	github.com/andybalholm/cascadia v1.3.1 // indirect
	github.com/btcsuite/btcd v0.23.1 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.2.0 // indirect
	github.com/btcsuite/btcd/btcutil v1.1.1 // indirect
	github.com/btcsuite/btcd/btcutil/psbt v1.1.4 // indirect
	github.com/btcsuite/btcd/chaincfg/chainhash v1.0.1 // indirect
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f // indirect
	github.com/btcsuite/btcwallet v0.15.1 // indirect
	github.com/btcsuite/btcwallet/wallet/txauthor v1.2.3 // indirect
	github.com/btcsuite/btcwallet/wallet/txrules v1.2.0 // indirect
	github.com/btcsuite/btcwallet/wallet/txsizes v1.1.0 // indirect
	github.com/btcsuite/btcwallet/walletdb v1.4.0 // indirect
	github.com/btcsuite/btcwallet/wtxmgr v1.5.0 // indirect
	github.com/btcsuite/go-socks v0.0.0-20170105172521-4720035b7bfd // indirect
	github.com/btcsuite/websocket v0.0.0-20150119174127-31079b680792 // indirect
	github.com/cespare/xxhash/v2 v2.1.1 // indirect
	github.com/cockroachdb/errors v1.8.1 // indirect
	github.com/cockroachdb/logtags v0.0.0-20190617123548-eb05cc24525f // indirect
	github.com/cockroachdb/redact v1.0.8 // indirect
	github.com/cockroachdb/sentry-go v0.6.1-cockroachdb.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.0.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/decred/dcrd/lru v1.0.0 // indirect
	github.com/go-errors/errors v1.0.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/json-iterator/go v1.1.11 // indirect
	github.com/kkdai/bstream v1.0.0 // indirect
	github.com/klauspost/compress v1.13.6 // indirect
	github.com/kr/pretty v0.1.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/lightninglabs/gozmq v0.0.0-20191113021534-d20a764486bf // indirect
	github.com/lightninglabs/neutrino v0.14.2 // indirect
	github.com/lightningnetwork/lnd v0.15.0-beta // indirect
	github.com/lightningnetwork/lnd/clock v1.1.0 // indirect
	github.com/lightningnetwork/lnd/queue v1.1.0 // indirect
	github.com/lightningnetwork/lnd/ticker v1.1.0 // indirect
	github.com/lightningnetwork/lnd/tlv v1.0.3 // indirect
	github.com/lightningnetwork/lnd/tor v1.0.1 // indirect
	github.com/miekg/dns v1.1.43 // indirect
	github.com/mmcdole/goxpp v0.0.0-20181012175147-0068e33feabf // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	github.com/valyala/fastjson v1.6.3 // indirect
	golang.org/x/crypto v0.0.0-20210921155107-089bfa567519 // indirect
	golang.org/x/exp v0.0.0-20220426173459-3bcf042a4bf5 // indirect
	golang.org/x/net v0.0.0-20211015210444-4f30a5c0130f // indirect
	golang.org/x/sys v0.0.0-20220520151302-bc2c85ada10a // indirect
	golang.org/x/term v0.0.0-20201126162022-7de9c90e9dd1 // indirect
	golang.org/x/text v0.3.7 // indirect
)

replace github.com/jb55/lnsocket/go => /home/fiatjaf/comp/lnsocket/go
