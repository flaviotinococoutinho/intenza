//intenza:paradigm functional-core

// Package event is the shape of domain events. The functional core decides
// what happened; the imperative shell stamps ids, writes them to the outbox
// in the same transaction as the state and publishes them as CloudEvents.
package event

import "time"

// Event is a fact the domain emits.
type Event struct {
	Type    string    // "intenza.intent.published.v1"
	Subject string    // aggregate id, also the partition key
	Time    time.Time // when it happened, by the clock the caller passed in
	Data    any       // JSON-serializable payload
}

// New builds an event.
func New(typ, subject string, at time.Time, data any) Event {
	return Event{Type: typ, Subject: subject, Time: at, Data: data}
}
