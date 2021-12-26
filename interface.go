package relayer

import (
	"github.com/fiatjaf/go-nostr/event"
	"github.com/fiatjaf/go-nostr/filter"
)

var Log = log

type Relay interface {
	Name() string
	Init() error
	SaveEvent(*event.Event) error
	QueryEvents(*filter.EventFilter) ([]event.Event, error)
}

type Injector interface {
	InjectEvents() chan event.Event
}
