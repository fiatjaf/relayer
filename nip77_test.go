package relayer

import (
	"context"
	"encoding/json"
	"net/http"
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

func TestNIP77_ConcurrentReconcile(t *testing.T) {
	// Fire several NEG-MSG frames back-to-back on the same subscription to
	// exercise the per-session lock under -race.
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

	// Writes go out sequentially (fasthttp/websocket forbids concurrent
	// writes from one client), but each message spawns its own goroutine on
	// the server side, so their Reconcile calls can overlap.
	msg := nip77.MessageEnvelope{SubscriptionID: "race", Message: first.Message}
	mb, _ := msg.MarshalJSON()
	for i := 0; i < 8; i++ {
		if err := conn.WriteMessage(websocket.TextMessage, mb); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	for i := 0; i < 8; i++ {
		readNegMessage(t, conn)
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
	env := readNegMessage(t, conn)
	errEnv, ok := env.(*nip77.ErrorEnvelope)
	if !ok {
		t.Fatalf("expected NEG-ERROR, got %s", env.Label())
	}
	if errEnv.SubscriptionID != "ghost" {
		t.Errorf("unexpected sub id %q", errEnv.SubscriptionID)
	}
	if errEnv.Reason == "" {
		t.Errorf("empty reason")
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
