package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esutil"
	"github.com/fiatjaf/relayer/v2"
	"github.com/nbd-wtf/go-nostr"
)

var _ relayer.Storage = (*ElasticsearchStorage)(nil)

type IndexedEvent struct {
	Event         nostr.Event `json:"event"`
	ContentSearch string      `json:"content_search"`
}

var indexMapping = `
{
	"settings": {
		"number_of_shards": 1,
		"number_of_replicas": 0
	},
	"mappings": {
		"dynamic": false,
		"properties": {
			"event": {
				"dynamic": false,
				"properties": {
					"id": {"type": "keyword"},
					"pubkey": {"type": "keyword"},
					"kind": {"type": "integer"},
					"tags": {"type": "keyword"},
					"created_at": {"type": "date"}
				}
			},
			"content_search": {"type": "text"}
		}
	}
}
`

type ElasticsearchStorage struct {
	IndexName string

	es *elasticsearch.Client
	bi esutil.BulkIndexer
}

func (ess *ElasticsearchStorage) Init() error {
	if ess.IndexName == "" {
		ess.IndexName = "events"
	}

	cfg := elasticsearch.Config{}
	if x := os.Getenv("ES_URL"); x != "" {
		cfg.Addresses = strings.Split(x, ",")
	}
	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return err
	}

	res, err := es.Indices.Create(ess.IndexName, es.Indices.Create.WithBody(strings.NewReader(indexMapping)))
	if err != nil {
		return err
	}
	if res.IsError() {
		body, _ := io.ReadAll(res.Body)
		txt := string(body)
		if !strings.Contains(txt, "resource_already_exists_exception") {
			return fmt.Errorf("%s", txt)
		}
	}

	// bulk indexer
	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Index:         ess.IndexName,
		Client:        es,
		NumWorkers:    2,
		FlushInterval: 3 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("error creating the indexer: %s", err)
	}

	ess.es = es
	ess.bi = bi

	return nil
}

func (ess *ElasticsearchStorage) DeleteEvent(ctx context.Context, id string, pubkey string) error {
	// first do get by ID and check that pubkeys match
	// this is cheaper than doing delete by query, which also doesn't work with bulk indexer.
	found, _ := ess.getByID(&nostr.Filter{IDs: []string{id}})
	if len(found) == 0 || found[0].PubKey != pubkey {
		return nil
	}

	done := make(chan error)
	err := ess.bi.Add(
		ctx,
		esutil.BulkIndexerItem{
			Action:     "delete",
			DocumentID: id,
			OnSuccess: func(ctx context.Context, item esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem) {
				close(done)
			},
			OnFailure: func(ctx context.Context, item esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem, err error) {
				if err != nil {
					done <- err
				} else {
					// ok if deleted item not found
					if res.Status == 404 {
						close(done)
						return
					}
					txt, _ := json.Marshal(res)
					err := fmt.Errorf("ERROR: %s", txt)
					done <- err
				}
			},
		},
	)
	if err != nil {
		return err
	}

	err = <-done
	return err
}

func (ess *ElasticsearchStorage) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	ie := &IndexedEvent{
		Event: *evt,
	}

	// post processing: index for FTS
	// some ideas:
	// - index kind=0 fields a set of dedicated mapped fields
	//   (or use a separate index for profiles with a dedicated mapping)
	// - if it's valid JSON just index the "values" and not the keys
	// - more content introspection: language detection
	// - denormalization... attach profile + ranking signals to events
	if evt.Kind != 4 {
		ie.ContentSearch = evt.Content
	}

	data, err := json.Marshal(ie)
	if err != nil {
		return err
	}

	done := make(chan error)

	// delete replaceable events
	deleteIDs := []string{}
	queryForDelete := func(filter *nostr.Filter) {
		toDelete, _ := ess.QueryEvents(ctx, filter)
		for e := range toDelete {
			// KindRecommendServer: we can't query ES for exact content match
			// so query by kind and loop over results to compare content
			if evt.Kind == nostr.KindRecommendServer {
				if e.Content == evt.Content {
					deleteIDs = append(deleteIDs, e.ID)
				}
			} else {
				deleteIDs = append(deleteIDs, e.ID)
			}
		}
	}
	if evt.Kind == nostr.KindSetMetadata || evt.Kind == nostr.KindContactList || evt.Kind == nostr.KindRecommendServer || (10000 <= evt.Kind && evt.Kind < 20000) {
		// delete past events from this user
		queryForDelete(&nostr.Filter{
			Authors: []string{evt.PubKey},
			Kinds:   []int{evt.Kind},
		})
	} else if evt.Kind >= 30000 && evt.Kind < 40000 {
		// NIP-33
		d := evt.Tags.GetFirst([]string{"d"})
		if d != nil {
			queryForDelete(&nostr.Filter{
				Authors: []string{evt.PubKey},
				Kinds:   []int{evt.Kind},
				Tags: nostr.TagMap{
					"d": []string{d.Value()},
				},
			})
		}
	}
	for _, id := range deleteIDs {
		ess.bi.Add(
			ctx,
			esutil.BulkIndexerItem{
				Action:     "delete",
				DocumentID: id,
			})
	}

	// adapted from:
	// https://github.com/elastic/go-elasticsearch/blob/main/_examples/bulk/indexer.go#L196
	err = ess.bi.Add(
		ctx,
		esutil.BulkIndexerItem{
			Action:     "index",
			DocumentID: evt.ID,
			Body:       bytes.NewReader(data),
			OnSuccess: func(ctx context.Context, item esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem) {
				close(done)
			},
			OnFailure: func(ctx context.Context, item esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem, err error) {
				if err != nil {
					done <- err
				} else {
					err := fmt.Errorf("ERROR: %s: %s", res.Error.Type, res.Error.Reason)
					done <- err
				}
			},
		},
	)
	if err != nil {
		return err
	}

	err = <-done
	return err
}
