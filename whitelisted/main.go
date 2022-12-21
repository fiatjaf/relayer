package main

import (
	"encoding/json"

	"github.com/fiatjaf/relayer"
	"github.com/fiatjaf/relayer/storage/postgresql"
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

func (r *Relay) OnInitialized() {}

func (r *Relay) Storage() relayer.Storage {
	return r.storage
}

func (r *Relay) Init() error {
	return nil
}

func (r *Relay) AcceptEvent(evt *nostr.Event) bool {
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
		relayer.Log.Fatal().Err(err).Msg("failed to read from env")
		return
	}
	r.storage = &postgresql.PostgresBackend{DatabaseURL: r.PostgresDatabase}
	relayer.Start(&r)
}
