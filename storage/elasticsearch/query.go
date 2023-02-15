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
		dsl.Must(esquery.Range("event.created_at").Gt(filter.Since.Unix()))
	}

	// until
	if filter.Until != nil {
		dsl.Must(esquery.Range("event.created_at").Lt(filter.Until.Unix()))
	}

	// search
	if filter.Search != "" {
		dsl.Must(esquery.Match("content_search", filter.Search))
	}

	return json.Marshal(esquery.Query(dsl))
}

func (ess *ElasticsearchStorage) getByID(filter *nostr.Filter) ([]nostr.Event, error) {
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

	events := make([]nostr.Event, 0, len(mgetResponse.Docs))
	for _, e := range mgetResponse.Docs {
		if e.Found {
			events = append(events, e.Source.Event)
		}
	}

	return events, nil
}

func (ess *ElasticsearchStorage) QueryEvents(filter *nostr.Filter) ([]nostr.Event, error) {
	if filter == nil {
		return nil, errors.New("filter cannot be null")
	}

	// optimization: get by id
	if isGetByID(filter) {
		return ess.getByID(filter)
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
		es.Search.WithContext(context.Background()),
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

	events := make([]nostr.Event, len(r.Hits.Hits))
	for i, e := range r.Hits.Hits {
		events[i] = e.Source.Event
	}

	return events, nil
}

func isGetByID(filter *nostr.Filter) bool {
	isGetById := len(filter.IDs) > 0 &&
		len(filter.Authors) == 0 &&
		len(filter.Kinds) == 0 &&
		len(filter.Tags) == 0 &&
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
