package relayer

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/nbd-wtf/go-nostr"
)

// testBus is an in-process stand-in for a pub/sub transport (Redis, NATS, ...)
// shared by several relay instances.
type testBus struct {
	mu   sync.Mutex
	subs []chan *nostr.Event
}

func (b *testBus) publish(evt *nostr.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		ch <- evt
	}
}

func (b *testBus) subscribe(ctx context.Context) <-chan *nostr.Event {
	ch := make(chan *nostr.Event, 16)
	b.mu.Lock()
	b.subs = append(b.subs, ch)
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, c := range b.subs {
			if c == ch {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				break
			}
		}
		close(ch)
	}()
	return ch
}

// testNotifierRelay is a relay that propagates events over a testBus.
type testNotifierRelay struct {
	testRelay
	bus      *testBus
	notified []*nostr.Event
}

func (r *testNotifierRelay) Notify(ctx context.Context, evt *nostr.Event) error {
	r.notified = append(r.notified, evt)
	r.bus.publish(evt)
	return nil
}

func (r *testNotifierRelay) Notifications(ctx context.Context) (<-chan *nostr.Event, error) {
	return r.bus.subscribe(ctx), nil
}

// testNotifierStorage is a store that implements Notifier itself, the way a
// backend with native change notifications would.
type testNotifierStorage struct {
	slicestore.SliceStore
	bus *testBus
}

func (s *testNotifierStorage) Notify(ctx context.Context, evt *nostr.Event) error {
	s.bus.publish(evt)
	return nil
}

func (s *testNotifierStorage) Notifications(ctx context.Context) (<-chan *nostr.Event, error) {
	return s.bus.subscribe(ctx), nil
}

func startServer(t *testing.T, relay Relay) *Server {
	t.Helper()
	srv, err := NewServer(relay)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan bool)
	go srv.Start("127.0.0.1", 0, started)
	<-started
	t.Cleanup(func() { srv.Shutdown(context.TODO()) })
	return srv
}

func subscribe(t *testing.T, conn *websocket.Conn, subID string, filter nostr.Filter) {
	t.Helper()
	sendJSON(t, conn, []interface{}{"REQ", subID, filter})
	if typ, _ := recvMessage(t, conn); typ != "EOSE" {
		t.Fatalf("expected EOSE for %s, got %s", subID, typ)
	}
}

func expectEvent(t *testing.T, conn *websocket.Conn, subID, eventID string) {
	t.Helper()
	typ, raw := recvMessage(t, conn)
	if typ != "EVENT" {
		t.Fatalf("expected EVENT, got %s", typ)
	}
	var gotSub string
	json.Unmarshal(raw[1], &gotSub)
	if gotSub != subID {
		t.Errorf("expected sub %q, got %q", subID, gotSub)
	}
	var evt nostr.Event
	json.Unmarshal(raw[2], &evt)
	if evt.ID != eventID {
		t.Errorf("expected event %s, got %s", eventID, evt.ID)
	}
}

func TestNotifier_PropagatesAcrossServers(t *testing.T) {
	bus := &testBus{}
	store := &slicestore.SliceStore{}

	a := &testNotifierRelay{testRelay: testRelay{name: "a", storage: store}, bus: bus}
	b := &testNotifierRelay{testRelay: testRelay{name: "b", storage: store}, bus: bus}
	srvA := startServer(t, a)
	srvB := startServer(t, b)

	connA := dialWS(t, srvA.Addr)
	connB := dialWS(t, srvB.Addr)
	subscribe(t, connA, "subA", nostr.Filter{Kinds: []int{1}})
	subscribe(t, connB, "subB", nostr.Filter{Kinds: []int{1}})

	// publish on a
	sk := nostr.GeneratePrivateKey()
	evt := signedEvent(sk, 1, "hello from a", nostr.Tags{})
	sendJSON(t, connA, []interface{}{"EVENT", evt})
	if id, ok, reason := recvOK(t, connA); !ok || id != evt.ID {
		t.Fatalf("expected OK for %s, got ok=%v id=%s reason=%q", evt.ID, ok, id, reason)
	}

	// both a's and b's subscribers get it, a's exactly once
	expectEvent(t, connA, "subA", evt.ID)
	expectEvent(t, connB, "subB", evt.ID)

	connA.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, msg, err := connA.ReadMessage(); err == nil {
		t.Fatalf("a received an extra message: %s", msg)
	}

	if len(a.notified) != 1 || a.notified[0].ID != evt.ID {
		t.Errorf("expected a.Notify to be called once with %s, got %v", evt.ID, a.notified)
	}
	if len(b.notified) != 0 {
		t.Errorf("expected b.Notify not to be called, got %v", b.notified)
	}
}

func TestNotifier_OnStorage(t *testing.T) {
	bus := &testBus{}
	store := &testNotifierStorage{bus: bus}

	srvA := startServer(t, &testRelay{name: "a", storage: store})
	srvB := startServer(t, &testRelay{name: "b", storage: store})

	connB := dialWS(t, srvB.Addr)
	subscribe(t, connB, "subB", nostr.Filter{Kinds: []int{1}})

	connA := dialWS(t, srvA.Addr)
	sk := nostr.GeneratePrivateKey()
	evt := signedEvent(sk, 1, "hello from a", nostr.Tags{})
	sendJSON(t, connA, []interface{}{"EVENT", evt})
	recvOK(t, connA)

	expectEvent(t, connB, "subB", evt.ID)
}

func TestNotifier_EphemeralEventsAreNotified(t *testing.T) {
	bus := &testBus{}
	store := &slicestore.SliceStore{}
	a := &testNotifierRelay{testRelay: testRelay{name: "a", storage: store}, bus: bus}
	srvA := startServer(t, a)

	conn := dialWS(t, srvA.Addr)
	subscribe(t, conn, "sub", nostr.Filter{Kinds: []int{20001}})

	sk := nostr.GeneratePrivateKey()
	evt := signedEvent(sk, 20001, "ephemeral", nostr.Tags{})
	sendJSON(t, conn, []interface{}{"EVENT", evt})
	recvOK(t, conn)

	expectEvent(t, conn, "sub", evt.ID)
	if len(a.notified) != 1 {
		t.Errorf("expected Notify once, got %d", len(a.notified))
	}
}

func TestNotifier_NotificationsErrorFailsNewServer(t *testing.T) {
	r := &testFailingNotifierRelay{testRelay: testRelay{name: "a", storage: &slicestore.SliceStore{}}}
	if _, err := NewServer(r); err == nil {
		t.Fatal("expected NewServer to fail")
	}
	serversMutex.RLock()
	defer serversMutex.RUnlock()
	for srv := range servers {
		if srv.relay == r {
			t.Error("failed server left registered")
		}
	}
}

type testFailingNotifierRelay struct{ testRelay }

func (r *testFailingNotifierRelay) Notify(context.Context, *nostr.Event) error { return nil }
func (r *testFailingNotifierRelay) Notifications(context.Context) (<-chan *nostr.Event, error) {
	return nil, context.Canceled
}
