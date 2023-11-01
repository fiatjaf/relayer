package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/eventstore/postgresql"
	"github.com/fiatjaf/relayer/v2"
	"github.com/kelseyhightower/envconfig"
	_ "github.com/lib/pq"
	"github.com/nbd-wtf/go-nostr"
)

type Relay struct {
	PostgresDatabase string `envconfig:"POSTGRESQL_DATABASE"`
	CLNNodeId        string `envconfig:"CLN_NODE_ID"`
	CLNHost          string `envconfig:"CLN_HOST"`
	CLNRune          string `envconfig:"CLN_RUNE"`
	TicketPriceSats  int64  `envconfig:"TICKET_PRICE_SATS"`

	storage *postgresql.PostgresBackend
}

var r = &Relay{}

func (r *Relay) Name() string {
	return "ExpensiveRelay"
}

func (r *Relay) Storage(ctx context.Context) eventstore.Store {
	return r.storage
}

func (r *Relay) Init() error {
	// every hour, delete all very old events
	go func() {
		db := r.Storage(context.TODO()).(*postgresql.PostgresBackend)

		for {
			time.Sleep(60 * time.Minute)
			db.DB.Exec(`DELETE FROM event WHERE created_at < $1`, time.Now().AddDate(0, -3, 0).Unix()) // 6 months
		}
	}()

	return nil
}

func (r *Relay) AcceptEvent(ctx context.Context, evt *nostr.Event) bool {
	// only accept they have a good preimage for a paid invoice for their public key
	if !checkInvoicePaidOk(evt.PubKey) {
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
	// special handlers
	server.Router().HandleFunc("/", handleWebpage)
	server.Router().HandleFunc("/invoice", func(w http.ResponseWriter, rq *http.Request) {
		handleInvoice(w, rq, &r)
	})
	if err := server.Start("0.0.0.0", 7447); err != nil {
		log.Fatalf("server terminated: %v", err)
	}
}
