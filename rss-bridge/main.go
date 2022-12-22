package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/nbd-wtf/go-nostr"
	"github.com/fiatjaf/relayer"
	"github.com/kelseyhightower/envconfig"
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

func (r *Relay) OnInitialized() {}

func (relay *Relay) Init() error {
	err := envconfig.Process("", relay)
	if err != nil {
		return fmt.Errorf("couldn't process envconfig: %w", err)
	}

	if db, err := pebble.Open("db", nil); err != nil {
		relayer.Log.Fatal().Err(err).Str("path", "db").Msg("failed to open db")
	} else {
		relay.db = db
	}

	relayer.Router.Path("/").HandlerFunc(handleWebpage)
	relayer.Router.Path("/create").HandlerFunc(handleCreateFeed)

	go func() {
		time.Sleep(20 * time.Minute)

		filters := relayer.GetListeningFilters()
		relayer.Log.Info().Int("filters active", len(filters)).
			Msg("checking for updates")

		for _, filter := range filters {
			if filter.Kinds == nil || filter.Kinds.Contains(nostr.KindTextNote) {
				for _, pubkey := range filter.Authors {
					if val, closer, err := relay.db.Get([]byte(pubkey)); err == nil {
						defer closer.Close()

						var entity Entity
						if err := json.Unmarshal(val, &entity); err != nil {
							relayer.Log.Error().Err(err).Str("key", pubkey).
								Str("val", string(val)).
								Msg("got invalid json from db")
							continue
						}

						feed, err := parseFeed(entity.URL)
						if err != nil {
							relayer.Log.Warn().Err(err).Str("url", entity.URL).
								Msg("failed to parse feed")
							continue
						}

						for _, item := range feed.Items {
							evt := itemToTextNote(pubkey, item)
							last, ok := relay.lastEmitted.Load(entity.URL)
							if !ok || time.Unix(last.(int64), 0).Before(evt.CreatedAt) {
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

func (b store) Init() error                    { return nil }
func (b store) SaveEvent(_ *nostr.Event) error { return errors.New("we don't accept any events") }
func (b store) DeleteEvent(_, _ string) error  { return errors.New("we can't delete any events") }

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
				relayer.Log.Error().Err(err).Str("key", pubkey).Str("val", string(val)).
					Msg("got invalid json from db")
				continue
			}

			feed, err := parseFeed(entity.URL)
			if err != nil {
				relayer.Log.Warn().Err(err).Str("url", entity.URL).
					Msg("failed to parse feed")
				continue
			}

			if filter.Kinds == nil || filter.Kinds.Contains(nostr.KindSetMetadata) {
				evt := feedToSetMetadata(pubkey, feed)

				if filter.Since != nil && evt.CreatedAt.Before(*filter.Since) {
					continue
				}
				if filter.Until != nil && evt.CreatedAt.After(*filter.Until) {
					continue
				}

				evt.Sign(entity.PrivateKey)
				evts = append(evts, evt)
			}

			if filter.Kinds == nil || filter.Kinds.Contains(nostr.KindTextNote) {
				var last uint32 = 0
				for _, item := range feed.Items {
					evt := itemToTextNote(pubkey, item)

					if filter.Since != nil && evt.CreatedAt.Before(*filter.Since) {
						continue
					}
					if filter.Until != nil && evt.CreatedAt.After(*filter.Until) {
						continue
					}

					evt.Sign(entity.PrivateKey)

					if evt.CreatedAt.After(time.Unix(int64(last), 0)) {
						last = uint32(evt.CreatedAt.Unix())
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
		relayer.Log.Fatal().Err(err).Msg("server terminated")
	}
}
