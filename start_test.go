package relayer

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nbd-wtf/go-nostr"
)

func TestServerStartShutdown(t *testing.T) {
	var (
		serverHost  string
		inited      bool
		storeInited bool
		shutdown    bool
	)
	ready := make(chan struct{})
	rl := &testRelay{
		name: "test server start",
		init: func() error {
			inited = true
			return nil
		},
		onInitialized: func(s *Server) {
			serverHost = s.Addr()
			close(ready)
		},
		onShutdown: func(context.Context) { shutdown = true },
		storage: &testStorage{
			init: func() error { storeInited = true; return nil },
		},
	}
	srv := NewServer("127.0.0.1:0", rl)
	done := make(chan error)
	go func() { done <- srv.Start(); close(done) }()

	// verify everything's initialized
	select {
	case <-ready:
		// continue
	case <-time.After(time.Second):
		t.Fatal("srv.Start too long to initialize")
	}
	if !inited {
		t.Error("didn't call testRelay.init")
	}
	if !storeInited {
		t.Error("didn't call testStorage.init")
	}

	// check that http requests are served
	if _, err := http.Get("http://" + serverHost); err != nil {
		t.Errorf("GET %s: %v", serverHost, err)
	}

	// verify server shuts down
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("srv.Shutdown: %v", err)
	}
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

func TestServerShutdownWebsocket(t *testing.T) {
	// set up a new relay server
	srv := startTestRelay(t, &testRelay{storage: &testStorage{}})

	// connect a client to it
	ctx1, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := nostr.RelayConnect(ctx1, "ws://"+srv.Addr())
	if err != nil {
		t.Fatalf("nostr.RelayConnectContext: %v", err)
	}

	// now, shut down the server
	ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx2); err != nil {
		t.Errorf("srv.Shutdown: %v", err)
	}

	// wait for the client to receive a "connection close"
	select {
	case err := <-client.ConnectionError:
		if _, ok := err.(*websocket.CloseError); !ok {
			t.Errorf("client.ConnextionError: %v (%T); want websocket.CloseError", err, err)
		}
	case <-time.After(2 * time.Second):
		t.Error("client took too long to disconnect")
	}
}
