package relayer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip11"
)

func dialWS(t *testing.T, addr string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+addr, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func sendJSON(t *testing.T, conn *websocket.Conn, v interface{}) {
	t.Helper()
	if err := conn.WriteJSON(v); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
}

func recvMessage(t *testing.T, conn *websocket.Conn) (typ string, raw []json.RawMessage) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	if err := json.Unmarshal(msg, &raw); err != nil {
		t.Fatalf("unmarshal: %v (raw: %s)", err, msg)
	}
	if len(raw) > 0 {
		json.Unmarshal(raw[0], &typ)
	}
	return
}

func recvOK(t *testing.T, conn *websocket.Conn) (eventID string, ok bool, reason string) {
	t.Helper()
	typ, raw := recvMessage(t, conn)
	if typ != "OK" {
		t.Fatalf("expected OK, got %s", typ)
	}
	json.Unmarshal(raw[1], &eventID)
	json.Unmarshal(raw[2], &ok)
	if len(raw) > 3 {
		json.Unmarshal(raw[3], &reason)
	}
	return
}

func signedEvent(sk string, kind int, content string, tags nostr.Tags) nostr.Event {
	evt := nostr.Event{
		Kind:      kind,
		Content:   content,
		CreatedAt: nostr.Now(),
		Tags:      tags,
	}
	evt.Sign(sk)
	return evt
}

// --- doEvent tests ---

func TestDoEvent_ValidEvent(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sk := nostr.GeneratePrivateKey()
	evt := signedEvent(sk, 1, "hello", nostr.Tags{})

	sendJSON(t, conn, []interface{}{"EVENT", evt})
	_, ok, _ := recvOK(t, conn)
	if !ok {
		t.Error("expected OK true")
	}
}

func TestDoEvent_InvalidID(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sk := nostr.GeneratePrivateKey()
	evt := signedEvent(sk, 1, "hello", nostr.Tags{})
	evt.ID = strings.Repeat("00", 32)

	sendJSON(t, conn, []interface{}{"EVENT", evt})
	_, ok, reason := recvOK(t, conn)
	if ok {
		t.Error("expected OK false")
	}
	if reason != "invalid: event id is computed incorrectly" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestDoEvent_InvalidSignature(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sk := nostr.GeneratePrivateKey()
	evt := signedEvent(sk, 1, "hello", nostr.Tags{})
	// corrupt signature, keep valid ID
	evt.Sig = strings.Repeat("00", 64)

	sendJSON(t, conn, []interface{}{"EVENT", evt})
	_, ok, _ := recvOK(t, conn)
	if ok {
		t.Error("expected OK false for invalid signature")
	}
}

func TestDoEvent_RejectedByRelay(t *testing.T) {
	rl := &testRelay{
		storage: &slicestore.SliceStore{},
		acceptEvent: func(e *nostr.Event) (bool, string) {
			return false, "blocked: not allowed"
		},
	}
	srv := startTestRelay(t, rl)
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sk := nostr.GeneratePrivateKey()
	evt := signedEvent(sk, 1, "hello", nostr.Tags{})

	sendJSON(t, conn, []interface{}{"EVENT", evt})
	_, ok, reason := recvOK(t, conn)
	if ok {
		t.Error("expected OK false")
	}
	if reason != "blocked: not allowed" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestDoEvent_NIP40Expiration(t *testing.T) {
	store := &slicestore.SliceStore{}
	srv := startTestRelay(t, &testRelay{storage: store})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sk := nostr.GeneratePrivateKey()
	now := nostr.Now()

	cases := []struct {
		name   string
		kind   int
		tags   nostr.Tags
		ok     bool
		reason string
	}{
		{"expired", 1, nostr.Tags{{"expiration", fmt.Sprint(now - 10)}}, false, "invalid: event has expired"},
		{"expires now", 1, nostr.Tags{{"expiration", fmt.Sprint(now)}}, false, "invalid: event has expired"},
		{"expired deletion", 5, nostr.Tags{{"expiration", fmt.Sprint(now - 10)}}, false, "invalid: event has expired"},
		{"expired ephemeral", 20001, nostr.Tags{{"expiration", fmt.Sprint(now - 10)}}, false, "invalid: event has expired"},
		{"expires later", 1, nostr.Tags{{"expiration", fmt.Sprint(now + 3600)}}, true, ""},
		{"malformed expiration", 1, nostr.Tags{{"expiration", "soon"}}, true, ""},
	}
	for _, c := range cases {
		evt := signedEvent(sk, c.kind, c.name, c.tags)
		sendJSON(t, conn, []interface{}{"EVENT", evt})
		_, ok, reason := recvOK(t, conn)
		if ok != c.ok || reason != c.reason {
			t.Errorf("%s: got (%v, %q), want (%v, %q)", c.name, ok, reason, c.ok, c.reason)
		}
	}

	// expired events must not have been stored
	ch, err := store.QueryEvents(context.Background(), nostr.Filter{Authors: []string{mustPublicKey(t, sk)}})
	if err != nil {
		t.Fatal(err)
	}
	stored := map[string]bool{}
	for evt := range ch {
		stored[evt.Content] = true
	}
	for _, name := range []string{"expired", "expires now", "expired deletion"} {
		if stored[name] {
			t.Errorf("%s event was stored", name)
		}
	}
	if !stored["expires later"] || !stored["malformed expiration"] {
		t.Errorf("accepted events were not stored: %v", stored)
	}
}

func mustPublicKey(t *testing.T, sk string) string {
	t.Helper()
	pk, err := nostr.GetPublicKey(sk)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

func TestDoEvent_NIP09Deletion(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sk := nostr.GeneratePrivateKey()

	// publish a note
	evt := signedEvent(sk, 1, "to delete", nostr.Tags{})
	sendJSON(t, conn, []interface{}{"EVENT", evt})
	recvOK(t, conn)

	// delete it
	delEvt := signedEvent(sk, 5, "", nostr.Tags{{"e", evt.ID}})
	sendJSON(t, conn, []interface{}{"EVENT", delEvt})
	_, ok, _ := recvOK(t, conn)
	if !ok {
		t.Error("expected OK true for deletion")
	}

	// verify it's gone
	sendJSON(t, conn, []interface{}{"REQ", "check", nostr.Filter{IDs: []string{evt.ID}}})
	typ, _ := recvMessage(t, conn)
	if typ != "EOSE" {
		t.Errorf("expected EOSE (event deleted), got %s", typ)
	}
}

func TestDoEvent_NIP09DeletionWrongAuthor(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sk1 := nostr.GeneratePrivateKey()
	sk2 := nostr.GeneratePrivateKey()

	// publish with sk1
	evt := signedEvent(sk1, 1, "mine", nostr.Tags{})
	sendJSON(t, conn, []interface{}{"EVENT", evt})
	recvOK(t, conn)

	// try to delete with sk2
	delEvt := signedEvent(sk2, 5, "", nostr.Tags{{"e", evt.ID}})
	sendJSON(t, conn, []interface{}{"EVENT", delEvt})
	_, ok, reason := recvOK(t, conn)
	if ok {
		t.Error("expected OK false")
	}
	if reason != "insufficient permissions" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

// --- doReq tests ---

func TestDoReq_Basic(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sk := nostr.GeneratePrivateKey()

	// publish
	evt := signedEvent(sk, 1, "hello", nostr.Tags{})
	sendJSON(t, conn, []interface{}{"EVENT", evt})
	recvOK(t, conn)

	// query
	sendJSON(t, conn, []interface{}{"REQ", "sub1", nostr.Filter{Kinds: []int{1}}})

	typ, raw := recvMessage(t, conn)
	if typ != "EVENT" {
		t.Fatalf("expected EVENT, got %s", typ)
	}
	// verify it's the right event
	var subID string
	json.Unmarshal(raw[1], &subID)
	if subID != "sub1" {
		t.Errorf("expected sub1, got %q", subID)
	}

	typ, _ = recvMessage(t, conn)
	if typ != "EOSE" {
		t.Fatalf("expected EOSE, got %s", typ)
	}
}

func TestDoReq_NIP40HidesExpiredEvents(t *testing.T) {
	store := &slicestore.SliceStore{}
	srv := startTestRelay(t, &testRelay{storage: store})
	defer srv.Shutdown(context.TODO())

	sk := nostr.GeneratePrivateKey()
	// An event stored before its expiration passed stays in the storage.
	expired := signedEvent(sk, 1, "expired", nostr.Tags{{"expiration", fmt.Sprint(nostr.Now() - 10)}})
	live := signedEvent(sk, 1, "live", nostr.Tags{{"expiration", fmt.Sprint(nostr.Now() + 3600)}})
	for _, evt := range []nostr.Event{expired, live} {
		if err := store.SaveEvent(context.Background(), &evt); err != nil {
			t.Fatal(err)
		}
	}

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"REQ", "sub1", nostr.Filter{Kinds: []int{1}}})
	var got []string
	for {
		typ, raw := recvMessage(t, conn)
		if typ == "EOSE" {
			break
		}
		if typ != "EVENT" {
			t.Fatalf("expected EVENT or EOSE, got %s", typ)
		}
		var evt nostr.Event
		json.Unmarshal(raw[2], &evt)
		got = append(got, evt.Content)
	}
	if len(got) != 1 || got[0] != "live" {
		t.Errorf("expected only the live event, got %v", got)
	}
}

func TestDoReq_EmptyID(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"REQ", "", nostr.Filter{Kinds: []int{1}}})

	typ, raw := recvMessage(t, conn)
	if typ != "NOTICE" {
		t.Fatalf("expected NOTICE, got %s", typ)
	}
	var msg string
	json.Unmarshal(raw[1], &msg)
	if msg != "REQ has no <id>" {
		t.Errorf("unexpected notice: %q", msg)
	}
}

func TestDoReq_NoResults(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"REQ", "sub1", nostr.Filter{Kinds: []int{99999}}})

	typ, _ := recvMessage(t, conn)
	if typ != "EOSE" {
		t.Fatalf("expected EOSE, got %s", typ)
	}
}

func TestDoReq_LimitZeroSkipsHistoryAndKeepsSubscription(t *testing.T) {
	queries := 0
	storage := &testStorage{
		queryEvents: func(context.Context, nostr.Filter) (chan *nostr.Event, error) {
			queries++
			ch := make(chan *nostr.Event)
			close(ch)
			return ch, nil
		},
	}
	srv := startTestRelay(t, &testRelay{storage: storage})
	defer srv.Shutdown(context.TODO())

	subscriber := dialWS(t, srv.Addr)
	sendJSON(t, subscriber, []interface{}{"REQ", "live", map[string]interface{}{
		"kinds": []int{1},
		"limit": 0,
	}})

	typ, _ := recvMessage(t, subscriber)
	if typ != "EOSE" {
		t.Fatalf("expected EOSE, got %s", typ)
	}
	if queries != 0 {
		t.Fatalf("storage queried %d times, want 0", queries)
	}

	publisher := dialWS(t, srv.Addr)
	evt := signedEvent(nostr.GeneratePrivateKey(), 1, "live", nostr.Tags{})
	sendJSON(t, publisher, []interface{}{"EVENT", evt})
	recvOK(t, publisher)

	typ, raw := recvMessage(t, subscriber)
	if typ != "EVENT" {
		t.Fatalf("expected live EVENT after EOSE, got %s", typ)
	}
	var subID string
	json.Unmarshal(raw[1], &subID)
	if subID != "live" {
		t.Fatalf("expected subscription live, got %q", subID)
	}
}

// --- doClose tests ---

func TestDoClose_EmptyID(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"CLOSE", ""})

	typ, raw := recvMessage(t, conn)
	if typ != "NOTICE" {
		t.Fatalf("expected NOTICE, got %s", typ)
	}
	var msg string
	json.Unmarshal(raw[1], &msg)
	if msg != "CLOSE has no <id>" {
		t.Errorf("unexpected notice: %q", msg)
	}
}

// --- doCount tests ---

func TestDoCount_NotSupported(t *testing.T) {
	// testStorage does not implement EventCounter
	srv := startTestRelay(t, &testRelay{storage: &testStorage{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"COUNT", "c1", nostr.Filter{Kinds: []int{1}}})

	typ, raw := recvMessage(t, conn)
	if typ != "NOTICE" {
		t.Fatalf("expected NOTICE, got %s", typ)
	}
	var msg string
	json.Unmarshal(raw[1], &msg)
	if msg != "restricted: this relay does not support NIP-45" {
		t.Errorf("unexpected notice: %q", msg)
	}
}

func TestDoCount_EmptyID(t *testing.T) {
	// Need a storage that implements EventCounter
	st := &testStorageWithCounter{
		testStorage: testStorage{},
		countEvents: func(_ context.Context, _ nostr.Filter) (int64, error) {
			return 0, nil
		},
	}
	srv := startTestRelay(t, &testRelay{storage: st})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"COUNT", "", nostr.Filter{Kinds: []int{1}}})

	typ, raw := recvMessage(t, conn)
	if typ != "NOTICE" {
		t.Fatalf("expected NOTICE, got %s", typ)
	}
	var msg string
	json.Unmarshal(raw[1], &msg)
	if msg != "COUNT has no <id>" {
		t.Errorf("unexpected notice: %q", msg)
	}
}

// --- handleMessage tests ---

func TestHandleMessage_UnknownType(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"UNKNOWN", "data"})

	typ, raw := recvMessage(t, conn)
	if typ != "NOTICE" {
		t.Fatalf("expected NOTICE, got %s", typ)
	}
	var msg string
	json.Unmarshal(raw[1], &msg)
	if msg != "unknown message type UNKNOWN" {
		t.Errorf("unexpected notice: %q", msg)
	}
}

func TestHandleMessage_TooFewParams(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"EVENT"})

	typ, raw := recvMessage(t, conn)
	if typ != "NOTICE" {
		t.Fatalf("expected NOTICE, got %s", typ)
	}
	var msg string
	json.Unmarshal(raw[1], &msg)
	if msg != "request has less than 2 parameters" {
		t.Errorf("unexpected notice: %q", msg)
	}
}

func TestHandleMessage_InvalidJSON(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	// send invalid JSON - server should silently ignore
	conn.WriteMessage(websocket.TextMessage, []byte("not json"))

	// verify connection still works
	sendJSON(t, conn, []interface{}{"CLOSE", ""})
	typ, _ := recvMessage(t, conn)
	if typ != "NOTICE" {
		t.Fatalf("expected NOTICE after invalid JSON, got %s", typ)
	}
}

// --- HandleNIP11 tests ---

func TestHandleNIP11(t *testing.T) {
	srv := startTestRelay(t, &testRelay{
		name:    "test-nip11",
		storage: &slicestore.SliceStore{},
	})
	defer srv.Shutdown(context.TODO())

	req, _ := http.NewRequest("GET", "http://"+srv.Addr, nil)
	req.Header.Set("Accept", "application/nostr+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("unexpected content-type: %q", ct)
	}

	var info nip11.RelayInformationDocument
	json.NewDecoder(resp.Body).Decode(&info)

	if info.Name != "test-nip11" {
		t.Errorf("expected name 'test-nip11', got %q", info.Name)
	}
	if info.Software != "https://github.com/fiatjaf/relayer" {
		t.Errorf("unexpected software: %q", info.Software)
	}
	hasNIP40 := false
	for _, nip := range info.SupportedNIPs {
		if fmt.Sprint(nip) == "40" {
			hasNIP40 = true
		}
	}
	if !hasNIP40 {
		t.Errorf("expected supported_nips to include 40, got %#v", info.SupportedNIPs)
	}
}

func TestHandleNIP11IncludesNIP45ForCounterStorage(t *testing.T) {
	srv := startTestRelay(t, &testRelay{
		name:    "test-nip45",
		storage: &testStorageWithCounter{},
	})
	defer srv.Shutdown(context.TODO())

	req, _ := http.NewRequest("GET", "http://"+srv.Addr, nil)
	req.Header.Set("Accept", "application/nostr+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var info nip11.RelayInformationDocument
	json.NewDecoder(resp.Body).Decode(&info)

	for _, nip := range info.SupportedNIPs {
		if fmt.Sprint(nip) == "45" {
			return
		}
	}
	t.Fatalf("expected supported_nips to include 45, got %#v", info.SupportedNIPs)
}

// --- GetAuthStatus tests ---

func TestGetAuthStatus(t *testing.T) {
	t.Run("no auth in context", func(t *testing.T) {
		pubkey, ok := GetAuthStatus(context.Background())
		if ok || pubkey != "" {
			t.Errorf("expected (\"\", false), got (%q, %v)", pubkey, ok)
		}
	})

	t.Run("with auth", func(t *testing.T) {
		ws := &WebSocket{authed: "abc123"}
		ctx := context.WithValue(context.Background(), AUTH_CONTEXT_KEY, ws)
		pubkey, ok := GetAuthStatus(ctx)
		if !ok || pubkey != "abc123" {
			t.Errorf("expected (\"abc123\", true), got (%q, %v)", pubkey, ok)
		}
	})

	t.Run("wrong type in context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), AUTH_CONTEXT_KEY, "not a websocket")
		pubkey, ok := GetAuthStatus(ctx)
		if ok || pubkey != "" {
			t.Errorf("expected (\"\", false), got (%q, %v)", pubkey, ok)
		}
	})
}

// --- test helpers ---

type testStorageWithCounter struct {
	testStorage
	countEvents func(context.Context, nostr.Filter) (int64, error)
}

type testStorageWithFiltersCounter struct {
	testStorageWithCounter
	countEventsFilters func(context.Context, nostr.Filters) (int64, error)
}

func (s *testStorageWithFiltersCounter) CountEventsFilters(ctx context.Context, f nostr.Filters) (int64, error) {
	if s.countEventsFilters != nil {
		return s.countEventsFilters(ctx, f)
	}
	return 0, nil
}

func (s *testStorageWithCounter) CountEvents(ctx context.Context, f nostr.Filter) (int64, error) {
	if s.countEvents != nil {
		return s.countEvents(ctx, f)
	}
	return 0, nil
}

func TestDoCount_SumsFiltersWithoutAUnionCounter(t *testing.T) {
	// A store that can only count one filter at a time is still summed: that
	// is exact as long as the filters do not overlap.
	calls := 0
	st := &testStorageWithCounter{
		testStorage: testStorage{},
		countEvents: func(_ context.Context, _ nostr.Filter) (int64, error) {
			calls++
			return 3, nil
		},
	}
	srv := startTestRelay(t, &testRelay{storage: st})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"COUNT", "c1",
		nostr.Filter{Kinds: []int{1}}, nostr.Filter{Kinds: []int{7}}})

	typ, raw := recvMessage(t, conn)
	if typ != "COUNT" {
		t.Fatalf("expected COUNT, got %s", typ)
	}
	var res struct {
		Count int64 `json:"count"`
	}
	json.Unmarshal(raw[2], &res)
	if res.Count != 6 {
		t.Errorf("count = %d, want 6", res.Count)
	}
	if calls != 2 {
		t.Errorf("CountEvents called %d times, want 2", calls)
	}
}

func TestDoCount_UnionCounterSeesEveryFilter(t *testing.T) {
	// NIP-45 OR's the filters together, so an event matching more than one of
	// them counts once. A store that can evaluate the union is handed all the
	// filters at once instead of being asked about them one by one.
	var got nostr.Filters
	perFilterCalls := 0
	st := &testStorageWithFiltersCounter{
		testStorageWithCounter: testStorageWithCounter{
			testStorage: testStorage{},
			countEvents: func(_ context.Context, _ nostr.Filter) (int64, error) {
				perFilterCalls++
				return 3, nil
			},
		},
		countEventsFilters: func(_ context.Context, f nostr.Filters) (int64, error) {
			got = f
			return 4, nil
		},
	}
	srv := startTestRelay(t, &testRelay{storage: st})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"COUNT", "c1",
		nostr.Filter{Kinds: []int{1}}, nostr.Filter{Kinds: []int{1, 7}}})

	typ, raw := recvMessage(t, conn)
	if typ != "COUNT" {
		t.Fatalf("expected COUNT, got %s", typ)
	}
	var res struct {
		Count int64 `json:"count"`
	}
	json.Unmarshal(raw[2], &res)
	if res.Count != 4 {
		t.Errorf("count = %d, want 4 (the union, not the sum)", res.Count)
	}
	if len(got) != 2 {
		t.Fatalf("union counter saw %d filters, want 2", len(got))
	}
	if perFilterCalls != 0 {
		t.Errorf("CountEvents called %d times, want 0", perFilterCalls)
	}
}

func TestDoCount_SingleFilterUsesCountEvents(t *testing.T) {
	// One filter cannot overlap with itself, so the union path is unnecessary.
	unionCalls := 0
	perFilterCalls := 0
	st := &testStorageWithFiltersCounter{
		testStorageWithCounter: testStorageWithCounter{
			testStorage: testStorage{},
			countEvents: func(_ context.Context, _ nostr.Filter) (int64, error) {
				perFilterCalls++
				return 5, nil
			},
		},
		countEventsFilters: func(_ context.Context, _ nostr.Filters) (int64, error) {
			unionCalls++
			return 99, nil
		},
	}
	srv := startTestRelay(t, &testRelay{storage: st})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"COUNT", "c1", nostr.Filter{Kinds: []int{1}}})

	typ, raw := recvMessage(t, conn)
	if typ != "COUNT" {
		t.Fatalf("expected COUNT, got %s", typ)
	}
	var res struct {
		Count int64 `json:"count"`
	}
	json.Unmarshal(raw[2], &res)
	if res.Count != 5 {
		t.Errorf("count = %d, want 5", res.Count)
	}
	if unionCalls != 0 || perFilterCalls != 1 {
		t.Errorf("union=%d perFilter=%d, want 0 and 1", unionCalls, perFilterCalls)
	}
}

// --- NIP-67 EOSE completeness hint tests ---

// recvEOSE reads an EOSE envelope and returns the NIP-67 completeness hints
// (nil when the relay sent a bare two-element EOSE).
func recvEOSE(t *testing.T, conn *websocket.Conn) (subID string, hints []string) {
	t.Helper()
	typ, raw := recvMessage(t, conn)
	if typ != "EOSE" {
		t.Fatalf("expected EOSE, got %s", typ)
	}
	json.Unmarshal(raw[1], &subID)
	if len(raw) > 2 {
		json.Unmarshal(raw[2], &hints)
	}
	return
}

// TestDoReq_EOSEFinishHint: when the relay has drained every matching event it
// must mark the EOSE with the NIP-67 "finish" hint.
func TestDoReq_EOSEFinishHint(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sk := nostr.GeneratePrivateKey()

	evt := signedEvent(sk, 1, "hello", nostr.Tags{})
	sendJSON(t, conn, []interface{}{"EVENT", evt})
	recvOK(t, conn)

	sendJSON(t, conn, []interface{}{"REQ", "sub1", nostr.Filter{Kinds: []int{1}}})
	typ, _ := recvMessage(t, conn)
	if typ != "EVENT" {
		t.Fatalf("expected EVENT, got %s", typ)
	}
	_, hints := recvEOSE(t, conn)
	if len(hints) != 1 || hints[0] != "finish" {
		t.Errorf("expected [finish] hint, got %v", hints)
	}
}

// TestDoReq_EOSEFinishHintNoResults: an empty result set is still definitively
// complete, so it carries the "finish" hint too.
func TestDoReq_EOSEFinishHintNoResults(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"REQ", "sub1", nostr.Filter{Kinds: []int{99999}}})

	_, hints := recvEOSE(t, conn)
	if len(hints) != 1 || hints[0] != "finish" {
		t.Errorf("expected [finish] hint, got %v", hints)
	}
}

// TestDoReq_EOSEMoreHint: a storage that hands back more events than the filter
// limit lets the relay prove there are leftovers, so the EOSE carries "more".
func TestDoReq_EOSEMoreHint(t *testing.T) {
	sk := nostr.GeneratePrivateKey()
	st := &testStorage{
		queryEvents: func(_ context.Context, _ nostr.Filter) (chan *nostr.Event, error) {
			// ignore the limit and emit 3 events
			ch := make(chan *nostr.Event, 3)
			for i := 0; i < 3; i++ {
				evt := signedEvent(sk, 1, "hello", nostr.Tags{})
				ch <- &evt
			}
			close(ch)
			return ch, nil
		},
	}
	srv := startTestRelay(t, &testRelay{storage: st})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"REQ", "sub1", nostr.Filter{Kinds: []int{1}, Limit: 1}})

	typ, _ := recvMessage(t, conn)
	if typ != "EVENT" {
		t.Fatalf("expected EVENT, got %s", typ)
	}
	_, hints := recvEOSE(t, conn)
	if len(hints) != 1 || hints[0] != "more" {
		t.Errorf("expected [more] hint, got %v", hints)
	}
}

// TestDoReq_EOSENoHintWhenAmbiguous: when the storage returns exactly `limit`
// events the relay cannot tell whether more exist (the storage may have capped
// the query), so per NIP-67 it must stay silent rather than guess.
func TestDoReq_EOSENoHintWhenAmbiguous(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sk := nostr.GeneratePrivateKey()

	// publish 3 events but ask for only 1; slicestore caps the query at the limit
	for i := 0; i < 3; i++ {
		evt := signedEvent(sk, 1, "hello", nostr.Tags{})
		sendJSON(t, conn, []interface{}{"EVENT", evt})
		recvOK(t, conn)
	}

	sendJSON(t, conn, []interface{}{"REQ", "sub1", nostr.Filter{Kinds: []int{1}, Limit: 1}})
	typ, _ := recvMessage(t, conn)
	if typ != "EVENT" {
		t.Fatalf("expected EVENT, got %s", typ)
	}
	_, hints := recvEOSE(t, conn)
	if hints != nil {
		t.Errorf("expected no hint (ambiguous), got %v", hints)
	}
}

// TestDoReq_EOSENoHintWhenLimitZero: a limit-zero filter is never queried, so
// the relay knows nothing about what it holds and must not claim "finish".
func TestDoReq_EOSENoHintWhenLimitZero(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"REQ", "live", map[string]interface{}{
		"kinds": []int{1},
		"limit": 0,
	}})

	_, hints := recvEOSE(t, conn)
	if hints != nil {
		t.Errorf("expected no hint for limit-zero filter, got %v", hints)
	}
}

// TestDoReq_EOSEMoreHintIgnoresSkippedEvents: events past the limit that
// skipEventFunc would drop are not events the client could ever fetch, so they
// must not be reported as "more".
func TestDoReq_EOSEMoreHintIgnoresSkippedEvents(t *testing.T) {
	sk := nostr.GeneratePrivateKey()
	st := &testStorage{
		queryEvents: func(_ context.Context, _ nostr.Filter) (chan *nostr.Event, error) {
			// ignore the limit: one deliverable event followed by two the
			// relay is configured to drop
			ch := make(chan *nostr.Event, 3)
			keep := signedEvent(sk, 1, "keep", nostr.Tags{})
			ch <- &keep
			for i := 0; i < 2; i++ {
				evt := signedEvent(sk, 1, "drop", nostr.Tags{})
				ch <- &evt
			}
			close(ch)
			return ch, nil
		},
	}
	srv := startTestRelayWithOptions(t, &testRelay{storage: st},
		WithSkipEventFunc(func(evt *nostr.Event) bool { return evt.Content == "drop" }))
	defer srv.Shutdown(context.TODO())

	conn := dialWS(t, srv.Addr)
	sendJSON(t, conn, []interface{}{"REQ", "sub1", nostr.Filter{Kinds: []int{1}, Limit: 1}})

	typ, _ := recvMessage(t, conn)
	if typ != "EVENT" {
		t.Fatalf("expected EVENT, got %s", typ)
	}
	_, hints := recvEOSE(t, conn)
	for _, h := range hints {
		if h == "more" {
			t.Errorf("skipped events must not be reported as more, got %v", hints)
		}
	}
}

// TestDoReq_EOSEAdvertisedInNIP11: relays implementing NIP-67 should list 67.
func TestDoReq_NIP67AdvertisedInNIP11(t *testing.T) {
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	req, _ := http.NewRequest(http.MethodGet, "http://"+srv.Addr+"/", nil)
	req.Header.Set("Accept", "application/nostr+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer resp.Body.Close()

	var info struct {
		SupportedNIPs []float64 `json:"supported_nips"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, n := range info.SupportedNIPs {
		if n == 67 {
			return
		}
	}
	t.Errorf("NIP-67 not advertised, got %v", info.SupportedNIPs)
}
