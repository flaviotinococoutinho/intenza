//intenza:paradigm functional-core

// Package agreement is the contract born from a selection and its settlement
// saga. The money never passes through the platform: the escrow agent holds
// it, and this aggregate only decides when to ask for a release or a refund
// and records the agent's confirmation.
//
// The terms are frozen as canonical JSON (RFC 8785) and their SHA-256 travels
// with the agreement: a dispute compares delivered attributes against fields
// whose integrity anyone can recompute.
package agreement

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/domain/event"
	"github.com/flaviotinococoutinho/intenza/internal/domain/fsm"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/money"
	"github.com/flaviotinococoutinho/intenza/internal/domain/proposal"
	"github.com/flaviotinococoutinho/intenza/internal/domain/schema"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
	"github.com/flaviotinococoutinho/intenza/standards/ietf/rfc8785"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso8601"
)

// Status is the settlement phase.
type Status string

const (
	StatusAwaitingFunding Status = "awaiting-funding"
	StatusFunded          Status = "funded"
	StatusShipped         Status = "shipped"
	StatusDelivered       Status = "delivered"
	StatusDisputed        Status = "disputed"
	StatusReleasing       Status = "releasing"
	StatusRefunding       Status = "refunding"
	StatusReleased        Status = "released"
	StatusRefunded        Status = "refunded"
	StatusCancelled       Status = "cancelled"
)

// Trigger moves the saga.
type Trigger string

const (
	TriggerFund          Trigger = "fund"
	TriggerCancel        Trigger = "cancel"
	TriggerShip          Trigger = "ship"
	TriggerDeliver       Trigger = "deliver"
	TriggerAccept        Trigger = "accept"
	TriggerDispute       Trigger = "dispute"
	TriggerRuleRelease   Trigger = "rule-release"
	TriggerRuleRefund    Trigger = "rule-refund"
	TriggerRequestRefund Trigger = "request-refund"
	TriggerReleased      Trigger = "released"
	TriggerRefunded      Trigger = "refunded"
)

type tr = fsm.Transition[Status, Trigger]

// Machine is the settlement saga. No path leads from releasing to refunded or
// from refunding to released: the outcomes are exclusive by construction.
var Machine = fsm.New("agreement", StatusAwaitingFunding,
	tr{From: StatusAwaitingFunding, On: TriggerFund, To: StatusFunded},
	tr{From: StatusAwaitingFunding, On: TriggerCancel, To: StatusCancelled},
	tr{From: StatusFunded, On: TriggerShip, To: StatusShipped},
	tr{From: StatusFunded, On: TriggerRequestRefund, To: StatusRefunding},
	tr{From: StatusShipped, On: TriggerDeliver, To: StatusDelivered},
	tr{From: StatusDelivered, On: TriggerAccept, To: StatusReleasing},
	tr{From: StatusDelivered, On: TriggerDispute, To: StatusDisputed},
	tr{From: StatusDisputed, On: TriggerRuleRelease, To: StatusReleasing},
	tr{From: StatusDisputed, On: TriggerRuleRefund, To: StatusRefunding},
	tr{From: StatusReleasing, On: TriggerReleased, To: StatusReleased},
	tr{From: StatusRefunding, On: TriggerRefunded, To: StatusRefunded},
)

// Event types. EvReleaseRequested and EvRefundRequested are what the
// settlement saga turns into commands to the escrow agent.
const (
	EvFormed           = "intenza.agreement.formed.v1"
	EvFunded           = "intenza.agreement.funded.v1"
	EvShipped          = "intenza.agreement.shipped.v1"
	EvDelivered        = "intenza.agreement.delivered.v1"
	EvReleaseRequested = "intenza.agreement.release-requested.v1"
	EvRefundRequested  = "intenza.agreement.refund-requested.v1"
	EvDisputeOpened    = "intenza.agreement.dispute-opened.v1"
	EvDisputeResolved  = "intenza.agreement.dispute-resolved.v1"
	EvReleased         = "intenza.agreement.released.v1"
	EvRefunded         = "intenza.agreement.refunded.v1"
	EvCancelled        = "intenza.agreement.cancelled.v1"
)

var (
	ErrNotAwarded    = errors.New("agreement: intent must be awarded to the proposal")
	ErrTampered      = errors.New("agreement: terms do not match their hash")
	ErrNotParty      = errors.New("agreement: not a party to this agreement")
	ErrTooEarly      = errors.New("agreement: deadline not reached")
	ErrTooLate       = errors.New("agreement: window is over")
	ErrNoDiscrepancy = errors.New("agreement: the claims do not contradict the terms")
	ErrConflict      = errors.New("agreement: confirmation contradicts the settled outcome")
	ErrTracking      = errors.New("agreement: tracking code required")
)

// Policy holds the settlement windows.
type Policy struct {
	FundingWindow    iso8601.Duration
	InspectionWindow iso8601.Duration
}

// DefaultPolicy gives the buyer two days to pay and seven to inspect.
var DefaultPolicy = Policy{FundingWindow: iso8601.MustParse("P2D"), InspectionWindow: iso8601.MustParse("P7D")}

// Terms is the frozen content of the contract.
type Terms struct {
	IntentID      string            `json:"intent_id"`
	ProposalID    string            `json:"proposal_id"`
	BuyerID       string            `json:"buyer_id"`
	SellerID      string            `json:"seller_id"`
	CategoryID    string            `json:"category_id"`
	MCC           string            `json:"mcc"`
	SchemaVersion int               `json:"schema_version"`
	Constraints   map[string]any    `json:"constraints"`
	Attributes    map[string]string `json:"attributes"`
	Evidentiary   []string          `json:"evidentiary"`
	Price         money.Money       `json:"price"`
	LeadTimeDays  int               `json:"lead_time_days"`
	DeliveryCEP   string            `json:"delivery_cep"`
	FormedAt      time.Time         `json:"formed_at"`
}

// Hash is the SHA-256 of the canonical JSON of the terms, in hex.
func (t Terms) Hash() (string, error) {
	raw, err := rfc8785.Marshal(t)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// ClaimedField is the buyer's statement about one evidentiary attribute.
type ClaimedField struct {
	Field    string `json:"field"`
	Observed any    `json:"observed"`
}

// Dispute records a field conference.
type Dispute struct {
	Claims        []ClaimedField       `json:"claims"`
	Discrepancies []schema.Discrepancy `json:"discrepancies"`
	OpenedAt      time.Time            `json:"opened_at"`
	Verdict       Verdict              `json:"verdict,omitempty"`
	ResolvedAt    *time.Time           `json:"resolved_at,omitempty"`
}

// Verdict is the outcome of a dispute.
type Verdict string

const (
	VerdictRelease Verdict = "release"
	VerdictRefund  Verdict = "refund"
)

// Agreement is the aggregate.
type Agreement struct {
	ID           string     `json:"id"`
	Terms        Terms      `json:"terms"`
	TermsHash    string     `json:"terms_hash"`
	Status       Status     `json:"status"`
	EscrowID     string     `json:"escrow_id,omitempty"`
	FundBy       time.Time  `json:"fund_by"`
	ShipBy       *time.Time `json:"ship_by,omitempty"`
	InspectUntil *time.Time `json:"inspect_until,omitempty"`
	Tracking     string     `json:"tracking,omitempty"`
	Dispute      *Dispute   `json:"dispute,omitempty"`
	Version      int64      `json:"version"`
}

// Form freezes the terms of an awarded intent and its selected proposal.
func Form(id string, in intent.Intent, p proposal.Proposal, price money.Money, tree *taxonomy.Tree, pol Policy, now time.Time) (Agreement, []event.Event, error) {
	if in.Status != intent.StatusAwarded || in.AwardedProposal != p.ID || p.IntentID != in.ID {
		return Agreement{}, nil, ErrNotAwarded
	}
	cat, err := tree.Lookup(in.CategoryID)
	if err != nil {
		return Agreement{}, nil, err
	}
	terms := Terms{
		IntentID: in.ID, ProposalID: p.ID, BuyerID: in.BuyerID, SellerID: p.SellerID, CategoryID: in.CategoryID,
		MCC: cat.MCC().String(), SchemaVersion: in.SchemaVersion, Constraints: in.Constraints, Attributes: p.Canonical,
		Evidentiary: cat.Schema().Evidentiary(), Price: price, LeadTimeDays: p.LeadTimeDays, DeliveryCEP: in.DeliveryCEP,
		FormedAt: now.UTC().Truncate(time.Second),
	}
	hash, err := terms.Hash()
	if err != nil {
		return Agreement{}, nil, err
	}
	a := Agreement{ID: id, Terms: terms, TermsHash: hash, Status: StatusAwaitingFunding, FundBy: pol.FundingWindow.AddTo(now), Version: 1}
	return a, []event.Event{a.event(EvFormed, now, map[string]any{
		"intent_id": in.ID, "proposal_id": p.ID, "buyer_id": terms.BuyerID, "seller_id": terms.SellerID,
		"price": price, "terms_hash": hash, "fund_by": a.FundBy,
	})}, nil
}

func (a Agreement) event(typ string, now time.Time, data map[string]any) event.Event {
	if data == nil {
		data = map[string]any{}
	}
	data["agreement_id"] = a.ID
	return event.New(typ, a.ID, now, data)
}

// VerifyTerms recomputes the hash; any change to the stored terms shows.
func (a Agreement) VerifyTerms() error {
	h, err := a.Terms.Hash()
	if err != nil || h != a.TermsHash {
		return ErrTampered
	}
	return nil
}

func (a Agreement) apply(t Trigger) (Agreement, error) {
	to, err := Machine.Next(a.Status, t)
	if err != nil {
		return a, err
	}
	a.Status = to
	a.Version++
	return a, nil
}

func (a Agreement) party(id string) (buyer, seller bool) {
	return id == a.Terms.BuyerID, id == a.Terms.SellerID
}

// Funded records that the escrow agent holds the money; the seller's
// shipping deadline starts now.
func (a Agreement) Funded(escrowID string, now time.Time) (Agreement, []event.Event, error) {
	next, err := a.apply(TriggerFund)
	if err != nil {
		return a, nil, err
	}
	shipBy := now.AddDate(0, 0, a.Terms.LeadTimeDays)
	next.EscrowID, next.ShipBy = escrowID, &shipBy
	return next, []event.Event{next.event(EvFunded, now, map[string]any{"escrow_id": escrowID, "ship_by": shipBy})}, nil
}

// Cancel ends an agreement whose payment never arrived.
func (a Agreement) Cancel(now time.Time) (Agreement, []event.Event, error) {
	if now.Before(a.FundBy) {
		return a, nil, ErrTooEarly
	}
	next, err := a.apply(TriggerCancel)
	if err != nil {
		return a, nil, err
	}
	return next, []event.Event{next.event(EvCancelled, now, nil)}, nil
}

// Ship records the seller's dispatch.
func (a Agreement) Ship(sellerID, tracking string, now time.Time) (Agreement, []event.Event, error) {
	if _, s := a.party(sellerID); !s {
		return a, nil, ErrNotParty
	}
	if tracking == "" {
		return a, nil, ErrTracking
	}
	next, err := a.apply(TriggerShip)
	if err != nil {
		return a, nil, err
	}
	next.Tracking = tracking
	return next, []event.Event{next.event(EvShipped, now, map[string]any{"tracking": tracking})}, nil
}

// Deliver records the arrival; the inspection window starts now.
func (a Agreement) Deliver(buyerID string, pol Policy, now time.Time) (Agreement, []event.Event, error) {
	if b, _ := a.party(buyerID); !b {
		return a, nil, ErrNotParty
	}
	next, err := a.apply(TriggerDeliver)
	if err != nil {
		return a, nil, err
	}
	until := pol.InspectionWindow.AddTo(now)
	next.InspectUntil = &until
	return next, []event.Event{next.event(EvDelivered, now, map[string]any{"inspect_until": until})}, nil
}

// Accept is the buyer's approval: the platform asks the agent to release.
func (a Agreement) Accept(buyerID string, now time.Time) (Agreement, []event.Event, error) {
	if b, _ := a.party(buyerID); !b {
		return a, nil, ErrNotParty
	}
	return a.requestRelease(TriggerAccept, now)
}

// AutoAccept approves on the buyer's behalf when the inspection window lapses.
func (a Agreement) AutoAccept(now time.Time) (Agreement, []event.Event, error) {
	if a.InspectUntil == nil || now.Before(*a.InspectUntil) {
		return a, nil, ErrTooEarly
	}
	return a.requestRelease(TriggerAccept, now)
}

func (a Agreement) requestRelease(t Trigger, now time.Time) (Agreement, []event.Event, error) {
	next, err := a.apply(t)
	if err != nil {
		return a, nil, err
	}
	return next, []event.Event{next.event(EvReleaseRequested, now, map[string]any{
		"escrow_id": a.EscrowID, "idempotency_key": "release:" + a.ID, "price": a.Terms.Price,
	})}, nil
}

// RequestRefund is the buyer's way out when the seller misses the shipping deadline.
func (a Agreement) RequestRefund(buyerID string, now time.Time) (Agreement, []event.Event, error) {
	if b, _ := a.party(buyerID); !b {
		return a, nil, ErrNotParty
	}
	if a.ShipBy == nil || now.Before(*a.ShipBy) {
		return a, nil, ErrTooEarly
	}
	return a.requestRefund(TriggerRequestRefund, now)
}

func (a Agreement) requestRefund(t Trigger, now time.Time) (Agreement, []event.Event, error) {
	next, err := a.apply(t)
	if err != nil {
		return a, nil, err
	}
	return next, []event.Event{next.event(EvRefundRequested, now, map[string]any{
		"escrow_id": a.EscrowID, "idempotency_key": "refund:" + a.ID, "price": a.Terms.Price,
	})}, nil
}

// OpenDispute runs the field conference on the claimed attributes. A dispute
// exists only if at least one claim contradicts a fixed evidentiary field.
func (a Agreement) OpenDispute(buyerID string, claims []ClaimedField, tree *taxonomy.Tree, now time.Time) (Agreement, []event.Event, error) {
	if b, _ := a.party(buyerID); !b {
		return a, nil, ErrNotParty
	}
	if a.InspectUntil == nil || !now.Before(*a.InspectUntil) {
		return a, nil, ErrTooLate
	}
	if err := a.VerifyTerms(); err != nil {
		return a, nil, err
	}
	cat, err := tree.Lookup(a.Terms.CategoryID)
	if err != nil {
		return a, nil, err
	}
	s := cat.Schema()
	promised, err := factsFromCanonical(s, a.Terms.Attributes)
	if err != nil {
		return a, nil, err
	}
	observed := schema.Facts{}
	var keys []string
	var errs schema.Errors
	for _, c := range claims {
		f, ok := s.Field(c.Field)
		if !ok {
			errs = append(errs, schema.FieldError{Field: c.Field, Reason: "atributo não existe no schema"})
			continue
		}
		v, err := f.ParseValue(c.Observed)
		if err != nil {
			errs = append(errs, schema.FieldError{Field: c.Field, Reason: err.Error()})
			continue
		}
		observed[c.Field] = v
		keys = append(keys, c.Field)
	}
	if len(errs) > 0 {
		return a, nil, errs
	}
	disc, err := s.Conference(promised, observed, keys)
	if err != nil {
		return a, nil, err
	}
	if len(disc) == 0 {
		return a, nil, ErrNoDiscrepancy
	}
	next, err := a.apply(TriggerDispute)
	if err != nil {
		return a, nil, err
	}
	next.Dispute = &Dispute{Claims: claims, Discrepancies: disc, OpenedAt: now}
	return next, []event.Event{next.event(EvDisputeOpened, now, map[string]any{"discrepancies": disc})}, nil
}

// factsFromCanonical turns the frozen strings back into typed facts.
func factsFromCanonical(s schema.Schema, canonical map[string]string) (schema.Facts, error) {
	raw := make(map[string]any, len(canonical))
	for k, v := range canonical {
		f, ok := s.Field(k)
		if !ok {
			return nil, ErrTampered
		}
		if f.Kind == schema.Bool {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, ErrTampered
			}
			raw[k] = b
			continue
		}
		raw[k] = v
	}
	return s.ParseFacts(raw)
}

// Decide is the arbitration rule: a discrepancy confirmed by inspection means
// refund; otherwise the seller is paid. Deterministic on purpose.
func Decide(d Dispute, confirmed []string) Verdict {
	for _, disc := range d.Discrepancies {
		for _, c := range confirmed {
			if c == disc.Field {
				return VerdictRefund
			}
		}
	}
	return VerdictRelease
}

// Resolve applies a verdict to a disputed agreement.
func (a Agreement) Resolve(v Verdict, now time.Time) (Agreement, []event.Event, error) {
	if a.Dispute == nil {
		return a, nil, ErrNoDiscrepancy
	}
	var (
		next Agreement
		evs  []event.Event
		err  error
	)
	switch v {
	case VerdictRelease:
		next, evs, err = a.requestRelease(TriggerRuleRelease, now)
	case VerdictRefund:
		next, evs, err = a.requestRefund(TriggerRuleRefund, now)
	default:
		return a, nil, errors.New("agreement: unknown verdict")
	}
	if err != nil {
		return a, nil, err
	}
	d := *a.Dispute
	d.Verdict, d.ResolvedAt = v, &now
	next.Dispute = &d
	return next, append([]event.Event{next.event(EvDisputeResolved, now, map[string]any{"verdict": v})}, evs...), nil
}

// Settled records the escrow agent's confirmation. A duplicate confirmation
// of the same outcome changes nothing; one that contradicts a settled outcome
// is refused.
func (a Agreement) Settled(released bool, now time.Time) (Agreement, []event.Event, error) {
	target, trigger, typ := StatusRefunded, TriggerRefunded, EvRefunded
	if released {
		target, trigger, typ = StatusReleased, TriggerReleased, EvReleased
	}
	switch {
	case a.Status == target:
		return a, nil, nil // duplicate webhook: idempotent
	case Machine.Terminal(a.Status):
		return a, nil, ErrConflict
	}
	next, err := a.apply(trigger)
	if err != nil {
		return a, nil, err
	}
	return next, []event.Event{next.event(typ, now, map[string]any{"escrow_id": a.EscrowID})}, nil
}
