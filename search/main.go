package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/fiatjaf/relayer"
	"github.com/fiatjaf/relayer/storage/elasticsearch"
	"github.com/kelseyhightower/envconfig"
	"github.com/nbd-wtf/go-nostr"
)

type Relay struct {
	storage *elasticsearch.ElasticsearchStorage
}

func (r *Relay) Name() string {
	return "BasicRelay"
}

func (r *Relay) Storage() relayer.Storage {
	return r.storage
}

func (r *Relay) OnInitialized(*relayer.Server) {}

func (r *Relay) Init() error {
	err := envconfig.Process("", r)
	if err != nil {
		return fmt.Errorf("couldn't process envconfig: %w", err)
	}

	return nil
}

func (r *Relay) AcceptEvent(evt *nostr.Event) bool {
	// block events that are too large
	jsonb, _ := json.Marshal(evt)
	if len(jsonb) > 100000 {
		return false
	}

	return true
}

func (r *Relay) BeforeSave(evt *nostr.Event) {
	// do nothing
}

func (r *Relay) AfterSave(evt *nostr.Event) {

}

func main() {
	r := Relay{}
	if err := envconfig.Process("", &r); err != nil {
		log.Fatalf("failed to read from env: %v", err)
		return
	}
	r.storage = &elasticsearch.ElasticsearchStorage{}
	if err := relayer.Start(&r); err != nil {
		log.Fatalf("server terminated: %v", err)
	}
}
