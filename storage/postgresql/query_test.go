package postgresql

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/nbd-wtf/go-nostr"
	"github.com/stretchr/testify/assert"
)

var defaultBackend = PostgresBackend{
	QueryLimit:        queryLimit,
	QueryIDsLimit:     queryIDsLimit,
	QueryAuthorsLimit: queryAuthorsLimit,
	QueryKindsLimit:   queryKindsLimit,
	QueryTagsLimit:    queryTagsLimit,
}

func TestQueryEventsSql(t *testing.T) {
	var tests = []struct {
		name    string
		backend PostgresBackend
		filter  *nostr.Filter
		query   string
		params  []any
		err     error
	}{
		{
			name:    "empty filter",
			backend: defaultBackend,
			filter:  &nostr.Filter{},
			query:   "SELECT id, pubkey, created_at, kind, tags, content, sig FROM event WHERE true ORDER BY created_at DESC LIMIT $1",
			params:  []any{100},
			err:     nil,
		},
		{
			name:    "valid filter limit",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Limit: 50,
			},
			query:  "SELECT id, pubkey, created_at, kind, tags, content, sig FROM event WHERE true ORDER BY created_at DESC LIMIT $1",
			params: []any{50},
			err:    nil,
		},
		{
			name:    "too large filter limit",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Limit: 2000,
			},
			query:  "SELECT id, pubkey, created_at, kind, tags, content, sig FROM event WHERE true ORDER BY created_at DESC LIMIT $1",
			params: []any{100},
			err:    nil,
		},
		{
			name:    "ids filter",
			backend: defaultBackend,
			filter: &nostr.Filter{
				IDs: []string{"083ec57f36a7b39ab98a57bedab4f85355b2ee89e4b205bed58d7c3ef9edd294"},
			},
			query: `SELECT id, pubkey, created_at, kind, tags, content, sig 
			FROM event 
			WHERE (id LIKE '083ec57f36a7b39ab98a57bedab4f85355b2ee89e4b205bed58d7c3ef9edd294%') 
			ORDER BY created_at DESC LIMIT $1`,
			params: []any{100},
			err:    nil,
		},
		{
			name:    "kind filter",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Kinds: []int{1, 2, 3},
			},
			query: `SELECT id, pubkey, created_at, kind, tags, content, sig 
			FROM event 
			WHERE kind IN(1,2,3) 
			ORDER BY created_at DESC LIMIT $1`,
			params: []any{100},
			err:    nil,
		},
		{
			name:    "authors filter",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Authors: []string{"7bdef7bdebb8721f77927d0e77c66059360fa62371fdf15f3add93923a613229"},
			},
			query: `SELECT id, pubkey, created_at, kind, tags, content, sig 
			FROM event 
			WHERE (pubkey LIKE '7bdef7bdebb8721f77927d0e77c66059360fa62371fdf15f3add93923a613229%') 
			ORDER BY created_at DESC LIMIT $1`,
			params: []any{100},
			err:    nil,
		},
		// errors
		{
			name:    "nil filter",
			backend: defaultBackend,
			filter:  nil,
			query:   "",
			params:  nil,
			err:     fmt.Errorf("filter cannot be null"),
		},
		{
			name:    "too many ids",
			backend: defaultBackend,
			filter: &nostr.Filter{
				IDs: strSlice(501),
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "invalid ids",
			backend: defaultBackend,
			filter: &nostr.Filter{
				IDs: []string{"stuff"},
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "too many authors",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Authors: strSlice(501),
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "invalid authors",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Authors: []string{"stuff"},
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "too many kinds",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Kinds: intSlice(11),
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "no kinds",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Kinds: []int{},
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "tags of empty array",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Tags: nostr.TagMap{
					"#e": []string{},
				},
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "too many tag values",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Tags: nostr.TagMap{
					"#e": strSlice(11),
				},
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, params, err := tt.backend.queryEventsSql(tt.filter, false)
			assert.Equal(t, tt.err, err)
			if err != nil {
				return
			}

			assert.Equal(t, clean(tt.query), clean(query))
			assert.Equal(t, tt.params, params)
		})
	}
}

func clean(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, "\t", ""), "\n", ""), " ", "")
}

func intSlice(n int) []int {
	slice := make([]int, 0, n)
	for i := 0; i < n; i++ {
		slice = append(slice, i)
	}
	return slice
}

func strSlice(n int) []string {
	slice := make([]string, 0, n)
	for i := 0; i < n; i++ {
		slice = append(slice, strconv.Itoa(i))
	}
	return slice
}

func TestCountEventsSql(t *testing.T) {
	var tests = []struct {
		name    string
		backend PostgresBackend
		filter  *nostr.Filter
		query   string
		params  []any
		err     error
	}{
		{
			name:    "empty filter",
			backend: defaultBackend,
			filter:  &nostr.Filter{},
			query:   "SELECT COUNT(*) FROM event WHERE true ORDER BY created_at DESC LIMIT $1",
			params:  []any{100},
			err:     nil,
		},
		{
			name:    "ids filter",
			backend: defaultBackend,
			filter: &nostr.Filter{
				IDs: []string{"083ec57f36a7b39ab98a57bedab4f85355b2ee89e4b205bed58d7c3ef9edd294"},
			},
			query: `SELECT COUNT(*)
			FROM event 
			WHERE (id LIKE '083ec57f36a7b39ab98a57bedab4f85355b2ee89e4b205bed58d7c3ef9edd294%') 
			ORDER BY created_at DESC LIMIT $1`,
			params: []any{100},
			err:    nil,
		},
		{
			name:    "kind filter",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Kinds: []int{1, 2, 3},
			},
			query: `SELECT COUNT(*)
			FROM event 
			WHERE kind IN(1,2,3) 
			ORDER BY created_at DESC LIMIT $1`,
			params: []any{100},
			err:    nil,
		},
		{
			name:    "authors filter",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Authors: []string{"7bdef7bdebb8721f77927d0e77c66059360fa62371fdf15f3add93923a613229"},
			},
			query: `SELECT COUNT(*)
			FROM event 
			WHERE (pubkey LIKE '7bdef7bdebb8721f77927d0e77c66059360fa62371fdf15f3add93923a613229%') 
			ORDER BY created_at DESC LIMIT $1`,
			params: []any{100},
			err:    nil,
		},
		// errors
		{
			name:    "nil filter",
			backend: defaultBackend,
			filter:  nil,
			query:   "",
			params:  nil,
			err:     fmt.Errorf("filter cannot be null"),
		},
		{
			name:    "too many ids",
			backend: defaultBackend,
			filter: &nostr.Filter{
				IDs: strSlice(501),
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "invalid ids",
			backend: defaultBackend,
			filter: &nostr.Filter{
				IDs: []string{"stuff"},
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "too many authors",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Authors: strSlice(501),
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "invalid authors",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Authors: []string{"stuff"},
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "too many kinds",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Kinds: intSlice(11),
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "no kinds",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Kinds: []int{},
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "tags of empty array",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Tags: nostr.TagMap{
					"#e": []string{},
				},
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
		{
			name:    "too many tag values",
			backend: defaultBackend,
			filter: &nostr.Filter{
				Tags: nostr.TagMap{
					"#e": strSlice(11),
				},
			},
			query:  "",
			params: nil,
			// REVIEW: should return error
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, params, err := tt.backend.queryEventsSql(tt.filter, true)
			assert.Equal(t, tt.err, err)
			if err != nil {
				return
			}

			assert.Equal(t, clean(tt.query), clean(query))
			assert.Equal(t, tt.params, params)
		})
	}
}
