package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/eventstore/sqlite3"
	"github.com/fiatjaf/relayer/v2"
	"github.com/kelseyhightower/envconfig"
	"github.com/nbd-wtf/go-nostr"
	"github.com/redis/go-redis/v9"
)

// Relay is a relay meant to be run as several processes sharing one SQLite
// database file. SQLite has no way to tell the other processes that a row was
// inserted, so accepted events are also published on a Redis channel and every
// instance delivers what it receives from there to its own subscribers.
type Relay struct {
	SQLiteDatabase string `envconfig:"SQLITE_DATABASE" default:"relay.db"`
	RedisURL       string `envconfig:"REDIS_URL" default:"redis://127.0.0.1:6379"`
	RedisChannel   string `envconfig:"REDIS_CHANNEL" default:"nostr:events"`
	Port           int    `envconfig:"PORT" default:"7447"`

	storage *sqlite3.SQLite3Backend
	rdb     *redis.Client
}

func (r *Relay) Name() string {
	return "MultiInstanceRelay"
}

func (r *Relay) Storage(ctx context.Context) eventstore.Store {
	return r.storage
}

func (r *Relay) Init() error {
	return r.rdb.Ping(context.Background()).Err()
}

func (r *Relay) AcceptEvent(ctx context.Context, evt *nostr.Event) (bool, string) {
	return true, ""
}

// Notify publishes every accepted event, so that all instances (this one
// included) pick it up in Notifications.
func (r *Relay) Notify(ctx context.Context, evt *nostr.Event) error {
	b, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return r.rdb.Publish(ctx, r.RedisChannel, b).Err()
}

// Notifications subscribes to the channel and hands the events over to the
// server, which matches them against the subscriptions of its own clients.
func (r *Relay) Notifications(ctx context.Context) (<-chan *nostr.Event, error) {
	sub := r.rdb.Subscribe(ctx, r.RedisChannel)
	// wait for the subscription to be confirmed before returning, so that
	// events published from now on are not missed
	if _, err := sub.Receive(ctx); err != nil {
		sub.Close()
		return nil, err
	}

	ch := make(chan *nostr.Event)
	go func() {
		defer close(ch)
		defer sub.Close()
		// sub.Channel reconnects by itself if the connection drops
		msgs := sub.Channel()
		for {
			select {
			case msg, ok := <-msgs:
				if !ok {
					return
				}
				var evt nostr.Event
				if err := json.Unmarshal([]byte(msg.Payload), &evt); err != nil {
					log.Printf("dropping malformed notification: %v", err)
					continue
				}
				select {
				case ch <- &evt:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (r *Relay) OnShutdown(ctx context.Context) {
	r.rdb.Close()
}

func main() {
	r := Relay{}
	if err := envconfig.Process("", &r); err != nil {
		log.Fatalf("failed to read from env: %v", err)
	}

	opts, err := redis.ParseURL(r.RedisURL)
	if err != nil {
		log.Fatalf("invalid REDIS_URL: %v", err)
	}
	r.rdb = redis.NewClient(opts)

	// WAL mode lets several processes read and write the same file, and the
	// busy timeout makes writers wait for each other instead of failing.
	r.storage = &sqlite3.SQLite3Backend{
		DatabaseURL: fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000", r.SQLiteDatabase),
	}

	server, err := relayer.NewServer(&r)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}
	if err := server.Start("0.0.0.0", r.Port); err != nil {
		log.Fatalf("server terminated: %v", err)
	}
}
