package main

import (
	"encoding/json"
	"fmt"

	"github.com/fiatjaf/go-nostr"
	"github.com/fiatjaf/relayer"
	"github.com/fiatjaf/relayer/storage/postgresql"
	"github.com/kelseyhightower/envconfig"
)

type Relay struct {
	PostgresDatabase string   `envconfig:"POSTGRESQL_DATABASE"`
	Whitelist        []string `envconfig:"WHITELIST"`
}

func (r *Relay) Name() string {
	return "WhitelistedRelay"
}

func (r *Relay) OnInitialized() {}

func (r *Relay) Storage() relayer.Storage {
	return &postgresql.PostgresBackend{DatabaseURL: r.PostgresDatabase}
}

func (r *Relay) Init() error {
	err := envconfig.Process("", r)
	if err != nil {
		return fmt.Errorf("couldn't process envconfig: %w", err)
	}

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
	relayer.Start(&Relay{})
}
