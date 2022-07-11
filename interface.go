package relayer

import (
	"github.com/fiatjaf/go-nostr"
	"github.com/fiatjaf/go-nostr/nip11"
)

var Log = log

type Relay interface {
	Name() string
	Init() error
	SaveEvent(*nostr.Event) error
	QueryEvents(*nostr.Filter) ([]nostr.Event, error)
}

type Injector interface {
	InjectEvents() chan nostr.Event
}

type Informationer interface {
	GetNIP11InformationDocument() nip11.RelayInformationDocument
}
