package relayer

import (
	"context"
	"testing"
	"time"

	"github.com/fiatjaf/relayer/v2/storage/eventmap"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip04"
)

func TestNip42(t *testing.T) {
	r := &testRelay{}
	r.storage = &eventmap.MapBackend{}

	srv := startTestRelay(t, r)
	defer srv.Shutdown(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sk1 := nostr.GeneratePrivateKey()
	sk2 := nostr.GeneratePrivateKey()
	pk1, _ := nostr.GetPublicKey(sk1)
	pk2, _ := nostr.GetPublicKey(sk2)

	client1, err := nostr.RelayConnect(ctx, "ws://"+srv.Addr, nostr.WithAuthHandler(
		func(ctx context.Context, authEvent *nostr.Event) (ok bool) {
			authEvent.Sign(sk1)
			return true
		},
	))
	if err != nil {
		t.Fatalf("nostr.RelayConnectContext: %v", err)
	}
	client2, err := nostr.RelayConnect(ctx, "ws://"+srv.Addr, nostr.WithAuthHandler(
		func(ctx context.Context, authEvent *nostr.Event) (ok bool) {
			authEvent.Sign(sk2)
			return true
		},
	))
	if err != nil {
		t.Fatalf("nostr.RelayConnectContext: %v", err)
	}

	sub1, err := client1.Subscribe(ctx, []nostr.Filter{
		{
			Kinds:   []int{nostr.KindEncryptedDirectMessage},
			Authors: []string{pk1},
		},
		{
			Kinds: []int{nostr.KindEncryptedDirectMessage},
			Tags:  nostr.TagMap{"p": {pk1}},
		},
	})
	if err != nil {
		t.Fatalf("relay.Subscribe: %v", err)
	}

	sub2, err := client2.Subscribe(ctx, []nostr.Filter{
		{
			Kinds:   []int{nostr.KindEncryptedDirectMessage},
			Authors: []string{pk2},
		},
		{
			Kinds: []int{nostr.KindEncryptedDirectMessage},
			Tags:  nostr.TagMap{"p": {pk2}},
		},
	})
	if err != nil {
		t.Fatalf("relay.Subscribe: %v", err)
	}

	ss, err := nip04.ComputeSharedSecret(pk2, sk1)
	if err != nil {
		t.Fatalf("nip04.ComputeSharedSecret: %v", err)
	}

	content, _ := nip04.Encrypt("Testing nip42!", ss)

	e := nostr.Event{
		PubKey:    pk1,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindEncryptedDirectMessage,
		Tags:      nostr.Tags{nostr.Tag{"p", pk2}},
		Content:   content,
	}
	e.Sign(sk1)

	oe := nostr.Event{
		PubKey:    pk1,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindTextNote,
		Tags:      nil,
		Content:   "content",
	}
	oe.Sign(sk1)

	_, err = client1.Publish(ctx, e)
	if err != nil {
		t.Fatalf("relay.Publish: %v", err)
	}
	_, err = client1.Publish(ctx, oe)
	if err != nil {
		t.Fatalf("relay.Publish: %v", err)
	}

	select {
	case e2 := <-sub1.Events:
		if e.ID != e2.ID {
			t.Fatalf("wrong message: %v %v", e, e2)
		}
	case <-time.After(time.Second * 2):
		t.Fatalf("no reply from relay.")
	}

	select {
	case e2 := <-sub2.Events:
		if e.ID != e2.ID {
			t.Fatalf("wrong message: %v %v", e, e2)
		}
	case <-time.After(time.Second * 2):
		t.Fatalf("no reply from relay.")
	}

	client3, err := nostr.RelayConnect(ctx, "ws://"+srv.Addr)
	if err != nil {
		t.Fatalf("unauthed nostr.RelayConnect: %v", err)
	}
	sub3, err := client3.Subscribe(ctx, []nostr.Filter{{
		Limit: 1,
	}})
	if err != nil {
		t.Fatalf("client3.Subscribe: %v", err)
	}

	for {
		select {
		case e2 := <-sub3.Events:
			if e.ID == e2.ID {
				t.Fatalf("unauthed metadata leak: %v %v", e, e2)
			} else if oe.ID != e2.ID {
				t.Fatalf("unexpected event!\n")
			}
		case <-time.After(time.Second * 2):
			return
		}
	}
}
