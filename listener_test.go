package relayer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/nbd-wtf/go-nostr"
)

func newListenerTestServer() *Server {
	return &Server{listeners: make(map[*WebSocket]map[string]*Listener)}
}

func listenerCount(s *Server, ws *WebSocket) int {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	return len(s.listeners[ws])
}

func totalConnections(s *Server) int {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	return len(s.listeners)
}

func hasListener(s *Server, ws *WebSocket, id string) bool {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	if subs, ok := s.listeners[ws]; ok {
		_, ok = subs[id]
		return ok
	}
	return false
}

func TestSetListener(t *testing.T) {
	srv := newListenerTestServer()
	ws := &WebSocket{}
	srv.setListener("sub1", ws, nostr.Filters{{Kinds: []int{1}}})

	if !hasListener(srv, ws, "sub1") {
		t.Error("sub1 not found")
	}
	if listenerCount(srv, ws) != 1 {
		t.Errorf("expected 1 sub, got %d", listenerCount(srv, ws))
	}
}

func TestSetListenerMultipleSubs(t *testing.T) {
	srv := newListenerTestServer()
	ws := &WebSocket{}
	srv.setListener("sub1", ws, nostr.Filters{{Kinds: []int{1}}})
	srv.setListener("sub2", ws, nostr.Filters{{Kinds: []int{2}}})

	if listenerCount(srv, ws) != 2 {
		t.Errorf("expected 2 subs, got %d", listenerCount(srv, ws))
	}
}

func TestSetListenerOverwrite(t *testing.T) {
	srv := newListenerTestServer()
	ws := &WebSocket{}
	srv.setListener("sub1", ws, nostr.Filters{{Kinds: []int{1}}})
	srv.setListener("sub1", ws, nostr.Filters{{Kinds: []int{2}}})

	if listenerCount(srv, ws) != 1 {
		t.Errorf("expected 1 sub after overwrite, got %d", listenerCount(srv, ws))
	}
}

func TestSetListenerMultipleWS(t *testing.T) {
	srv := newListenerTestServer()
	ws1 := &WebSocket{}
	ws2 := &WebSocket{}
	srv.setListener("sub1", ws1, nostr.Filters{{Kinds: []int{1}}})
	srv.setListener("sub1", ws2, nostr.Filters{{Kinds: []int{1}}})

	if totalConnections(srv) != 2 {
		t.Errorf("expected 2 connections, got %d", totalConnections(srv))
	}
}

func TestRemoveListenerId(t *testing.T) {
	srv := newListenerTestServer()
	ws := &WebSocket{}
	srv.setListener("sub1", ws, nostr.Filters{{Kinds: []int{1}}})
	srv.setListener("sub2", ws, nostr.Filters{{Kinds: []int{2}}})

	srv.removeListenerId(ws, "sub1")

	if hasListener(srv, ws, "sub1") {
		t.Error("sub1 should be removed")
	}
	if !hasListener(srv, ws, "sub2") {
		t.Error("sub2 should still exist")
	}
}

func TestRemoveListenerIdRemovesWSWhenEmpty(t *testing.T) {
	srv := newListenerTestServer()
	ws := &WebSocket{}
	srv.setListener("sub1", ws, nostr.Filters{{Kinds: []int{1}}})

	srv.removeListenerId(ws, "sub1")

	if totalConnections(srv) != 0 {
		t.Error("ws entry should be removed when all subs are gone")
	}
}

func TestRemoveListenerIdNonexistent(t *testing.T) {
	srv := newListenerTestServer()
	ws := &WebSocket{}
	srv.removeListenerId(ws, "nope")
}

func TestRemoveListener(t *testing.T) {
	srv := newListenerTestServer()
	ws := &WebSocket{}
	srv.setListener("sub1", ws, nostr.Filters{{Kinds: []int{1}}})
	srv.setListener("sub2", ws, nostr.Filters{{Kinds: []int{2}}})

	srv.removeListener(ws)

	if totalConnections(srv) != 0 {
		t.Error("ws should be removed")
	}
}

func TestRemoveListenerNonexistent(t *testing.T) {
	srv := newListenerTestServer()
	ws := &WebSocket{}
	srv.removeListener(ws)
}

func TestGetListeningFilters(t *testing.T) {
	srv := newListenerTestServer()
	ws1 := &WebSocket{}
	ws2 := &WebSocket{}

	f1 := nostr.Filter{Kinds: []int{1}}
	f2 := nostr.Filter{Kinds: []int{2}}

	srv.setListener("sub1", ws1, nostr.Filters{f1, f2})
	srv.setListener("sub2", ws2, nostr.Filters{f1})

	filters := srv.GetListeningFilters()
	if len(filters) != 2 {
		t.Errorf("expected 2 distinct filters, got %d", len(filters))
	}
}

func TestGetListeningFiltersEmpty(t *testing.T) {
	srv := newListenerTestServer()
	filters := srv.GetListeningFilters()
	if len(filters) != 0 {
		t.Errorf("expected 0 filters, got %d", len(filters))
	}
}

func TestServersDoNotCrossDeliverSubscriptions(t *testing.T) {
	srv1 := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv1.Shutdown(context.TODO())
	srv2 := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv2.Shutdown(context.TODO())

	pubConn := dialWS(t, srv1.Addr)
	conn1 := dialWS(t, srv1.Addr)
	conn2 := dialWS(t, srv2.Addr)

	sendJSON(t, conn1, []interface{}{"REQ", "sub1", nostr.Filter{Kinds: []int{1}}})
	if typ, _ := recvMessage(t, conn1); typ != "EOSE" {
		t.Fatalf("expected EOSE, got %s", typ)
	}

	sendJSON(t, conn2, []interface{}{"REQ", "sub2", nostr.Filter{Kinds: []int{1}}})
	if typ, _ := recvMessage(t, conn2); typ != "EOSE" {
		t.Fatalf("expected EOSE, got %s", typ)
	}

	sk := nostr.GeneratePrivateKey()
	evt := signedEvent(sk, 1, "server-1-only", nil)
	sendJSON(t, pubConn, []interface{}{"EVENT", evt})
	if _, ok, _ := recvOK(t, pubConn); !ok {
		t.Fatal("expected publish to be accepted")
	}

	typ, raw := recvMessage(t, conn1)
	if typ != "EVENT" {
		t.Fatalf("expected EVENT on server 1, got %s", typ)
	}
	var subID string
	if err := json.Unmarshal(raw[1], &subID); err != nil {
		t.Fatalf("unmarshal sub id: %v", err)
	}
	if subID != "sub1" {
		t.Fatalf("expected sub1, got %s", subID)
	}

	conn2.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := conn2.ReadMessage(); err == nil {
		t.Fatal("unexpected cross-server event delivery")
	} else if !websocket.IsCloseError(err) && !isTimeout(err) {
		t.Fatalf("unexpected read error: %v", err)
	}
}

func isTimeout(err error) bool {
	type timeout interface {
		Timeout() bool
	}
	if te, ok := err.(timeout); ok {
		return te.Timeout()
	}
	return false
}
