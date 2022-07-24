package relayer

import (
	"github.com/fiatjaf/go-nostr"
	"github.com/fiatjaf/go-nostr/nip11"
)

var Log = log

type Relay interface {
	Name() string
	Init() error
	AcceptEvent(*nostr.Event) bool
	Storage() Storage
}

type Injector interface {
	InjectEvents() chan nostr.Event
}

type Informationer interface {
	GetNIP11InformationDocument() nip11.RelayInformationDocument
}

type AdvancedQuerier interface {
	BeforeQuery(*nostr.Filter)
	AfterQuery(*nostr.Filter)
}

type AdvancedDeleter interface {
	BeforeDelete(id string, pubkey string)
	AfterDelete(id string, pubkey string)
}

type AdvancedSaver interface {
	BeforeSave(*nostr.Event)
	AfterSave(*nostr.Event)
}

type Storage interface {
	Init() error

	QueryEvents(filter *nostr.Filter) (events []nostr.Event, err error)
	DeleteEvent(id string, pubkey string) error
	SaveEvent(event *nostr.Event) error
}
