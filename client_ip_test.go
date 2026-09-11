package relayer

import (
	"net/http"
	"testing"
)

func TestClientIPPrefersTheTrustedHeader(t *testing.T) {
	r := &http.Request{Header: http.Header{}}
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")

	if got := clientIP(r, "CF-Connecting-IP", nil); got != "203.0.113.7" {
		t.Errorf("got %q, want %q", got, "203.0.113.7")
	}
}

func TestClientIPTakesTheFirstForwardedEntry(t *testing.T) {
	r := &http.Request{Header: http.Header{}}
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 198.51.100.1, 192.0.2.1")

	if got := clientIP(r, "X-Forwarded-For", nil); got != "203.0.113.7" {
		t.Errorf("got %q, want %q", got, "203.0.113.7")
	}
}

// A header nothing overwrites is a value the client chose, so a relay that did
// not name one must not read it.
func TestClientIPIgnoresHeadersWhenNoProxyIsTrusted(t *testing.T) {
	r := &http.Request{Header: http.Header{}}
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	r.Header.Set("CF-Connecting-IP", "203.0.113.8")

	defer func() {
		if recover() == nil {
			t.Error("expected the connection to be consulted, but it was not")
		}
	}()
	clientIP(r, "", nil) // nil conn: reaching RemoteAddr panics, which is the assertion
}

func TestClientIPFallsBackWhenTheHeaderIsAbsentOrBlank(t *testing.T) {
	for _, value := range []string{"", "   ", " , "} {
		r := &http.Request{Header: http.Header{}}
		if value != "" {
			r.Header.Set("CF-Connecting-IP", value)
		}

		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("value %q: expected the connection to be consulted", value)
				}
			}()
			clientIP(r, "CF-Connecting-IP", nil)
		}()
	}
}
