package relayer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func TestAddEvent(t *testing.T) {
	t.Run("nil event", func(t *testing.T) {
		rl := &testRelay{storage: &testStorage{}}
		accepted, msg := AddEvent(context.Background(), rl, nil)
		if accepted || msg != "" {
			t.Errorf("got (%v, %q), want (false, \"\")", accepted, msg)
		}
	})

	t.Run("rejected by relay", func(t *testing.T) {
		rl := &testRelay{
			storage: &testStorage{},
			acceptEvent: func(e *nostr.Event) (bool, string) {
				return false, "blocked: custom"
			},
		}
		accepted, msg := AddEvent(context.Background(), rl, &nostr.Event{Kind: 1})
		if accepted {
			t.Error("expected rejection")
		}
		if msg != "blocked: custom" {
			t.Errorf("got %q", msg)
		}
	})

	t.Run("rejected default message", func(t *testing.T) {
		rl := &testRelay{
			storage: &testStorage{},
			acceptEvent: func(e *nostr.Event) (bool, string) {
				return false, ""
			},
		}
		accepted, msg := AddEvent(context.Background(), rl, &nostr.Event{Kind: 1})
		if accepted {
			t.Error("expected rejection")
		}
		if msg != "blocked: event blocked by relay" {
			t.Errorf("got %q", msg)
		}
	})

	t.Run("ephemeral event not saved", func(t *testing.T) {
		clearListeners()
		defer clearListeners()
		saveCalled := false
		rl := &testRelay{
			storage: &testStorage{
				saveEvent: func(_ context.Context, _ *nostr.Event) error {
					saveCalled = true
					return nil
				},
			},
		}
		accepted, _ := AddEvent(context.Background(), rl, &nostr.Event{Kind: 25000})
		if !accepted {
			t.Error("expected acceptance")
		}
		if saveCalled {
			t.Error("SaveEvent should not be called for ephemeral events")
		}
	})

	t.Run("ephemeral boundary low", func(t *testing.T) {
		clearListeners()
		defer clearListeners()
		saveCalled := false
		rl := &testRelay{
			storage: &testStorage{
				saveEvent: func(_ context.Context, _ *nostr.Event) error {
					saveCalled = true
					return nil
				},
			},
		}
		AddEvent(context.Background(), rl, &nostr.Event{Kind: 20000})
		if saveCalled {
			t.Error("kind 20000 should be ephemeral")
		}
	})

	t.Run("ephemeral boundary high", func(t *testing.T) {
		clearListeners()
		defer clearListeners()
		saveCalled := false
		rl := &testRelay{
			storage: &testStorage{
				saveEvent: func(_ context.Context, _ *nostr.Event) error {
					saveCalled = true
					return nil
				},
			},
		}
		AddEvent(context.Background(), rl, &nostr.Event{Kind: 29999})
		if saveCalled {
			t.Error("kind 29999 should be ephemeral")
		}
	})

	t.Run("non-ephemeral kind 30000", func(t *testing.T) {
		clearListeners()
		defer clearListeners()
		called := false
		rl := &testRelay{
			storage: &testStorage{
				saveEvent: func(_ context.Context, _ *nostr.Event) error {
					called = true
					return nil
				},
				replaceEvent: func(_ context.Context, _ *nostr.Event) error {
					called = true
					return nil
				},
			},
		}
		AddEvent(context.Background(), rl, &nostr.Event{Kind: 30000, Tags: nostr.Tags{{"d", ""}}})
		if !called {
			t.Error("kind 30000 should be saved")
		}
	})

	t.Run("save success", func(t *testing.T) {
		clearListeners()
		defer clearListeners()
		saved := false
		rl := &testRelay{
			storage: &testStorage{
				saveEvent: func(_ context.Context, _ *nostr.Event) error {
					saved = true
					return nil
				},
			},
		}
		accepted, msg := AddEvent(context.Background(), rl, &nostr.Event{Kind: 1})
		if !accepted || msg != "" {
			t.Errorf("got (%v, %q), want (true, \"\")", accepted, msg)
		}
		if !saved {
			t.Error("SaveEvent not called")
		}
	})

	t.Run("duplicate event via wrapper", func(t *testing.T) {
		clearListeners()
		defer clearListeners()
		// eventstore.RelayWrapper.Publish handles dups silently (returns nil)
		rl := &testRelay{
			storage: &testStorage{},
		}
		accepted, msg := AddEvent(context.Background(), rl, &nostr.Event{Kind: 1})
		if !accepted {
			t.Error("expected acceptance")
		}
		if msg != "" {
			t.Errorf("got %q", msg)
		}
	})

	t.Run("save error", func(t *testing.T) {
		rl := &testRelay{
			storage: &testStorage{
				saveEvent: func(_ context.Context, _ *nostr.Event) error {
					return errors.New("db connection failed")
				},
			},
		}
		accepted, msg := AddEvent(context.Background(), rl, &nostr.Event{Kind: 1})
		if accepted {
			t.Error("expected rejection")
		}
		if !strings.Contains(msg, "db connection failed") {
			t.Errorf("expected error containing 'db connection failed', got %q", msg)
		}
	})

	t.Run("AdvancedSaver hooks called", func(t *testing.T) {
		clearListeners()
		defer clearListeners()
		var beforeCalled, afterCalled bool
		st := &testAdvancedStorage{
			testStorage: testStorage{
				saveEvent: func(_ context.Context, _ *nostr.Event) error { return nil },
			},
			beforeSave: func(_ context.Context, _ *nostr.Event) { beforeCalled = true },
			afterSave:  func(_ *nostr.Event) { afterCalled = true },
		}
		rl := &testRelay{storage: st}
		accepted, _ := AddEvent(context.Background(), rl, &nostr.Event{Kind: 1})
		if !accepted {
			t.Error("expected acceptance")
		}
		if !beforeCalled {
			t.Error("BeforeSave not called")
		}
		if !afterCalled {
			t.Error("AfterSave not called")
		}
	})

	t.Run("AdvancedSaver AfterSave not called on error", func(t *testing.T) {
		afterCalled := false
		st := &testAdvancedStorage{
			testStorage: testStorage{
				saveEvent: func(_ context.Context, _ *nostr.Event) error {
					return errors.New("save failed")
				},
			},
			afterSave: func(_ *nostr.Event) { afterCalled = true },
		}
		rl := &testRelay{storage: st}
		AddEvent(context.Background(), rl, &nostr.Event{Kind: 1})
		if afterCalled {
			t.Error("AfterSave should not be called on save error")
		}
	})
}
