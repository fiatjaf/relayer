package elasticsearch

import (
	"bytes"
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
		IDs:   []string{"abc", "123", "971b9489b4fd4e41a85951607922b982d981fa9d55318bc304f21f390721404c"},
		Kinds: []int{0, 1},
		Tags: nostr.TagMap{
			"e": []string{"abc"},
			"p": []string{"aaa", "bbb"},
		},
		Since:  &yesterday,
		Until:  &now,
		Limit:  100,
		Search: "other stuff",
	}

	dsl, err := buildDsl(filter)
	if err != nil {
		t.Fatal(err)
	}
	pprint(dsl)

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
