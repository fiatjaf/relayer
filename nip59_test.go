package relayer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip42"
)

// testAutherRelay is a relay that implements Auther and CustomWebSocketHandler.
// The CustomWebSocketHandler exposes the challenge to the client for testing.
type testAutherRelay struct {
	testRelay
	serviceURL string
}

func (r *testAutherRelay) ServiceURL() string { return r.serviceURL }

func (r *testAutherRelay) HandleUnknownType(ws *WebSocket, typ string, request []json.RawMessage) {
	if typ == "GETCHALLENGE" {
		ws.WriteJSON([]string{"CHALLENGE", ws.challenge})
	}
}

func startAutherRelay(t *testing.T) *Server {
	t.Helper()
	rl := &testAutherRelay{
		testRelay:  testRelay{storage: &slicestore.SliceStore{}},
		serviceURL: "ws://localhost",
	}
	srv, _ := NewServer(rl)
	started := make(chan bool)
	go srv.Start("127.0.0.1", 0, started)
	<-started
	rl.serviceURL = "ws://" + srv.Addr
	return srv
}

func dialTestWS(t *testing.T, addr string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+addr, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func readMsg(t *testing.T, conn *websocket.Conn) (string, []json.RawMessage) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(msg, &raw); err != nil {
		t.Fatalf("unmarshal: %v (raw: %s)", err, msg)
	}
	var typ string
	if len(raw) > 0 {
		json.Unmarshal(raw[0], &typ)
	}
	return typ, raw
}

// authenticate performs NIP-42 auth via the GETCHALLENGE custom message.
func authenticate(t *testing.T, conn *websocket.Conn, sk string, relayURL string) string {
	t.Helper()

	// get challenge from server
	conn.WriteJSON([]interface{}{"GETCHALLENGE", ""})
	typ, raw := readMsg(t, conn)
	if typ != "CHALLENGE" {
		t.Fatalf("expected CHALLENGE, got %s", typ)
	}
	var challenge string
	json.Unmarshal(raw[1], &challenge)
	return authenticateWithChallenge(t, conn, sk, relayURL, challenge)
}

func authenticateWithChallenge(t *testing.T, conn *websocket.Conn, sk string, relayURL, challenge string) string {
	t.Helper()
	pub, _ := nostr.GetPublicKey(sk)

	// create and sign auth event
	authEvt := nip42.CreateUnsignedAuthEvent(challenge, pub, relayURL)
	authEvt.Sign(sk)

	// send AUTH
	conn.WriteJSON([]interface{}{"AUTH", authEvt})
	typ, raw := readMsg(t, conn)
	if typ != "OK" {
		t.Fatalf("expected OK, got %s", typ)
	}
	var ok bool
	json.Unmarshal(raw[2], &ok)
	if !ok {
		var reason string
		if len(raw) > 3 {
			json.Unmarshal(raw[3], &reason)
		}
		t.Fatalf("AUTH failed: %s", reason)
	}

	return pub
}

func TestNIP59_GiftWrap_Unauthenticated(t *testing.T) {
	srv := startAutherRelay(t)
	defer srv.Shutdown(context.TODO())

	conn := dialTestWS(t, srv.Addr)

	// REQ for kind 1059 without authentication
	conn.WriteJSON([]interface{}{"REQ", "sub1", nostr.Filter{Kinds: []int{nostr.KindGiftWrap}}})

	typ, raw := readMsg(t, conn)
	if typ != "AUTH" {
		t.Fatalf("expected AUTH challenge, got %s", typ)
	}
	var challenge string
	json.Unmarshal(raw[1], &challenge)
	if challenge == "" {
		t.Fatal("expected non-empty AUTH challenge")
	}

	typ, raw = readMsg(t, conn)
	if typ != "CLOSED" {
		t.Fatalf("expected CLOSED, got %s", typ)
	}
	var reason string
	json.Unmarshal(raw[2], &reason)
	if reason != "auth-required: this relay requires NIP-42 authentication to serve gift-wrapped events" {
		t.Errorf("unexpected reason: %q", reason)
	}

	// Authenticate using the relay-provided challenge and retry a
	// recipient-scoped subscription, as a NIP-42 client would.
	sk := nostr.GeneratePrivateKey()
	pub := authenticateWithChallenge(t, conn, sk, "ws://"+srv.Addr, challenge)
	conn.WriteJSON([]interface{}{"REQ", "sub2", nostr.Filter{
		Kinds: []int{nostr.KindGiftWrap},
		Tags:  nostr.TagMap{"p": []string{pub}},
	}})

	typ, _ = readMsg(t, conn)
	if typ != "EOSE" {
		t.Fatalf("expected EOSE after authentication, got %s", typ)
	}
}

func TestNIP59_GiftWrap_AuthenticatedSelf(t *testing.T) {
	srv := startAutherRelay(t)
	defer srv.Shutdown(context.TODO())

	conn := dialTestWS(t, srv.Addr)
	sk := nostr.GeneratePrivateKey()
	pub := authenticate(t, conn, sk, "ws://"+srv.Addr)

	// REQ for gift wraps addressed to self
	conn.WriteJSON([]interface{}{"REQ", "sub1", nostr.Filter{
		Kinds: []int{nostr.KindGiftWrap},
		Tags:  nostr.TagMap{"p": []string{pub}},
	}})

	typ, _ := readMsg(t, conn)
	if typ != "EOSE" {
		t.Fatalf("expected EOSE (allowed), got %s", typ)
	}
}

func TestNIP59_GiftWrap_AuthenticatedOther(t *testing.T) {
	srv := startAutherRelay(t)
	defer srv.Shutdown(context.TODO())

	conn := dialTestWS(t, srv.Addr)
	sk := nostr.GeneratePrivateKey()
	authenticate(t, conn, sk, "ws://"+srv.Addr)

	otherPub, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())

	// REQ for gift wraps addressed to someone else
	conn.WriteJSON([]interface{}{"REQ", "sub1", nostr.Filter{
		Kinds: []int{nostr.KindGiftWrap},
		Tags:  nostr.TagMap{"p": []string{otherPub}},
	}})

	typ, raw := readMsg(t, conn)
	if typ != "CLOSED" {
		t.Fatalf("expected CLOSED, got %s", typ)
	}
	var reason string
	json.Unmarshal(raw[2], &reason)
	if reason != "restricted: authenticated user does not have authorization for requested filters." {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestNIP59_GiftWrap_AuthenticatedNoFilter(t *testing.T) {
	srv := startAutherRelay(t)
	defer srv.Shutdown(context.TODO())

	conn := dialTestWS(t, srv.Addr)
	sk := nostr.GeneratePrivateKey()
	authenticate(t, conn, sk, "ws://"+srv.Addr)

	// REQ for gift wraps with no p filter
	conn.WriteJSON([]interface{}{"REQ", "sub1", nostr.Filter{
		Kinds: []int{nostr.KindGiftWrap},
	}})

	typ, raw := readMsg(t, conn)
	if typ != "CLOSED" {
		t.Fatalf("expected CLOSED, got %s", typ)
	}
	var reason string
	json.Unmarshal(raw[2], &reason)
	if reason != "restricted: authenticated user does not have authorization for requested filters." {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestNIP59_GiftWrap_NonAutherRelay(t *testing.T) {
	// Without Auther, NIP-59 guard should not activate
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	conn := dialTestWS(t, srv.Addr)

	// REQ for kind 1059 on a non-Auther relay should just return EOSE
	conn.WriteJSON([]interface{}{"REQ", "sub1", nostr.Filter{Kinds: []int{nostr.KindGiftWrap}}})

	typ, _ := readMsg(t, conn)
	if typ != "EOSE" {
		t.Fatalf("expected EOSE (no auth required), got %s", typ)
	}
}
