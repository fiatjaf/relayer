package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"

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

func buildDsl(filter *nostr.Filter) string {
	b := &strings.Builder{}
	b.WriteString(`{"query": {"bool": {"filter": {"bool": {"must": [`)

	prefixFilter := func(fieldName string, values []string) {
		b.WriteString(`{"bool": {"should": [`)
		for idx, val := range values {
			if idx > 0 {
				b.WriteRune(',')
			}
			op := "term"
			if len(val) < 64 {
				op = "prefix"
			}
			b.WriteString(fmt.Sprintf(`{"%s": {"event.%s": %q}}`, op, fieldName, val))
		}
		b.WriteString(`]}},`)
	}

	// ids
	prefixFilter("id", filter.IDs)

	// authors
	prefixFilter("pubkey", filter.Authors)

	// kinds
	if len(filter.Kinds) > 0 {
		k, _ := json.Marshal(filter.Kinds)
		b.WriteString(fmt.Sprintf(`{"terms": {"event.kind": %s}},`, k))
	}

	// tags
	{
		b.WriteString(`{"bool": {"should": [`)
		commaIdx := 0
		for char, terms := range filter.Tags {
			if len(terms) == 0 {
				continue
			}
			if commaIdx > 0 {
				b.WriteRune(',')
			}
			commaIdx++
			b.WriteString(`{"bool": {"must": [`)
			for _, t := range terms {
				b.WriteString(fmt.Sprintf(`{"term": {"event.tags": %q}},`, t))
			}
			// add the tag type at the end
			b.WriteString(fmt.Sprintf(`{"term": {"event.tags": %q}}`, char))
			b.WriteString(`]}}`)
		}
		b.WriteString(`]}},`)
	}

	// since
	if filter.Since != nil {
		b.WriteString(fmt.Sprintf(`{"range": {"event.created_at": {"gt": %d}}},`, filter.Since.Unix()))
	}

	// until
	if filter.Until != nil {
		b.WriteString(fmt.Sprintf(`{"range": {"event.created_at": {"lt": %d}}},`, filter.Until.Unix()))
	}

	// search
	if filter.Search != "" {
		b.WriteString(fmt.Sprintf(`{"match": {"content_search": {"query": %s}}},`, filter.Search))
	}

	// all blocks have a trailing comma...
	// add a match_all "noop" at the end
	// so json is valid
	b.WriteString(`{"match_all": {}}`)
	b.WriteString(`]}}}}}`)
	return b.String()
}

func (ess *ElasticsearchStorage) getByID(filter *nostr.Filter) ([]nostr.Event, error) {
	got, err := ess.es.Mget(
		esutil.NewJSONReader(filter),
		ess.es.Mget.WithIndex(ess.indexName))
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
	// Perform the search request...
	// need to build up query body...
	if filter == nil {
		return nil, errors.New("filter cannot be null")
	}

	// optimization: get by id
	if isGetByID(filter) {
		return ess.getByID(filter)
	}

	dsl := buildDsl(filter)
	pprint([]byte(dsl))

	limit := 1000
	if filter.Limit > 0 && filter.Limit < limit {
		limit = filter.Limit
	}

	es := ess.es
	res, err := es.Search(
		es.Search.WithContext(context.Background()),
		es.Search.WithIndex(ess.indexName),

		es.Search.WithBody(strings.NewReader(dsl)),
		es.Search.WithSize(limit),
		es.Search.WithSort("event.created_at:desc"),

		// es.Search.WithTrackTotalHits(true),
		// es.Search.WithPretty(),
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

func pprint(j []byte) {
	var dst bytes.Buffer
	err := json.Indent(&dst, j, "", "    ")
	if err != nil {
		fmt.Println("invalid JSON", err, string(j))
	} else {
		fmt.Println(dst.String())
	}
}
