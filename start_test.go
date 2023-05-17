package relayer

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gobwas/ws/wsutil"
	"github.com/nbd-wtf/go-nostr"
)

func TestServerStartShutdown(t *testing.T) {
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
	if _, err := http.Get("http://" + srv.Addr); err != nil {
		t.Errorf("GET %s: %v", srv.Addr, err)
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

func TestServerShutdownWebsocket(t *testing.T) {
	// set up a new relay server
	srv := startTestRelay(t, &testRelay{storage: &testStorage{}})

	// connect a client to it
	ctx1, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := nostr.RelayConnect(ctx1, "ws://"+srv.Addr)
	if err != nil {
		t.Fatalf("nostr.RelayConnectContext: %v", err)
	}

	// now, shut down the server
	ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv.Shutdown(ctx2)

	// wait for the client to receive a "connection close"
	time.Sleep(1 * time.Second)
	err = client.ConnectionError
	if e := errors.Unwrap(err); e != nil {
		err = e
	}
	if _, ok := err.(wsutil.ClosedError); !ok {
		t.Errorf("client.ConnextionError: %v (%T); want wsutil.ClosedError", err, err)
	}
}
