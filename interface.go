package relayer

import (
	"context"
	"encoding/json"

	"github.com/fiatjaf/eventstore"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip11"
)

// Relay is the main interface for implementing a nostr relay.
type Relay interface {
	// Name is used as the "name" field in NIP-11 and as a prefix in default Server logging.
	// For other NIP-11 fields, see [Informationer].
	Name() string
	// Init is called at the very beginning by [Server.Start], allowing a relay
	// to initialize its internal resources.
	// Also see [eventstore.Store.Init].
	Init() error
	// AcceptEvent is called for every nostr event received by the server.
	// If the returned value is true, the event is passed on to [Storage.SaveEvent].
	// Otherwise, the server responds with a negative and "blocked" message as described
	// in NIP-20.
	AcceptEvent(context.Context, *nostr.Event) (bool, string)
	// Storage returns the relay storage implementation.
	Storage(context.Context) eventstore.Store
}

// ReqAccepter is the main interface for implementing a nostr relay.
type ReqAccepter interface {
	// AcceptReq is called for every nostr request filters received by the
	// server. If the returned value is true, the filtres is passed on to
	// [Storage.QueryEvent].
	AcceptReq(ctx context.Context, id string, filters nostr.Filters, authedPubkey string) bool
}

// Auther is the interface for implementing NIP-42.
// ServiceURL() returns the URL used to verify the "AUTH" event from clients.
type Auther interface {
	ServiceURL() string
}

type Injector interface {
	InjectEvents() chan nostr.Event
}

// Notifier propagates accepted events between multiple relay instances that
// share the same storage, so that clients subscribed on one instance receive
// events published on another.
//
// It is looked up first on the [Relay] and then on the [eventstore.Store]
// returned by [Relay.Storage]; the first one found is used. When a Notifier is
// present, [AddEvent] no longer delivers events to local subscribers directly:
// every accepted event is passed to Notify, and only events arriving from
// Notifications are delivered. An implementation must therefore also send back
// the events it was asked to Notify about, including to the instance that
// published them.
type Notifier interface {
	// Notify is called for every accepted event, after it has been saved.
	// Ephemeral events are passed as well, even though they are not saved.
	Notify(context.Context, *nostr.Event) error
	// Notifications returns a channel delivering events accepted by any
	// instance, including this one. The channel must stay open until ctx is
	// done, reconnecting to the underlying transport as needed.
	Notifications(context.Context) (<-chan *nostr.Event, error)
}

// resolveNotifier returns the Notifier to use for relay, if any.
func resolveNotifier(relay Relay, store eventstore.Store) Notifier {
	if n, ok := relay.(Notifier); ok {
		return n
	}
	if n, ok := store.(Notifier); ok {
		return n
	}
	return nil
}

// Informationer is called to compose NIP-11 response to an HTTP request
// with application/nostr+json mime type.
// See also [Relay.Name].
type Informationer interface {
	GetNIP11InformationDocument() nip11.RelayInformationDocument
}

// CustomWebSocketHandler, if implemented, is passed nostr message types unrecognized
// by the server.
// The server handles "EVENT", "REQ" and "CLOSE" messages, as described in NIP-01.
type CustomWebSocketHandler interface {
	HandleUnknownType(ws *WebSocket, typ string, request []json.RawMessage)
}

// ShutdownAware is called during the server shutdown.
// See [Server.Shutdown] for details.
type ShutdownAware interface {
	OnShutdown(context.Context)
}

// Logger is what [Server] uses to log messages.
type Logger interface {
	Infof(format string, v ...any)
	Warningf(format string, v ...any)
	Errorf(format string, v ...any)
}

// AdvancedDeleter methods are called before and after [Storage.DeleteEvent].
type AdvancedDeleter interface {
	BeforeDelete(ctx context.Context, id string, pubkey string)
	AfterDelete(id string, pubkey string)
}

// AdvancedSaver methods are called before and after [Storage.SaveEvent].
type AdvancedSaver interface {
	BeforeSave(context.Context, *nostr.Event)
	AfterSave(*nostr.Event)
}

type EventCounter interface {
	CountEvents(ctx context.Context, filter nostr.Filter) (int64, error)
}

// FiltersCounter counts the events matching any of several filters for NIP-45,
// which asks for the filters to be OR'd together into a single count. Summing
// CountEvents over each filter separately counts an event that matches more
// than one of them once per filter, so a store that can evaluate the union
// itself — a SQL backend counting over a UNION, say — should implement this.
// Stores that do not are still summed, which is exact as long as the filters
// do not overlap.
type FiltersCounter interface {
	CountEventsFilters(ctx context.Context, filters nostr.Filters) (int64, error)
}
