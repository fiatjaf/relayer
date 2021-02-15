module github.com/fiatjaf/nostr-relay

go 1.15

require (
	github.com/fiatjaf/go-nostr v0.0.0-00010101000000-000000000000
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/websocket v1.4.2
	github.com/jmoiron/sqlx v1.2.0
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/lib/pq v1.8.0
	github.com/mattn/go-sqlite3 v1.14.4
	github.com/rs/cors v1.7.0
	github.com/rs/zerolog v1.20.0
	google.golang.org/appengine v1.6.7 // indirect
)

replace github.com/fiatjaf/go-nostr => /home/fiatjaf/comp/go-nostr
