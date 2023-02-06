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

	"github.com/elastic/go-elasticsearch/esapi"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/nbd-wtf/go-nostr"
)

/*
1. create index with mapping in Init
2. implement delete
3. build query in QueryEvents
4. implement replaceable events
*/

var indexMapping = `
{
	"settings": {
		"number_of_shards": 1,
		"number_of_replicas": 0
	},
	"mappings": {
		"dynamic": false,
		"properties": {
			"id": {"type": "keyword"},
			"pubkey": {"type": "keyword"},
			"kind": {"type": "integer"},
			"tags": {"type": "keyword"},
			"created_at": {"type": "date"}
		}
	}
}
`

type ElasticsearchStorage struct {
	es        *elasticsearch.Client
	indexName string
}

func (ess *ElasticsearchStorage) Init() error {
	es, err := elasticsearch.NewDefaultClient()
	if err != nil {
		return err
	}
	// log.Println(elasticsearch.Version)
	// log.Println(es.Info())

	// todo: config
	ess.indexName = "test"

	// todo: don't delete index every time
	// es.Indices.Delete([]string{ess.indexName})

	res, err := es.Indices.Create(ess.indexName, es.Indices.Create.WithBody(strings.NewReader(indexMapping)))
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

	ess.es = es

	return nil
}

type EsSearchResult struct {
	Took     int
	TimedOut bool `json:"timed_out"`
	Hits     struct {
		Total struct {
			Value    int
			Relation string
		}
		Hits []struct {
			Source nostr.Event `json:"_source"`
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
			b.WriteString(fmt.Sprintf(`{"%s": {"%s": %q}}`, op, fieldName, val))
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
		b.WriteString(fmt.Sprintf(`{"terms": {"kind": %s}},`, k))
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
				b.WriteString(fmt.Sprintf(`{"term": {"tags": %q}},`, t))
			}
			// add the tag type at the end
			b.WriteString(fmt.Sprintf(`{"term": {"tags": %q}}`, char))
			b.WriteString(`]}}`)
		}
		b.WriteString(`]}},`)
	}

	// since
	if filter.Since != nil {
		b.WriteString(fmt.Sprintf(`{"range": {"created_at": {"gt": %d}}},`, filter.Since.Unix()))
	}

	// until
	if filter.Until != nil {
		b.WriteString(fmt.Sprintf(`{"range": {"created_at": {"lt": %d}}},`, filter.Until.Unix()))
	}

	// all blocks have a trailing comma...
	// add a match_all "noop" at the end
	// so json is valid
	b.WriteString(`{"match_all": {}}`)
	b.WriteString(`]}}}}}`)
	return b.String()
}

func (ess *ElasticsearchStorage) QueryEvents(filter *nostr.Filter) ([]nostr.Event, error) {
	// Perform the search request...
	// need to build up query body...
	if filter == nil {
		return nil, errors.New("filter cannot be null")
	}

	dsl := buildDsl(filter)
	// pprint([]byte(dsl))

	es := ess.es
	res, err := es.Search(
		es.Search.WithContext(context.Background()),
		es.Search.WithIndex(ess.indexName),

		es.Search.WithBody(strings.NewReader(dsl)),
		es.Search.WithSize(filter.Limit),
		es.Search.WithSort("created_at:desc"),

		es.Search.WithTrackTotalHits(true),
		es.Search.WithPretty(),
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
		events[i] = e.Source
	}

	return events, nil
}

func (ess *ElasticsearchStorage) DeleteEvent(id string, pubkey string) error {
	// todo: is pubkey match required?
	res, err := ess.es.Delete(ess.indexName, id)
	if err != nil {
		return err
	}
	if res.IsError() {
		txt, _ := io.ReadAll(res.Body)
		return fmt.Errorf("%s", txt)
	}
	return nil
}

func (ess *ElasticsearchStorage) SaveEvent(event *nostr.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	req := esapi.IndexRequest{
		Index:      ess.indexName,
		DocumentID: event.ID,
		Body:       bytes.NewReader(data),
		// Refresh:    "true",
	}

	_, err = req.Do(context.Background(), ess.es)
	return err
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
