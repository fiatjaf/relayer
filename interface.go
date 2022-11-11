package relayer

import (
	"encoding/json"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip11"
)

var Log = log

// relay
type Relay interface {
	Name() string
	Init() error
	OnInitialized()
	AcceptEvent(*nostr.Event) bool
	Storage() Storage
}

type Injector interface {
	InjectEvents() chan nostr.Event
}

type Informationer interface {
	GetNIP11InformationDocument() nip11.RelayInformationDocument
}

type CustomWebSocketHandler interface {
	HandleUnknownType(ws *WebSocket, typ string, request []json.RawMessage)
}

// storage
type Storage interface {
	Init() error

	QueryEvents(filter *nostr.Filter) (events []nostr.Event, err error)
	DeleteEvent(id string, pubkey string) error
	SaveEvent(event *nostr.Event) error
}

type AdvancedQuerier interface {
	BeforeQuery(*nostr.Filter)
	AfterQuery([]nostr.Event, *nostr.Filter)
}

type AdvancedDeleter interface {
	BeforeDelete(id string, pubkey string)
	AfterDelete(id string, pubkey string)
}

type AdvancedSaver interface {
	BeforeSave(*nostr.Event)
	AfterSave(*nostr.Event)
}
