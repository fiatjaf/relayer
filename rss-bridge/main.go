package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/fiatjaf/relayer"
	"github.com/kelseyhightower/envconfig"
	"github.com/nbd-wtf/go-nostr"
	"golang.org/x/exp/slices"
)

var relay = &Relay{
	updates: make(chan nostr.Event),
}

type Relay struct {
	Secret string `envconfig:"SECRET" required:"true"`

	updates     chan nostr.Event
	lastEmitted sync.Map
	db          *pebble.DB
}

func (relay *Relay) Name() string {
	return "relayer-rss-bridge"
}

func (r *Relay) OnInitialized(s *relayer.Server) {
	s.Router().Path("/").HandlerFunc(handleWebpage)
	s.Router().Path("/create").HandlerFunc(handleCreateFeed)
}

func (relay *Relay) Init() error {
	err := envconfig.Process("", relay)
	if err != nil {
		return fmt.Errorf("couldn't process envconfig: %w", err)
	}

	if db, err := pebble.Open("db", nil); err != nil {
		log.Fatalf("failed to open db: %v", err)
	} else {
		relay.db = db
	}

	go func() {
		time.Sleep(20 * time.Minute)

		filters := relayer.GetListeningFilters()
		log.Printf("checking for updates; %d filters active", len(filters))

		for _, filter := range filters {
			if filter.Kinds == nil || slices.Contains(filter.Kinds, nostr.KindTextNote) {
				for _, pubkey := range filter.Authors {
					if val, closer, err := relay.db.Get([]byte(pubkey)); err == nil {
						defer closer.Close()

						var entity Entity
						if err := json.Unmarshal(val, &entity); err != nil {
							log.Printf("got invalid json from db at key %s: %v", pubkey, err)
							continue
						}

						feed, err := parseFeed(entity.URL)
						if err != nil {
							log.Printf("failed to parse feed at url %q: %v", entity.URL, err)
							continue
						}

						for _, item := range feed.Items {
							evt := itemToTextNote(pubkey, item)
							last, ok := relay.lastEmitted.Load(entity.URL)
							if !ok || time.Unix(last.(int64), 0).Before(evt.CreatedAt.Time()) {
								evt.Sign(entity.PrivateKey)
								relay.updates <- evt
								relay.lastEmitted.Store(entity.URL, last)
							}
						}
					}
				}
			}
		}
	}()

	return nil
}

func (relay *Relay) AcceptEvent(_ *nostr.Event) bool {
	return false
}

func (relay *Relay) Storage() relayer.Storage {
	return store{relay.db}
}

type store struct {
	db *pebble.DB
}

func (b store) Init() error { return nil }
func (b store) SaveEvent(_ *nostr.Event) error {
	return errors.New("blocked: we don't accept any events")
}

func (b store) DeleteEvent(_, _ string) error {
	return errors.New("blocked: we can't delete any events")
}

func (b store) QueryEvents(filter *nostr.Filter) ([]nostr.Event, error) {
	var evts []nostr.Event

	if filter.IDs != nil || len(filter.Tags) > 0 {
		return evts, nil
	}

	for _, pubkey := range filter.Authors {
		if val, closer, err := relay.db.Get([]byte(pubkey)); err == nil {
			defer closer.Close()

			var entity Entity
			if err := json.Unmarshal(val, &entity); err != nil {
				log.Printf("got invalid json from db at key %s: %v", pubkey, err)
				continue
			}

			feed, err := parseFeed(entity.URL)
			if err != nil {
				log.Printf("failed to parse feed at url %q: %v", entity.URL, err)
				continue
			}

			if filter.Kinds == nil || slices.Contains(filter.Kinds, nostr.KindSetMetadata) {
				evt := feedToSetMetadata(pubkey, feed)

				if filter.Since != nil && evt.CreatedAt.Time().Before(filter.Since.Time()) {
					continue
				}
				if filter.Until != nil && evt.CreatedAt.Time().After(filter.Until.Time()) {
					continue
				}

				evt.Sign(entity.PrivateKey)
				evts = append(evts, evt)
			}

			if filter.Kinds == nil || slices.Contains(filter.Kinds, nostr.KindTextNote) {
				var last uint32 = 0
				for _, item := range feed.Items {
					evt := itemToTextNote(pubkey, item)

					if filter.Since != nil && evt.CreatedAt.Time().Before(filter.Since.Time()) {
						continue
					}
					if filter.Until != nil && evt.CreatedAt.Time().After(filter.Until.Time()) {
						continue
					}

					evt.Sign(entity.PrivateKey)

					if evt.CreatedAt.Time().After(time.Unix(int64(last), 0)) {
						last = uint32(evt.CreatedAt.Time().Unix())
					}

					evts = append(evts, evt)
				}

				relay.lastEmitted.Store(entity.URL, last)
			}
		}
	}

	return evts, nil
}

func (relay *Relay) InjectEvents() chan nostr.Event {
	return relay.updates
}

func main() {
	if err := relayer.Start(relay); err != nil {
		log.Fatalf("server terminated: %v", err)
	}
}
