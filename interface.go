package relayer

import (
	"context"
	"encoding/json"

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
	// Also see [Storage.Init].
	Init() error
	// OnInitialized is called by [Server.Start] right before starting to serve HTTP requests.
	// It is passed the server to allow callers make final adjustments, such as custom routing.
	OnInitialized(*Server)
	// AcceptEvent is called for every nostr event received by the server.
	// If the returned value is true, the event is passed on to [Storage.SaveEvent].
	// Otherwise, the server responds with a negative and "blocked" message as described
	// in NIP-20.
	AcceptEvent(*nostr.Event) bool
	// Storage returns the relay storage implementation.
	Storage() Storage
}

// Auther is the interface for implementing NIP-42.
// ServiceURL() returns the URL used to verify the "AUTH" event from clients.
type Auther interface {
	ServiceURL() string
}

type Injector interface {
	InjectEvents() chan nostr.Event
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

// Storage is a persistence layer for nostr events handled by a relay.
type Storage interface {
	// Init is called at the very beginning by [Server.Start], after [Relay.Init],
	// allowing a storage to initialize its internal resources.
	Init() error

	// QueryEvents is invoked upon a client's REQ as described in NIP-01.
	QueryEvents(filter *nostr.Filter) (events []nostr.Event, err error)
	// DeleteEvent is used to handle deletion events, as per NIP-09.
	DeleteEvent(id string, pubkey string) error
	// SaveEvent is called once Relay.AcceptEvent reports true.
	SaveEvent(event *nostr.Event) error
}

// AdvancedQuerier methods are called before and after [Storage.QueryEvents].
type AdvancedQuerier interface {
	BeforeQuery(*nostr.Filter)
	AfterQuery([]nostr.Event, *nostr.Filter)
}

// AdvancedDeleter methods are called before and after [Storage.DeleteEvent].
type AdvancedDeleter interface {
	BeforeDelete(id string, pubkey string)
	AfterDelete(id string, pubkey string)
}

// AdvancedSaver methods are called before and after [Storage.SaveEvent].
type AdvancedSaver interface {
	BeforeSave(*nostr.Event)
	AfterSave(*nostr.Event)
}
