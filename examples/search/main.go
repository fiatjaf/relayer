package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fiatjaf/relayer/v2"
	"github.com/fiatjaf/relayer/v2/storage/elasticsearch"
	"github.com/kelseyhightower/envconfig"
	"github.com/nbd-wtf/go-nostr"
)

type Relay struct {
	storage *elasticsearch.ElasticsearchStorage
}

func (r *Relay) Name() string {
	return "SearchRelay"
}

func (r *Relay) Storage(ctx context.Context) relayer.Storage {
	return r.storage
}

func (r *Relay) Init() error {
	err := envconfig.Process("", r)
	if err != nil {
		return fmt.Errorf("couldn't process envconfig: %w", err)
	}

	return nil
}

func (r *Relay) AcceptEvent(ctx context.Context, evt *nostr.Event) bool {
	// block events that are too large
	// jsonb, _ := json.Marshal(evt)
	// if len(jsonb) > 100000 {
	// 	return false
	// }

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
	server, err := relayer.NewServer(&r)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}
	if err := server.Start("0.0.0.0", 7447); err != nil {
		log.Fatalf("server terminated: %v", err)
	}
}
