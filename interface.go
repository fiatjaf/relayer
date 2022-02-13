package relayer

import (
	"github.com/fiatjaf/go-nostr"
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
