package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/fiatjaf/relayer/v2"
	"github.com/fiatjaf/relayer/v2/storage/postgresql"
	"github.com/kelseyhightower/envconfig"
	"github.com/nbd-wtf/go-nostr"
)

type Relay struct {
	PostgresDatabase string   `envconfig:"POSTGRESQL_DATABASE"`
	Whitelist        []string `envconfig:"WHITELIST"`

	storage *postgresql.PostgresBackend
}

func (r *Relay) Name() string {
	return "WhitelistedRelay"
}

func (r *Relay) Storage(ctx context.Context) relayer.Storage {
	return r.storage
}

func (r *Relay) Init() error {
	return nil
}

func (r *Relay) AcceptEvent(ctx context.Context, evt *nostr.Event) bool {
	// disallow anything from non-authorized pubkeys
	found := false
	for _, pubkey := range r.Whitelist {
		if pubkey == evt.PubKey {
			found = true
			break
		}
	}
	if !found {
		return false
	}

	// block events that are too large
	jsonb, _ := json.Marshal(evt)
	if len(jsonb) > 100000 {
		return false
	}

	return true
}

func main() {
	r := Relay{}
	if err := envconfig.Process("", &r); err != nil {
		log.Fatalf("failed to read from env: %v", err)
		return
	}
	r.storage = &postgresql.PostgresBackend{DatabaseURL: r.PostgresDatabase}
	server, err := relayer.NewServer(&r)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}
	if err := server.Start("0.0.0.0", 7447); err != nil {
		log.Fatalf("server terminated: %v", err)
	}
}
