//intenza:paradigm functional-core

// Package proposal is the seller's sealed answer to an intent: a conditional
// commitment with declared attributes, a total price nobody else can read
// before the window closes, and an explicit validity.
package proposal

import (
	"errors"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/domain/event"
	"github.com/flaviotinococoutinho/intenza/internal/domain/fsm"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/money"
	"github.com/flaviotinococoutinho/intenza/internal/domain/schema"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso4217"
)

// Status is the lifecycle phase.
type Status string

const (
	StatusActive    Status = "active"
	StatusWithdrawn Status = "withdrawn"
	StatusSelected  Status = "selected"
	StatusRejected  Status = "rejected"
	StatusLapsed    Status = "lapsed"
)

// Trigger moves a proposal between phases.
type Trigger string

const (
	TriggerRevise   Trigger = "revise"
	TriggerWithdraw Trigger = "withdraw"
	TriggerSelect   Trigger = "select"
	TriggerReject   Trigger = "reject"
	TriggerLapse    Trigger = "lapse"
)

// Machine is the proposal lifecycle.
var Machine = fsm.New("proposal", StatusActive,
	fsm.Transition[Status, Trigger]{From: StatusActive, On: TriggerRevise, To: StatusActive},
	fsm.Transition[Status, Trigger]{From: StatusActive, On: TriggerWithdraw, To: StatusWithdrawn},
	fsm.Transition[Status, Trigger]{From: StatusActive, On: TriggerSelect, To: StatusSelected},
	fsm.Transition[Status, Trigger]{From: StatusActive, On: TriggerReject, To: StatusRejected},
	fsm.Transition[Status, Trigger]{From: StatusActive, On: TriggerLapse, To: StatusLapsed},
)

// Event types. None of them carries a price.
const (
	EvSubmitted = "intenza.proposal.submitted.v1"
	EvRevised   = "intenza.proposal.revised.v1"
	EvWithdrawn = "intenza.proposal.withdrawn.v1"
)

var (
	ErrSelfDealing = errors.New("proposal: the buyer cannot answer its own intent")
	ErrIneligible  = errors.New("proposal: seller lacks the credentials this category requires")
	ErrPrice       = errors.New("proposal: total price must be a positive amount in BRL")
	ErrLeadTime    = errors.New("proposal: lead time between 1 and 60 days")
	ErrValidity    = errors.New("proposal: validity must cover the selection window, up to 30 days beyond it")
	ErrSchema      = errors.New("proposal: the category schema changed since the intent was published")
	ErrNotOwner    = errors.New("proposal: only its seller may do this")
)

// Proposal is the aggregate.
type Proposal struct {
	ID           string            `json:"id"`
	IntentID     string            `json:"intent_id"`
	SellerID     string            `json:"seller_id"`
	Attributes   map[string]any    `json:"attributes"`
	Facts        schema.Facts      `json:"-"`
	Price        Sealed            `json:"price"`
	LeadTimeDays int               `json:"lead_time_days"`
	ValidUntil   time.Time         `json:"valid_until"`
	Status       Status            `json:"status"`
	Revision     int               `json:"revision"`
	SubmittedAt  time.Time         `json:"submitted_at"`
	Version      int64             `json:"version"`
	Canonical    map[string]string `json:"canonical"`
}

// SubmitCommand is what the seller sends.
type SubmitCommand struct {
	ID           string
	SellerID     string
	Credentials  []taxonomy.Credential
	Attributes   map[string]any
	Price        money.Money
	LeadTimeDays int
	ValidUntil   time.Time
}

// Submitted is the payload of submission and revision events: identities and
// revision only.
type Submitted struct {
	ProposalID string `json:"proposal_id"`
	IntentID   string `json:"intent_id"`
	SellerID   string `json:"seller_id"`
	Revision   int    `json:"revision"`
}

func validate(cmd SubmitCommand, in intent.Intent, tree *taxonomy.Tree, now time.Time) (schema.Facts, map[string]string, error) {
	if err := in.AcceptsProposals(now); err != nil {
		return nil, nil, err
	}
	if cmd.SellerID == in.BuyerID {
		return nil, nil, ErrSelfDealing
	}
	cat, err := tree.Lookup(in.CategoryID)
	if err != nil {
		return nil, nil, err
	}
	if !cat.Eligible(cmd.Credentials) {
		return nil, nil, ErrIneligible
	}
	s := cat.Schema()
	if s.Version != in.SchemaVersion {
		return nil, nil, ErrSchema
	}
	facts, err := s.ParseFacts(cmd.Attributes)
	if err != nil {
		return nil, nil, err
	}
	if !cmd.Price.Positive() || cmd.Price.Currency != iso4217.BRL {
		return nil, nil, ErrPrice
	}
	if cmd.LeadTimeDays < 1 || cmd.LeadTimeDays > 60 {
		return nil, nil, ErrLeadTime
	}
	if cmd.ValidUntil.Before(in.SelectBy) || cmd.ValidUntil.After(in.SelectBy.AddDate(0, 0, 30)) {
		return nil, nil, ErrValidity
	}
	return facts, s.Canonical(facts), nil
}

// Submit validates and seals a new proposal.
func Submit(cmd SubmitCommand, in intent.Intent, tree *taxonomy.Tree, sealer *Sealer, now time.Time) (Proposal, []event.Event, error) {
	facts, canonical, err := validate(cmd, in, tree, now)
	if err != nil {
		return Proposal{}, nil, err
	}
	sealed, err := sealer.Seal(in.ID, cmd.ID, cmd.SellerID, 1, cmd.Price)
	if err != nil {
		return Proposal{}, nil, err
	}
	p := Proposal{
		ID: cmd.ID, IntentID: in.ID, SellerID: cmd.SellerID, Attributes: cmd.Attributes, Facts: facts, Canonical: canonical,
		Price: sealed, LeadTimeDays: cmd.LeadTimeDays, ValidUntil: cmd.ValidUntil, Status: StatusActive,
		Revision: 1, SubmittedAt: now, Version: 1,
	}
	return p, []event.Event{event.New(EvSubmitted, in.ID, now, Submitted{p.ID, in.ID, p.SellerID, 1})}, nil
}

// Revise replaces the attributes, price, lead time and validity while the
// window is open; the price is sealed again under the new revision.
func (p Proposal) Revise(cmd SubmitCommand, in intent.Intent, tree *taxonomy.Tree, sealer *Sealer, now time.Time) (Proposal, []event.Event, error) {
	if cmd.SellerID != p.SellerID {
		return p, nil, ErrNotOwner
	}
	if _, err := Machine.Next(p.Status, TriggerRevise); err != nil {
		return p, nil, err
	}
	facts, canonical, err := validate(cmd, in, tree, now)
	if err != nil {
		return p, nil, err
	}
	sealed, err := sealer.Seal(in.ID, p.ID, p.SellerID, p.Revision+1, cmd.Price)
	if err != nil {
		return p, nil, err
	}
	p.Attributes, p.Facts, p.Canonical, p.Price = cmd.Attributes, facts, canonical, sealed
	p.LeadTimeDays, p.ValidUntil = cmd.LeadTimeDays, cmd.ValidUntil
	p.Revision++
	p.Version++
	return p, []event.Event{event.New(EvRevised, in.ID, now, Submitted{p.ID, in.ID, p.SellerID, p.Revision})}, nil
}

// Withdraw takes the proposal back while the window is open.
func (p Proposal) Withdraw(sellerID string, in intent.Intent, now time.Time) (Proposal, []event.Event, error) {
	if sellerID != p.SellerID {
		return p, nil, ErrNotOwner
	}
	if err := in.AcceptsProposals(now); err != nil {
		return p, nil, err
	}
	next, err := p.transition(TriggerWithdraw)
	if err != nil {
		return p, nil, err
	}
	return next, []event.Event{event.New(EvWithdrawn, in.ID, now, Submitted{p.ID, in.ID, p.SellerID, p.Revision})}, nil
}

func (p Proposal) transition(t Trigger) (Proposal, error) {
	to, err := Machine.Next(p.Status, t)
	if err != nil {
		return p, err
	}
	p.Status = to
	p.Version++
	return p, nil
}

// Select marks the chosen proposal; Reject and Lapse settle the others.
func (p Proposal) Select() (Proposal, error) { return p.transition(TriggerSelect) }

// Reject marks a proposal that was not chosen.
func (p Proposal) Reject() (Proposal, error) { return p.transition(TriggerReject) }

// Lapse marks a proposal whose validity ran out.
func (p Proposal) Lapse() (Proposal, error) { return p.transition(TriggerLapse) }

// Reveal opens the price only when the intent is closed or awarded as
// persisted. It deliberately ignores EffectiveStatus: a node whose clock runs
// ahead may display "closed" but still cannot read a price.
func Reveal(in intent.Intent, p Proposal, sealer *Sealer) (money.Money, error) {
	if in.Status != intent.StatusClosed && in.Status != intent.StatusAwarded {
		return money.Money{}, ErrSealedNow
	}
	if p.IntentID != in.ID {
		return money.Money{}, ErrSealOpen
	}
	return sealer.open(in.ID, p.ID, p.SellerID, p.Revision, p.Price)
}

// OwnPrice lets the seller read back its own price at any time.
func OwnPrice(sellerID string, p Proposal, sealer *Sealer) (money.Money, error) {
	if sellerID != p.SellerID {
		return money.Money{}, ErrNotOwner
	}
	return sealer.open(p.IntentID, p.ID, p.SellerID, p.Revision, p.Price)
}
