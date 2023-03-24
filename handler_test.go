package relayer

import (
	"context"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

func TestHandler(t *testing.T) {
	rl := &testRelay{
		name:          "test server start",
		init:          func() error { return nil },
		onInitialized: func(s *Server) {},
		onShutdown:    func(context.Context) {},
		storage:       &testStorage{init: func() error { return nil }},
	}
	srv := NewServer("127.0.0.1:0", rl)
	done := make(chan error)
	go func() { done <- srv.Start(); close(done) }()

	time.Sleep(time.Second)

	client, err := nostr.RelayConnect(context.Background(), "ws://"+srv.Addr())
	if err != nil {
		t.Fatal(err)
	}

	var ev nostr.Event
	var sk string
	nsec := "nsec180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsgyumg0"
	if _, s, err := nip19.Decode(nsec); err != nil {
		t.Fatal(err)
	} else {
		sk = s.(string)
	}
	if pub, err := nostr.GetPublicKey(sk); err == nil {
		if _, err := nip19.EncodePublicKey(pub); err != nil {
			t.Fatal(err)
		}
		ev.PubKey = pub
	} else {
		t.Fatal(err)
	}

	ev.Content = "test"
	ev.CreatedAt = time.Now().Add(80 * time.Minute)
	ev.Kind = nostr.KindTextNote
	ev.Sign(sk)
	_, err = client.Publish(context.Background(), ev)
	if err == nil {
		t.Fatal("should be an error")
	}

	ev.Content = "test"
	ev.CreatedAt = time.Now()
	ev.Kind = nostr.KindTextNote
	ev.Sign(sk)
	_, err = client.Publish(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
}
