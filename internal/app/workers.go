package app

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/domain/agreement"
	"github.com/flaviotinococoutinho/intenza/internal/domain/event"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/reputation"
)

// AttachEscrow records the escrow opened for an agreement.
func (m *Market) AttachEscrow(ctx context.Context, id string, ref EscrowRef) (agreement.Agreement, error) {
	var out agreement.Agreement
	err := m.Store.Tx(ctx, func(ctx context.Context, tx Tx) error {
		a, err := tx.Agreement(ctx, id)
		if err != nil {
			return err
		}
		next, err := a.AttachEscrow(ref.ID, ref.PaymentCode)
		if err != nil {
			return err
		}
		out = next
		if next.Version == a.Version {
			return nil
		}
		return tx.SaveAgreement(ctx, next, a.Version)
	})
	return out, err
}

// AgreementDeadline applies whichever window lapsed: unpaid agreements are
// cancelled, delivered ones are accepted on the buyer's behalf.
func (m *Market) AgreementDeadline(ctx context.Context, id string) (agreement.Agreement, error) {
	return m.mutateAgreement(ctx, id, func(a agreement.Agreement, now time.Time, _ Tx) (agreement.Agreement, []event.Event, error) {
		if a.Status == agreement.StatusAwaitingFunding {
			return a.Cancel(now)
		}
		if a.Status == agreement.StatusDelivered {
			return a.AutoAccept(now)
		}
		return a, nil, nil
	})
}

// systemAgreement reads an agreement without a viewer check (workers only).
func (m *Market) systemAgreement(ctx context.Context, id string) (agreement.Agreement, error) {
	var a agreement.Agreement
	err := m.Store.View(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		a, err = tx.Agreement(ctx, id)
		return err
	})
	return a, err
}

// Settlement turns agreement events into commands to the escrow agent and
// reconciles with it when confirmations go missing.
type Settlement struct {
	Market *Market
	Escrow Escrow
	Back   Backoffice
	Log    *slog.Logger
}

// Handle reacts to one agreement event. Every command carries an idempotency
// key derived from the agreement, so redelivered events repeat nothing.
func (s *Settlement) Handle(ctx context.Context, eventType, agreementID string) error {
	switch eventType {
	case agreement.EvFormed:
		return s.open(ctx, agreementID)
	case agreement.EvReleaseRequested:
		return s.command(ctx, agreementID, agreement.StatusReleasing)
	case agreement.EvRefundRequested:
		return s.command(ctx, agreementID, agreement.StatusRefunding)
	}
	return nil
}

func (s *Settlement) open(ctx context.Context, id string) error {
	a, err := s.Market.systemAgreement(ctx, id)
	if err != nil || a.EscrowID != "" || a.Status != agreement.StatusAwaitingFunding {
		return err
	}
	ref, err := s.Escrow.Open(ctx, EscrowRequest{
		Reference: a.ID, IdempotencyKey: "open:" + a.ID, AmountMinor: a.Terms.Price.Minor, Currency: a.Terms.Price.Currency.Alpha(),
		MCC: a.Terms.MCC, Payer: a.Terms.BuyerID, Payee: a.Terms.SellerID,
	})
	if err != nil {
		return err
	}
	_, err = s.Market.AttachEscrow(ctx, id, ref)
	return err
}

func (s *Settlement) command(ctx context.Context, id string, want agreement.Status) error {
	a, err := s.Market.systemAgreement(ctx, id)
	if err != nil || a.Status != want || a.EscrowID == "" {
		return err
	}
	if want == agreement.StatusReleasing {
		return s.Escrow.Release(ctx, a.EscrowID, "release:"+a.ID)
	}
	return s.Escrow.Refund(ctx, a.EscrowID, "refund:"+a.ID)
}

// Reconcile asks the agent about every agreement stuck waiting on it and
// applies what it says. With webhooks lost, this is how state converges.
func (s *Settlement) Reconcile(ctx context.Context, olderThan time.Duration) (int, error) {
	fixed := 0
	orphans, err := s.Back.FormedWithoutEscrow(ctx, olderThan, 200)
	if err != nil {
		return 0, err
	}
	for _, a := range orphans {
		if err := s.open(ctx, a.ID); err == nil {
			fixed++
		}
	}
	pending, err := s.Back.PendingSettlements(ctx, olderThan, 200)
	if err != nil {
		return fixed, err
	}
	var errs []error
	for _, a := range pending {
		st, err := s.Escrow.Status(ctx, a.EscrowID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		switch {
		case st == EscrowHeld && a.Status == agreement.StatusAwaitingFunding:
			_, err = s.Market.Funded(ctx, a.ID, a.EscrowID)
		case st == EscrowReleased && a.Status == agreement.StatusReleasing:
			_, err = s.Market.Settle(ctx, a.ID, true)
		case st == EscrowRefunded && a.Status == agreement.StatusRefunding:
			_, err = s.Market.Settle(ctx, a.ID, false)
		case st == EscrowHeld && (a.Status == agreement.StatusReleasing || a.Status == agreement.StatusRefunding):
			err = s.command(ctx, a.ID, a.Status) // the command itself was lost: repeat it, it is idempotent
		default:
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		fixed++
	}
	return fixed, errors.Join(errs...)
}

// Deadlines fires lifecycle deadlines from the fast index (Redis) and from a
// sweep of the store, which is the source of truth when the index is lost.
type Deadlines struct {
	Market *Market
	Claim  func(ctx context.Context, now time.Time, limit int) ([]string, error) // nil without Redis
	Log    *slog.Logger
	OnFire func(kind, source string, lateness time.Duration) // metrics hook
}

// Tick drains due entries from the index.
func (d *Deadlines) Tick(ctx context.Context) error {
	if d.Claim == nil {
		return nil
	}
	due, err := d.Claim(ctx, time.Now(), 200)
	if err != nil {
		return err
	}
	for _, member := range due {
		kind, id, ok := strings.Cut(member, ":")
		if ok {
			d.fire(ctx, kind, id, "index")
		}
	}
	return nil
}

// Sweep finds everything overdue by the database clock.
func (d *Deadlines) Sweep(ctx context.Context) error {
	closing, expiring, err := d.Market.Store.DueIntents(ctx, 500)
	if err != nil {
		return err
	}
	for _, id := range closing {
		d.fire(ctx, "intent-close", id, "sweep")
	}
	for _, id := range expiring {
		d.fire(ctx, "intent-expire", id, "sweep")
	}
	agreements, err := d.Market.Store.DueAgreements(ctx, 500)
	if err != nil {
		return err
	}
	for _, id := range agreements {
		d.fire(ctx, "agreement-deadline", id, "sweep")
	}
	return nil
}

// fire runs one deadline. Lateness is reported only by the trigger that made
// the transition: when the index comes back after an outage, replaying what
// the sweep already closed must not look like late deadlines.
func (d *Deadlines) fire(ctx context.Context, kind, id, source string) {
	var (
		due   time.Time
		fired bool
		err   error
	)
	switch kind {
	case "intent-close":
		var in intent.Intent
		in, fired, err = d.Market.closeIntent(ctx, id)
		due = in.ClosesAt
	case "intent-expire":
		var in intent.Intent
		in, fired, err = d.Market.expireIntent(ctx, id)
		due = in.SelectBy
	default: // agreement-funding, agreement-inspection, agreement-deadline
		_, err = d.Market.AgreementDeadline(ctx, id)
	}
	if err != nil && !errors.Is(err, intent.ErrTooEarly) && !errors.Is(err, agreement.ErrTooEarly) {
		d.Log.WarnContext(ctx, "prazo falhou, a varredura tenta de novo", "kind", kind, "id", id, "err", err)
		return
	}
	if d.OnFire != nil && fired {
		d.OnFire(kind, source, time.Since(due))
	}
}

// Fanout notifies a sample of eligible sellers about a new intent, choosing by
// Thompson sampling so sellers without history still get seen.
type Fanout struct {
	Market *Market
	Back   Backoffice
	Inbox  Inbox
	K      int
	Log    *slog.Logger

	mu  sync.Mutex
	rng *rand.Rand
}

// NewFanout seeds the sampler (the seed is logged so a run can be replayed).
func NewFanout(m *Market, back Backoffice, inbox Inbox, k int, seed uint64, log *slog.Logger) *Fanout {
	log.Info("fan-out com semente", "seed", seed)
	return &Fanout{Market: m, Back: back, Inbox: inbox, K: k, Log: log, rng: rand.New(rand.NewPCG(seed, seed^0xda3e39cb94b95bdb))}
}

// Handle processes one intent.published event, at most once.
func (f *Fanout) Handle(ctx context.Context, eventID, intentID string) error {
	ran, err := f.Inbox.Once(ctx, "fanout", eventID, func(ctx context.Context, tx Tx) error {
		in, err := tx.Intent(ctx, intentID)
		if err != nil {
			return err
		}
		cat, err := f.Market.Tree.Lookup(in.CategoryID)
		if err != nil {
			return err
		}
		var required []string
		for _, c := range cat.Requires() {
			required = append(required, string(c))
		}
		candidates, err := f.Back.EligibleSellers(ctx, required, in.BuyerID, 200)
		if err != nil {
			return err
		}
		contenders := make([]reputation.Contender, 0, len(candidates))
		now := f.Market.Clock.Now()
		for _, c := range candidates {
			rs, err := tx.Ratings(ctx, c.ID, reputation.AsSeller)
			if err != nil {
				return err
			}
			contenders = append(contenders, reputation.Contender{
				SellerID: c.ID,
				Score:    reputation.Estimate(rs, reputation.DefaultPrior, reputation.DefaultHalfLife, now),
			})
		}
		f.mu.Lock()
		chosen := reputation.Explore(f.rng, contenders, f.K)
		f.mu.Unlock()
		return f.Back.Notify(ctx, in.ID, chosen)
	})
	if err == nil && !ran {
		f.Market.fact(ctx, "inbox.duplicate") // a redelivery absorbed by the inbox
	}
	return err
}
