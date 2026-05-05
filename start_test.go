package relayer

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/nbd-wtf/go-nostr"
	"go.uber.org/goleak"
)

func TestServerStartShutdown(t *testing.T) {
	defer goleak.VerifyNone(t)
	var (
		inited      bool
		storeInited bool
		shutdown    bool
	)
	rl := &testRelay{
		name: "test server start",
		init: func() error {
			inited = true
			return nil
		},
		onShutdown: func(context.Context) { shutdown = true },
		storage: &testStorage{
			init: func() error { storeInited = true; return nil },
		},
	}
	srv, _ := NewServer(rl)
	defer srv.Shutdown(context.TODO())
	ready := make(chan bool)
	done := make(chan error)
	go func() { done <- srv.Start("127.0.0.1", 0, ready); close(done) }()
	<-ready

	// verify everything's initialized
	if !inited {
		t.Error("didn't call testRelay.init")
	}
	if !storeInited {
		t.Error("didn't call testStorage.init")
	}

	// check that http requests are served
	if resp, err := http.Get("http://" + srv.Addr); err != nil {
		t.Errorf("GET %s: %v", srv.Addr, err)
	} else {
		resp.Body.Close()
	}

	// verify server shuts down
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	if !shutdown {
		t.Error("didn't call testRelay.onShutdown")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("srv.Start: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("srv.Start too long to return")
	}
}

func TestNewServerInitFailureDoesNotRegisterServer(t *testing.T) {
	relay := &testRelay{
		init: func() error { return fmt.Errorf("boom") },
		storage: &testStorage{
			init: func() error { return nil },
		},
	}

	serversMutex.RLock()
	before := len(servers)
	serversMutex.RUnlock()

	srv, err := NewServer(relay)
	if err == nil {
		t.Fatal("expected init error")
	}
	if srv != nil {
		t.Fatal("expected nil server on init failure")
	}
	serversMutex.RLock()
	after := len(servers)
	serversMutex.RUnlock()
	if after != before {
		t.Fatalf("expected servers registry size %d, got %d", before, after)
	}
}

func TestServerShutdownWebsocket(t *testing.T) {
	defer goleak.VerifyNone(t)
	// set up a new relay server
	srv := startTestRelay(t, &testRelay{storage: &slicestore.SliceStore{}})
	defer srv.Shutdown(context.TODO())

	// connect a client to it
	ctx1, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := nostr.RelayConnect(ctx1, "ws://"+srv.Addr)
	if err != nil {
		t.Fatalf("nostr.RelayConnectContext: %v", err)
	}

	sk := nostr.GeneratePrivateKey()

	var ev nostr.Event
	ev.Kind = nostr.KindTextNote
	ev.Content = "test"
	ev.CreatedAt = nostr.Now()
	ev.Sign(sk)
	client.Publish(ctx1, ev)

	var filter nostr.Filter
	filter.Kinds = []int{nostr.KindTextNote}
	evs, err := client.QuerySync(ctx1, filter)
	if err != nil {
		t.Fatalf("client.QuerySync: %v", err)
	}
	fmt.Println(evs)

	// now, shut down the server
	ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv.Shutdown(ctx2)

	// wait for the client to receive a "connection close"
	time.Sleep(1 * time.Second)
	err = client.ConnectionError
	if err == nil {
		t.Error("expected client.ConnectionError to be non-nil after shutdown")
	}
}
