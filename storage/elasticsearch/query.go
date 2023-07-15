package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"

	"github.com/aquasecurity/esquery"
	"github.com/elastic/go-elasticsearch/v8/esutil"
	"github.com/nbd-wtf/go-nostr"
)

type EsSearchResult struct {
	Took     int
	TimedOut bool `json:"timed_out"`
	Hits     struct {
		Total struct {
			Value    int
			Relation string
		}
		Hits []struct {
			Source IndexedEvent `json:"_source"`
		}
	}
}

type EsCountResult struct {
	Count int64
}

func buildDsl(filter *nostr.Filter) ([]byte, error) {
	dsl := esquery.Bool()

	prefixFilter := func(fieldName string, values []string) {
		if len(values) == 0 {
			return
		}
		prefixQ := esquery.Bool()
		for _, v := range values {
			if len(v) < 64 {
				prefixQ.Should(esquery.Prefix(fieldName, v))
			} else {
				prefixQ.Should(esquery.Term(fieldName, v))
			}
		}
		dsl.Must(prefixQ)
	}

	// ids
	prefixFilter("event.id", filter.IDs)

	// authors
	prefixFilter("event.pubkey", filter.Authors)

	// kinds
	if len(filter.Kinds) > 0 {
		dsl.Must(esquery.Terms("event.kind", toInterfaceSlice(filter.Kinds)...))
	}

	// tags
	if len(filter.Tags) > 0 {
		tagQ := esquery.Bool()
		for char, terms := range filter.Tags {
			vs := toInterfaceSlice(append(terms, char))
			tagQ.Should(esquery.Terms("event.tags", vs...))
		}
		dsl.Must(tagQ)
	}

	// since
	if filter.Since != nil {
		dsl.Must(esquery.Range("event.created_at").Gte(filter.Since))
	}

	// until
	if filter.Until != nil {
		dsl.Must(esquery.Range("event.created_at").Lte(filter.Until))
	}

	// search
	if filter.Search != "" {
		dsl.Must(esquery.Match("content_search", filter.Search))
	}

	return json.Marshal(esquery.Query(dsl))
}

func (ess *ElasticsearchStorage) getByID(filter *nostr.Filter) ([]*nostr.Event, error) {
	got, err := ess.es.Mget(
		esutil.NewJSONReader(filter),
		ess.es.Mget.WithIndex(ess.IndexName))
	if err != nil {
		return nil, err
	}

	var mgetResponse struct {
		Docs []struct {
			Found  bool
			Source IndexedEvent `json:"_source"`
		}
	}
	if err := json.NewDecoder(got.Body).Decode(&mgetResponse); err != nil {
		return nil, err
	}

	events := make([]*nostr.Event, 0, len(mgetResponse.Docs))
	for _, e := range mgetResponse.Docs {
		if e.Found {
			events = append(events, &e.Source.Event)
		}
	}

	return events, nil
}

func (ess *ElasticsearchStorage) QueryEvents(ctx context.Context, filter *nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event)

	if filter == nil {
		return nil, errors.New("filter cannot be null")
	}

	// optimization: get by id
	if isGetByID(filter) {
		if evts, err := ess.getByID(filter); err == nil {
			for _, evt := range evts {
				ch <- evt
			}
			close(ch)
		} else {
			return nil, fmt.Errorf("error getting by id: %w", err)
		}
	}

	dsl, err := buildDsl(filter)
	if err != nil {
		return nil, err
	}

	limit := 1000
	if filter.Limit > 0 && filter.Limit < limit {
		limit = filter.Limit
	}

	es := ess.es
	res, err := es.Search(
		es.Search.WithContext(ctx),
		es.Search.WithIndex(ess.IndexName),

		es.Search.WithBody(bytes.NewReader(dsl)),
		es.Search.WithSize(limit),
		es.Search.WithSort("event.created_at:desc"),
	)
	if err != nil {
		log.Fatalf("Error getting response: %s", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		txt, _ := io.ReadAll(res.Body)
		fmt.Println("oh no", string(txt))
		return nil, fmt.Errorf("%s", txt)
	}

	var r EsSearchResult
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return nil, err
	}

	go func() {
		for _, e := range r.Hits.Hits {
			ch <- &e.Source.Event
		}
		close(ch)
	}()

	return ch, nil
}

func isGetByID(filter *nostr.Filter) bool {
	isGetById := len(filter.IDs) > 0 &&
		len(filter.Authors) == 0 &&
		len(filter.Kinds) == 0 &&
		len(filter.Tags) == 0 &&
		len(filter.Search) == 0 &&
		filter.Since == nil &&
		filter.Until == nil

	if isGetById {
		for _, id := range filter.IDs {
			if len(id) != 64 {
				return false
			}
		}
	}
	return isGetById
}

// from: https://stackoverflow.com/a/12754757
func toInterfaceSlice(slice interface{}) []interface{} {
	s := reflect.ValueOf(slice)
	if s.Kind() != reflect.Slice {
		panic("InterfaceSlice() given a non-slice type")
	}

	// Keep the distinction between nil and empty slice input
	if s.IsNil() {
		return nil
	}

	ret := make([]interface{}, s.Len())

	for i := 0; i < s.Len(); i++ {
		ret[i] = s.Index(i).Interface()
	}

	return ret
}

func (ess *ElasticsearchStorage) CountEvents(ctx context.Context, filter *nostr.Filter) (int64, error) {
	if filter == nil {
		return 0, errors.New("filter cannot be null")
	}

	count := int64(0)

	// optimization: get by id
	if isGetByID(filter) {
		if evts, err := ess.getByID(filter); err == nil {
			count += int64(len(evts))
		} else {
			return 0, fmt.Errorf("error getting by id: %w", err)
		}
	}

	dsl, err := buildDsl(filter)
	if err != nil {
		return 0, err
	}

	es := ess.es
	res, err := es.Count(
		es.Count.WithContext(ctx),
		es.Count.WithIndex(ess.IndexName),

		es.Count.WithBody(bytes.NewReader(dsl)),
	)
	if err != nil {
		log.Fatalf("Error getting response: %s", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		txt, _ := io.ReadAll(res.Body)
		fmt.Println("oh no", string(txt))
		return 0, fmt.Errorf("%s", txt)
	}

	var r EsCountResult
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return 0, err
	}

	return r.Count + count, nil
}
