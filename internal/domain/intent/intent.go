//intenza:paradigm functional-core

// Package intent is the buyer's public call for proposals and its lifecycle.
//
// Time is an input, never a read: every operation receives "now" from the
// caller. EffectiveStatus answers reads correctly even when the closing timer
// is late, while the transitions that unlock prices (Close) are only applied
// with the clock of the database, so a skewed node can never open envelopes
// early.
package intent

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/domain/event"
	"github.com/flaviotinococoutinho/intenza/internal/domain/fsm"
	"github.com/flaviotinococoutinho/intenza/internal/domain/schema"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
	"github.com/flaviotinococoutinho/intenza/standards/correios/cep"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso8601"
)

// Status is the lifecycle phase (the ontology's intent phases).
type Status string

const (
	StatusOpen      Status = "open"
	StatusClosed    Status = "closed"
	StatusAwarded   Status = "awarded"
	StatusExpired   Status = "expired"
	StatusCancelled Status = "cancelled"
)

// Trigger moves an intent between phases.
type Trigger string

const (
	TriggerClose  Trigger = "close"
	TriggerAward  Trigger = "award"
	TriggerExpire Trigger = "expire"
	TriggerCancel Trigger = "cancel"
)

// Machine is the intent lifecycle.
var Machine = fsm.New("intent", StatusOpen,
	fsm.Transition[Status, Trigger]{From: StatusOpen, On: TriggerClose, To: StatusClosed},
	fsm.Transition[Status, Trigger]{From: StatusOpen, On: TriggerCancel, To: StatusCancelled},
	fsm.Transition[Status, Trigger]{From: StatusClosed, On: TriggerAward, To: StatusAwarded},
	fsm.Transition[Status, Trigger]{From: StatusClosed, On: TriggerExpire, To: StatusExpired},
	fsm.Transition[Status, Trigger]{From: StatusClosed, On: TriggerCancel, To: StatusCancelled},
)

// Event types.
const (
	EvPublished = "intenza.intent.published.v1"
	EvClosed    = "intenza.intent.closed.v1"
	EvAwarded   = "intenza.intent.awarded.v1"
	EvExpired   = "intenza.intent.expired.v1"
	EvCancelled = "intenza.intent.cancelled.v1"
)

var (
	ErrWindow     = errors.New("intent: bidding window outside policy")
	ErrTitle      = errors.New("intent: title must have 5 to 120 characters")
	ErrDelivery   = errors.New("intent: delivery postal code is not assigned to a state")
	ErrNotOpen    = errors.New("intent: not accepting proposals")
	ErrNotBuyer   = errors.New("intent: only the buyer may do this")
	ErrTooEarly   = errors.New("intent: deadline not reached")
	ErrTooLate    = errors.New("intent: selection window is over")
	ErrNoProposal = errors.New("intent: closed without proposals")
)

// Policy holds the market rules, expressed as ISO 8601 durations.
type Policy struct {
	MinWindow       iso8601.Duration
	MaxWindow       iso8601.Duration
	SelectionWindow iso8601.Duration
	ActiveWave      int
}

// DefaultPolicy is the production market: one hour to fourteen days of
// bidding, three days to choose, only the first niche open.
var DefaultPolicy = Policy{
	MinWindow:       iso8601.MustParse("PT1H"),
	MaxWindow:       iso8601.MustParse("P14D"),
	SelectionWindow: iso8601.MustParse("P3D"),
	ActiveWave:      1,
}

// Intent is the aggregate.
type Intent struct {
	ID              string             `json:"id"`
	BuyerID         string             `json:"buyer_id"`
	CategoryID      string             `json:"category_id"`
	SchemaVersion   int                `json:"schema_version"`
	Title           string             `json:"title"`
	Constraints     map[string]any     `json:"constraints"`
	Predicates      schema.Constraints `json:"-"`
	DeliveryCEP     string             `json:"delivery_cep"`
	DeliveryUF      string             `json:"delivery_uf"`
	OpenedAt        time.Time          `json:"opened_at"`
	ClosesAt        time.Time          `json:"closes_at"`
	SelectBy        time.Time          `json:"select_by"`
	Status          Status             `json:"status"`
	ProposalCount   int                `json:"proposal_count"`
	AwardedProposal string             `json:"awarded_proposal,omitempty"`
	Version         int64              `json:"version"`
}

// PublishCommand is what the buyer sends.
type PublishCommand struct {
	ID          string
	BuyerID     string
	CategoryID  string
	Title       string
	Constraints map[string]any
	DeliveryCEP string
	Window      iso8601.Duration
}

// Published is the payload of EvPublished. Constraints are public: sellers
// need them to decide whether to answer.
type Published struct {
	IntentID    string         `json:"intent_id"`
	BuyerID     string         `json:"buyer_id"`
	CategoryID  string         `json:"category_id"`
	MCC         string         `json:"mcc"`
	Constraints map[string]any `json:"constraints"`
	DeliveryUF  string         `json:"delivery_uf"`
	ClosesAt    time.Time      `json:"closes_at"`
}

// Publish validates the command against the catalog and the policy.
func Publish(cmd PublishCommand, tree *taxonomy.Tree, pol Policy, now time.Time) (Intent, []event.Event, error) {
	cat, err := tree.Publishable(cmd.CategoryID, pol.ActiveWave)
	if err != nil {
		return Intent{}, nil, err
	}
	title := strings.TrimSpace(cmd.Title)
	if n := len([]rune(title)); n < 5 || n > 120 {
		return Intent{}, nil, ErrTitle
	}
	s := cat.Schema()
	preds, err := s.ParseConstraints(cmd.Constraints)
	if err != nil {
		return Intent{}, nil, err
	}
	code, err := cep.Parse(cmd.DeliveryCEP)
	if err != nil {
		return Intent{}, nil, ErrDelivery
	}
	state, ok := code.UF()
	if !ok {
		return Intent{}, nil, ErrDelivery
	}
	closes := cmd.Window.AddTo(now)
	if cmd.Window.Negative || closes.Before(pol.MinWindow.AddTo(now)) || closes.After(pol.MaxWindow.AddTo(now)) {
		return Intent{}, nil, fmt.Errorf("%w: entre %s e %s", ErrWindow, pol.MinWindow, pol.MaxWindow)
	}
	in := Intent{
		ID: cmd.ID, BuyerID: cmd.BuyerID, CategoryID: cat.ID(), SchemaVersion: s.Version, Title: title,
		Constraints: cmd.Constraints, Predicates: preds, DeliveryCEP: code.String(), DeliveryUF: state.Abbrev(),
		OpenedAt: now, ClosesAt: closes, SelectBy: pol.SelectionWindow.AddTo(closes), Status: StatusOpen, Version: 1,
	}
	ev := event.New(EvPublished, in.ID, now, Published{
		IntentID: in.ID, BuyerID: in.BuyerID, CategoryID: in.CategoryID, MCC: cat.MCC().String(),
		Constraints: in.Constraints, DeliveryUF: in.DeliveryUF, ClosesAt: in.ClosesAt,
	})
	return in, []event.Event{ev}, nil
}

// Rehydrate rebuilds the parsed predicates of a stored intent.
func (i Intent) Rehydrate(tree *taxonomy.Tree) (Intent, error) {
	cat, err := tree.Lookup(i.CategoryID)
	if err != nil {
		return i, err
	}
	preds, err := cat.Schema().ParseConstraints(i.Constraints)
	if err != nil {
		return i, err
	}
	i.Predicates = preds
	return i, nil
}

// EffectiveStatus is what a reader at instant now should see: an open intent
// past its deadline reads as closed even before the closer runs. It never
// unlocks anything; it only keeps reads honest when timers are late.
func (i Intent) EffectiveStatus(now time.Time) Status {
	switch {
	case i.Status == StatusOpen && !now.Before(i.ClosesAt):
		if !now.Before(i.SelectBy) {
			return StatusExpired
		}
		return StatusClosed
	case i.Status == StatusClosed && !now.Before(i.SelectBy):
		return StatusExpired
	}
	return i.Status
}

// AcceptsProposals reports whether a proposal may be added at instant now.
func (i Intent) AcceptsProposals(now time.Time) error {
	if i.Status != StatusOpen || !now.Before(i.ClosesAt) {
		return ErrNotOpen
	}
	return nil
}

func (i Intent) apply(t Trigger) (Intent, error) {
	to, err := Machine.Next(i.Status, t)
	if err != nil {
		return i, err
	}
	i.Status = to
	i.Version++
	return i, nil
}

// Close ends the bidding window. now must come from the authoritative clock
// (the database), never from the node running the code.
func (i Intent) Close(now time.Time) (Intent, []event.Event, error) {
	if now.Before(i.ClosesAt) {
		return i, nil, ErrTooEarly
	}
	next, err := i.apply(TriggerClose)
	if err != nil {
		return i, nil, err
	}
	return next, []event.Event{event.New(EvClosed, i.ID, now, map[string]any{"intent_id": i.ID, "proposal_count": i.ProposalCount})}, nil
}

// Award records the buyer's choice inside the selection window.
func (i Intent) Award(buyerID, proposalID, sellerID string, now time.Time) (Intent, []event.Event, error) {
	if buyerID != i.BuyerID {
		return i, nil, ErrNotBuyer
	}
	if !now.Before(i.SelectBy) {
		return i, nil, ErrTooLate
	}
	next, err := i.apply(TriggerAward)
	if err != nil {
		return i, nil, err
	}
	next.AwardedProposal = proposalID
	return next, []event.Event{event.New(EvAwarded, i.ID, now, map[string]any{
		"intent_id": i.ID, "proposal_id": proposalID, "seller_id": sellerID,
	})}, nil
}

// Expire closes the selection window without a choice.
func (i Intent) Expire(now time.Time) (Intent, []event.Event, error) {
	if now.Before(i.SelectBy) {
		return i, nil, ErrTooEarly
	}
	next, err := i.apply(TriggerExpire)
	if err != nil {
		return i, nil, err
	}
	return next, []event.Event{event.New(EvExpired, i.ID, now, map[string]any{"intent_id": i.ID})}, nil
}

// Cancel withdraws the intent before a choice.
func (i Intent) Cancel(buyerID string, now time.Time) (Intent, []event.Event, error) {
	if buyerID != i.BuyerID {
		return i, nil, ErrNotBuyer
	}
	next, err := i.apply(TriggerCancel)
	if err != nil {
		return i, nil, err
	}
	return next, []event.Event{event.New(EvCancelled, i.ID, now, map[string]any{"intent_id": i.ID})}, nil
}

// CountProposal registers one more proposal while open.
func (i Intent) CountProposal(now time.Time) (Intent, error) {
	if err := i.AcceptsProposals(now); err != nil {
		return i, err
	}
	i.ProposalCount++
	i.Version++
	return i, nil
}
