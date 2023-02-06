package elasticsearch

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

func TestQuery(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(time.Hour * -24)
	filter := &nostr.Filter{
		// IDs:   []string{"abc", "123", "971b9489b4fd4e41a85951607922b982d981fa9d55318bc304f21f390721404c"},
		Kinds: []int{0, 1},
		// Tags: nostr.TagMap{
		// 	"a": []string{"abc"},
		// 	"b": []string{"aaa", "bbb"},
		// },
		Since: &yesterday,
		Until: &now,
		Limit: 100,
	}

	dsl := buildDsl(filter)
	pprint([]byte(dsl))

	if !json.Valid([]byte(dsl)) {
		t.Fail()
	}

	// "integration" test
	ess := &ElasticsearchStorage{}
	err := ess.Init()
	if err != nil {
		t.Error(err)
	}

	found, err := ess.QueryEvents(filter)
	if err != nil {
		t.Error(err)
	}
	fmt.Println(found)
}
