package relayer

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip11"
	"github.com/nbd-wtf/go-nostr/nip77"
	"github.com/nbd-wtf/go-nostr/nip77/negentropy"
	"github.com/nbd-wtf/go-nostr/nip77/negentropy/storage/vector"
)

// seedRelay stores n signed events in-memory and returns them sorted by CreatedAt.
func seedRelay(t *testing.T, store *slicestore.SliceStore, n int) []*nostr.Event {
	t.Helper()
	sk := nostr.GeneratePrivateKey()
	events := make([]*nostr.Event, 0, n)
	for i := 0; i < n; i++ {
		evt := &nostr.Event{
			Kind:      nostr.KindTextNote,
			CreatedAt: nostr.Timestamp(1700000000 + i),
			Content:   "negentropy test",
			Tags:      nostr.Tags{},
		}
		if err := evt.Sign(sk); err != nil {
			t.Fatalf("sign: %v", err)
		}
		if err := store.SaveEvent(context.Background(), evt); err != nil {
			t.Fatalf("save: %v", err)
		}
		events = append(events, evt)
	}
	return events
}

func readNegMessage(t *testing.T, conn *websocket.Conn) nostr.Envelope {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	env := nip77.ParseNegMessage(raw)
	if env == nil {
		t.Fatalf("not a NEG envelope: %s", raw)
	}
	return env
}

func TestNIP77_ReconcileEmptyClient(t *testing.T) {
	store := &slicestore.SliceStore{}
	srv := startTestRelay(t, &testRelay{storage: store})
	defer srv.Shutdown(context.TODO())

	serverEvents := seedRelay(t, store, 5)
	serverIDs := make(map[string]struct{}, len(serverEvents))
	for _, e := range serverEvents {
		serverIDs[e.ID] = struct{}{}
	}

	conn := dialTestWS(t, srv.Addr)

	// empty client-side storage — server should report all IDs as "theirs, not ours"
	vec := vector.New()
	vec.Seal()
	neg := negentropy.New(vec, 4096)
	initial := neg.Start()

	filter := nostr.Filter{Kinds: []int{nostr.KindTextNote}}
	open := nip77.OpenEnvelope{SubscriptionID: "neg1", Filter: filter, Message: initial}
	openBytes, err := open.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal open: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, openBytes); err != nil {
		t.Fatalf("write open: %v", err)
	}

	collected := make(map[string]struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for id := range neg.HaveNots {
			collected[id] = struct{}{}
		}
		// drain Haves too so goroutines don't block
		for range neg.Haves {
		}
	}()

	for {
		env := readNegMessage(t, conn)
		msgEnv, ok := env.(*nip77.MessageEnvelope)
		if !ok {
			t.Fatalf("unexpected envelope %s", env.Label())
		}
		if msgEnv.SubscriptionID != "neg1" {
			t.Fatalf("wrong sub id: %s", msgEnv.SubscriptionID)
		}
		reply, err := neg.Reconcile(msgEnv.Message)
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if reply == "" {
			break
		}
		msg := nip77.MessageEnvelope{SubscriptionID: "neg1", Message: reply}
		msgBytes, _ := msg.MarshalJSON()
		if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
			t.Fatalf("write msg: %v", err)
		}
	}

	<-done

	if len(collected) != len(serverIDs) {
		t.Fatalf("expected %d ids from server, got %d", len(serverIDs), len(collected))
	}
	for id := range serverIDs {
		if _, ok := collected[id]; !ok {
			t.Errorf("missing id %s", id)
		}
	}
}

func TestNIP77_SequentialReconcileInReceiveOrder(t *testing.T) {
	// Fire several NEG-MSG frames back-to-back on the same subscription. They
	// must all be answered, in order, on the session opened by the NEG-OPEN
	// that preceded them.
	store := &slicestore.SliceStore{}
	srv := startTestRelay(t, &testRelay{storage: store})
	defer srv.Shutdown(context.TODO())
	seedRelay(t, store, 3)

	conn := dialTestWS(t, srv.Addr)

	vec := vector.New()
	vec.Seal()
	neg := negentropy.New(vec, 4096)
	initial := neg.Start()

	open := nip77.OpenEnvelope{SubscriptionID: "race", Filter: nostr.Filter{Kinds: []int{nostr.KindTextNote}}, Message: initial}
	b, _ := open.MarshalJSON()
	if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
		t.Fatalf("write open: %v", err)
	}
	env := readNegMessage(t, conn)
	first, ok := env.(*nip77.MessageEnvelope)
	if !ok {
		t.Fatalf("unexpected envelope: %s", env.Label())
	}

	msg := nip77.MessageEnvelope{SubscriptionID: "race", Message: first.Message}
	mb, _ := msg.MarshalJSON()
	for i := 0; i < 8; i++ {
		if err := conn.WriteMessage(websocket.TextMessage, mb); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	for i := 0; i < 8; i++ {
		env := readNegMessage(t, conn)
		if _, ok := env.(*nip77.MessageEnvelope); !ok {
			t.Fatalf("frame %d: expected NEG-MSG, got %s: %s", i, env.Label(), env)
		}
	}
}

// TestNIP77_CloseIsNotReorderedBeforeOpen: a NEG-CLOSE sent immediately after
// its NEG-OPEN must be applied after it. Handling NEG-* frames on worker
// goroutines let the cheap close win the race, no-op against a session that
// did not exist yet, and leave the session that the open then created alive
// forever. The leak is only visible afterwards: a NEG-MSG on a closed
// subscription has to be rejected.
func TestNIP77_CloseIsNotReorderedBeforeOpen(t *testing.T) {
	store := &slicestore.SliceStore{}
	srv := startTestRelay(t, &testRelay{storage: store})
	defer srv.Shutdown(context.TODO())
	seedRelay(t, store, 20)

	for attempt := 0; attempt < 20; attempt++ {
		conn := dialTestWS(t, srv.Addr)

		vec := vector.New()
		vec.Seal()
		neg := negentropy.New(vec, 4096)

		open := nip77.OpenEnvelope{SubscriptionID: "ord", Filter: nostr.Filter{Kinds: []int{nostr.KindTextNote}}, Message: neg.Start()}
		ob, _ := open.MarshalJSON()
		if err := conn.WriteMessage(websocket.TextMessage, ob); err != nil {
			t.Fatalf("write open: %v", err)
		}
		cb, _ := (nip77.CloseEnvelope{SubscriptionID: "ord"}).MarshalJSON()
		if err := conn.WriteMessage(websocket.TextMessage, cb); err != nil {
			t.Fatalf("write close: %v", err)
		}

		env := readNegMessage(t, conn)
		reply, ok := env.(*nip77.MessageEnvelope)
		if !ok {
			t.Fatalf("attempt %d: expected the NEG-OPEN reply, got %s: %s", attempt, env.Label(), env)
		}

		// the subscription is closed, so this must be refused
		mb, _ := (nip77.MessageEnvelope{SubscriptionID: "ord", Message: reply.Message}).MarshalJSON()
		if err := conn.WriteMessage(websocket.TextMessage, mb); err != nil {
			t.Fatalf("write msg: %v", err)
		}
		env = readNegMessage(t, conn)
		if _, ok := env.(*nip77.ErrorEnvelope); !ok {
			t.Fatalf("attempt %d: NEG-CLOSE was applied before its NEG-OPEN, leaking the session: got %s: %s", attempt, env.Label(), env)
		}
		conn.Close()
	}
}

func TestNIP77_CloseUnknownSubscription(t *testing.T) {
	store := &slicestore.SliceStore{}
	srv := startTestRelay(t, &testRelay{storage: store})
	defer srv.Shutdown(context.TODO())

	conn := dialTestWS(t, srv.Addr)

	msg := nip77.MessageEnvelope{SubscriptionID: "ghost", Message: "61"}
	b, _ := msg.MarshalJSON()
	if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// NIP-77 spells the error label NEG-ERR; go-nostr's ErrorEnvelope
	// marshals NEG-ERROR, so assert the raw bytes rather than round-tripping
	// through the library that has the bug.
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %v", arr)
	}
	if arr[0] != "NEG-ERR" {
		t.Errorf("label must be NEG-ERR per NIP-77, got %q", arr[0])
	}
	if arr[1] != "ghost" {
		t.Errorf("unexpected sub id %q", arr[1])
	}
	// "a machine-readable single-word prefix, followed by a `:`"
	word, rest, found := strings.Cut(arr[2], ":")
	if !found || word == "" || strings.ContainsAny(word, " \t") || strings.TrimSpace(rest) == "" {
		t.Errorf("reason %q is not in NIP-01 \"word: message\" form", arr[2])
	}

	// it must still decode as an error for clients using go-nostr's parser
	if _, ok := nip77.ParseNegMessage(raw).(*nip77.ErrorEnvelope); !ok {
		t.Errorf("go-nostr does not parse %s as a NEG error", raw)
	}
}

func TestNIP77_AdvertisedInNIP11(t *testing.T) {
	store := &slicestore.SliceStore{}
	srv := startTestRelay(t, &testRelay{storage: store})
	defer srv.Shutdown(context.TODO())

	req, _ := http.NewRequest(http.MethodGet, "http://"+srv.Addr+"/", nil)
	req.Header.Set("Accept", "application/nostr+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer resp.Body.Close()
	var info nip11.RelayInformationDocument
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range info.SupportedNIPs {
		switch v := n.(type) {
		case float64:
			if v == 77 {
				found = true
			}
		case int:
			if v == 77 {
				found = true
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Errorf("NIP-77 not advertised, got %v", info.SupportedNIPs)
	}
}
