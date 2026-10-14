package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/domain/agreement"
	"github.com/flaviotinococoutinho/intenza/internal/domain/event"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/money"
	"github.com/flaviotinococoutinho/intenza/internal/domain/proposal"
	"github.com/flaviotinococoutinho/intenza/internal/domain/reputation"
	"github.com/flaviotinococoutinho/intenza/internal/domain/schema"
	"github.com/flaviotinococoutinho/intenza/internal/domain/selection"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
	"github.com/flaviotinococoutinho/intenza/standards/rfb/cnpj"
	"github.com/flaviotinococoutinho/intenza/standards/rfb/cpf"
)

// Market wires the use cases to their ports.
type Market struct {
	Store      Store
	Scheduler  Scheduler
	IDs        IDs
	Clock      Clock
	Sealer     *proposal.Sealer
	Tree       *taxonomy.Tree
	Intents    intent.Policy
	Settlement agreement.Policy
	RatingDays int
	Log        *slog.Logger
}

func (m *Market) stamp(ctx context.Context, evs []event.Event) []Envelope {
	out := make([]Envelope, len(evs))
	for i, e := range evs {
		out[i] = Envelope{ID: m.IDs.New(), TraceParent: TraceParent(ctx), Event: e}
	}
	return out
}

// TraceParent is set by the transport layer; empty when tracing is off.
var TraceParent = func(context.Context) string { return "" }

func (m *Market) schedule(ctx context.Context, kind, id string, at time.Time) {
	if m.Scheduler == nil {
		return
	}
	if err := m.Scheduler.Schedule(ctx, kind, id, at); err != nil {
		// The deadline still happens: the sweeper reads the store.
		m.Log.WarnContext(ctx, "agendamento falhou, a varredura assume", "kind", kind, "id", id, "err", err)
	}
}

// RegisterPartyInput is the dev-mode registration (real KYC is out of scope).
type RegisterPartyInput struct {
	ID          string
	Name        string
	Document    string
	Credentials []taxonomy.Credential
	Arbiter     bool
}

// RegisterParty validates the document with the RFB SDKs and stores the party.
func (m *Market) RegisterParty(ctx context.Context, in RegisterPartyInput) (Party, error) {
	p := Party{ID: in.ID, Name: strings.TrimSpace(in.Name), Credentials: in.Credentials, Arbiter: in.Arbiter, CreatedAt: m.Clock.Now()}
	if p.ID == "" {
		p.ID = m.IDs.New()
	}
	if doc, err := cpf.Normalize(in.Document); err == nil {
		p.Document, p.Kind = doc, "person"
	} else if doc, err := cnpj.Normalize(in.Document); err == nil {
		p.Document, p.Kind = doc, "company"
		if !hasCredential(p.Credentials, taxonomy.Business) {
			p.Credentials = append(p.Credentials, taxonomy.Business)
		}
	} else {
		return Party{}, fmt.Errorf("%w: documento não é CPF nem CNPJ válido", schema.ErrInvalid)
	}
	if p.Kind == "person" && hasCredential(p.Credentials, taxonomy.Business) {
		return Party{}, fmt.Errorf("%w: credencial de empresa exige CNPJ", ErrForbidden)
	}
	if p.Name == "" {
		return Party{}, fmt.Errorf("%w: nome obrigatório", schema.ErrInvalid)
	}
	if !hasCredential(p.Credentials, taxonomy.KYCBasic) {
		p.Credentials = append(p.Credentials, taxonomy.KYCBasic)
	}
	err := m.Store.Tx(ctx, func(ctx context.Context, tx Tx) error { return tx.InsertParty(ctx, p) })
	return p, err
}

func hasCredential(cs []taxonomy.Credential, c taxonomy.Credential) bool {
	for _, x := range cs {
		if x == c {
			return true
		}
	}
	return false
}

// PublishIntent creates an intent. With an idempotency key the id is derived
// from (buyer, key), so a retry finds the intent already there and gets it
// back: the database's unique _id is the guarantee, with or without Redis.
func (m *Market) PublishIntent(ctx context.Context, buyerID, idempotencyKey string, cmd intent.PublishCommand) (intent.Intent, bool, error) {
	cmd.BuyerID = buyerID
	cmd.ID = m.IDs.New()
	if idempotencyKey != "" {
		cmd.ID = m.IDs.Derived("intent", buyerID+"|"+idempotencyKey)
	}
	var out intent.Intent
	created := true
	err := m.Store.Tx(ctx, func(ctx context.Context, tx Tx) error {
		if existing, err := tx.Intent(ctx, cmd.ID); err == nil {
			out, created = existing, false
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if _, err := tx.Party(ctx, buyerID); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		in, evs, err := intent.Publish(cmd, m.Tree, m.Intents, now)
		if err != nil {
			return err
		}
		if err := tx.InsertIntent(ctx, in); err != nil {
			return err
		}
		out = in
		return tx.Outbox(ctx, m.stamp(ctx, evs))
	})
	if err == nil && created {
		m.schedule(ctx, "intent-close", out.ID, out.ClosesAt)
	}
	return out, created, err
}

// IntentView is an intent as a given viewer may see it.
type IntentView struct {
	intent.Intent
	EffectiveStatus intent.Status `json:"effective_status"`
}

// Intent returns an intent with its effective status by the local clock.
func (m *Market) Intent(ctx context.Context, id string) (IntentView, error) {
	var in intent.Intent
	err := m.Store.View(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		in, err = tx.Intent(ctx, id)
		return err
	})
	return IntentView{in, in.EffectiveStatus(m.Clock.Now())}, err
}

// OpenIntents lists open intents of a category subtree (sellers browse these).
func (m *Market) OpenIntents(ctx context.Context, categoryID string, limit int) ([]intent.Intent, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return m.Store.OpenIntents(ctx, categoryID, limit)
}

// CancelIntent withdraws an intent before a choice.
func (m *Market) CancelIntent(ctx context.Context, buyerID, id string) (intent.Intent, error) {
	var out intent.Intent
	err := m.Store.Tx(ctx, func(ctx context.Context, tx Tx) error {
		in, err := tx.Intent(ctx, id)
		if err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		next, evs, err := in.Cancel(buyerID, now)
		if err != nil {
			return err
		}
		if err := tx.SaveIntent(ctx, next, in.Version, GuardNone); err != nil {
			return err
		}
		out = next
		return tx.Outbox(ctx, m.stamp(ctx, evs))
	})
	return out, err
}

// SubmitInput is a seller's proposal.
type SubmitInput struct {
	Attributes   map[string]any
	Price        money.Money
	LeadTimeDays int
	ValidUntil   time.Time
}

// SubmitProposal seals and stores a proposal. The intent is saved in the same
// transaction with a database-clock guard (status open, closes_at > $$NOW):
// a race with the closer ends in a write conflict and a retry that sees the
// intent closed, never in a proposal past the deadline.
func (m *Market) SubmitProposal(ctx context.Context, sellerID, intentID string, in SubmitInput) (proposal.Proposal, error) {
	var out proposal.Proposal
	err := m.Store.Tx(ctx, func(ctx context.Context, tx Tx) error {
		seller, err := tx.Party(ctx, sellerID)
		if err != nil {
			return err
		}
		it, err := tx.Intent(ctx, intentID)
		if err != nil {
			return err
		}
		if it, err = it.Rehydrate(m.Tree); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		p, evs, err := proposal.Submit(proposal.SubmitCommand{
			ID: m.IDs.New(), SellerID: sellerID, Credentials: seller.Credentials, Attributes: in.Attributes,
			Price: in.Price, LeadTimeDays: in.LeadTimeDays, ValidUntil: in.ValidUntil,
		}, it, m.Tree, m.Sealer, now)
		if err != nil {
			return err
		}
		counted, err := it.CountProposal(now)
		if err != nil {
			return err
		}
		if err := tx.SaveIntent(ctx, counted, it.Version, GuardOpenBeforeEnd); err != nil {
			return err
		}
		if err := tx.InsertProposal(ctx, p); err != nil {
			return err
		}
		out = p
		return tx.Outbox(ctx, m.stamp(ctx, evs))
	})
	return out, err
}

// ReviseProposal replaces the seller's active proposal while the window is open.
func (m *Market) ReviseProposal(ctx context.Context, sellerID, proposalID string, in SubmitInput) (proposal.Proposal, error) {
	var out proposal.Proposal
	err := m.Store.Tx(ctx, func(ctx context.Context, tx Tx) error {
		seller, err := tx.Party(ctx, sellerID)
		if err != nil {
			return err
		}
		p, err := tx.Proposal(ctx, proposalID)
		if err != nil {
			return err
		}
		it, err := tx.Intent(ctx, p.IntentID)
		if err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		next, evs, err := p.Revise(proposal.SubmitCommand{
			ID: p.ID, SellerID: sellerID, Credentials: seller.Credentials, Attributes: in.Attributes,
			Price: in.Price, LeadTimeDays: in.LeadTimeDays, ValidUntil: in.ValidUntil,
		}, it, m.Tree, m.Sealer, now)
		if err != nil {
			return err
		}
		// Touch the intent under the same guard so a revision cannot land after the close.
		if err := tx.SaveIntent(ctx, it, it.Version, GuardOpenBeforeEnd); err != nil {
			return err
		}
		if err := tx.SaveProposal(ctx, next, p.Version); err != nil {
			return err
		}
		out = next
		return tx.Outbox(ctx, m.stamp(ctx, evs))
	})
	return out, err
}

// WithdrawProposal takes the seller's proposal back while the window is open.
func (m *Market) WithdrawProposal(ctx context.Context, sellerID, proposalID string) (proposal.Proposal, error) {
	var out proposal.Proposal
	err := m.Store.Tx(ctx, func(ctx context.Context, tx Tx) error {
		p, err := tx.Proposal(ctx, proposalID)
		if err != nil {
			return err
		}
		it, err := tx.Intent(ctx, p.IntentID)
		if err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		next, evs, err := p.Withdraw(sellerID, it, now)
		if err != nil {
			return err
		}
		if err := tx.SaveIntent(ctx, it, it.Version, GuardOpenBeforeEnd); err != nil {
			return err
		}
		if err := tx.SaveProposal(ctx, next, p.Version); err != nil {
			return err
		}
		out = next
		return tx.Outbox(ctx, m.stamp(ctx, evs))
	})
	return out, err
}

// ProposalView is a proposal plus what the viewer is entitled to see.
type ProposalView struct {
	proposal.Proposal
	TotalPrice *money.Money      `json:"total_price,omitempty"`
	Adherence  *float64          `json:"adherence,omitempty"`
	Compliant  *bool             `json:"compliant,omitempty"`
	Matches    []schema.Match    `json:"matches,omitempty"`
	Seller     *reputation.Score `json:"seller_reputation,omitempty"`
}

// Proposals returns what the viewer may see: the buyer sees every proposal
// with its price only once the intent is closed as persisted; a seller sees
// only its own, with its own price.
func (m *Market) Proposals(ctx context.Context, viewerID, intentID string) ([]ProposalView, error) {
	var out []ProposalView
	err := m.Store.View(ctx, func(ctx context.Context, tx Tx) error {
		it, err := tx.Intent(ctx, intentID)
		if err != nil {
			return err
		}
		if it, err = it.Rehydrate(m.Tree); err != nil {
			return err
		}
		ps, err := tx.Proposals(ctx, intentID)
		if err != nil {
			return err
		}
		cat, err := m.Tree.Lookup(it.CategoryID)
		if err != nil {
			return err
		}
		for _, p := range ps {
			if viewerID != it.BuyerID && viewerID != p.SellerID {
				continue
			}
			facts, err := cat.Schema().ParseFacts(p.Attributes)
			if err != nil {
				return err
			}
			score, compliant, matches := schema.Adherence(it.Predicates, facts)
			v := ProposalView{Proposal: p, Adherence: &score, Compliant: &compliant, Matches: matches}
			var price money.Money
			if viewerID == p.SellerID {
				price, err = proposal.OwnPrice(viewerID, p, m.Sealer)
			} else {
				price, err = proposal.Reveal(it, p, m.Sealer)
			}
			switch {
			case err == nil:
				v.TotalPrice = &price
			case errors.Is(err, proposal.ErrSealedNow):
			default:
				return err
			}
			ratings, err := tx.Ratings(ctx, p.SellerID, reputation.AsSeller)
			if err != nil {
				return err
			}
			rep := reputation.Estimate(ratings, reputation.DefaultPrior, reputation.DefaultHalfLife, m.Clock.Now())
			v.Seller = &rep
			out = append(out, v)
		}
		if viewerID != it.BuyerID && len(out) == 0 {
			return ErrForbidden
		}
		return nil
	})
	return out, err
}

// Ranking orders the revealed proposals for the buyer.
func (m *Market) Ranking(ctx context.Context, buyerID, intentID string, w selection.Weights, method selection.Method) ([]selection.Ranked, error) {
	views, err := m.Proposals(ctx, buyerID, intentID)
	if err != nil {
		return nil, err
	}
	var cands []selection.Candidate
	for _, v := range views {
		if v.TotalPrice == nil {
			return nil, proposal.ErrSealedNow
		}
		if v.Status != proposal.StatusActive {
			continue
		}
		cands = append(cands, selection.Candidate{
			ProposalID: v.ID, PriceMinor: v.TotalPrice.Minor, Reputation: v.Seller.Mean, Adherence: *v.Adherence,
			Compliant: *v.Compliant, LeadTimeDays: v.LeadTimeDays, SubmittedAt: v.SubmittedAt,
		})
	}
	return selection.Rank(cands, w, method)
}

// CloseIntent ends the bidding window under the database clock. It is what
// the worker runs when a deadline fires; running it twice is harmless.
func (m *Market) CloseIntent(ctx context.Context, intentID string) (intent.Intent, error) {
	var out intent.Intent
	err := m.Store.Tx(ctx, func(ctx context.Context, tx Tx) error {
		in, err := tx.Intent(ctx, intentID)
		if err != nil {
			return err
		}
		if in.Status != intent.StatusOpen {
			out = in
			return nil
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		next, evs, err := in.Close(now)
		if err != nil {
			return err
		}
		if err := tx.SaveIntent(ctx, next, in.Version, GuardDeadlinePassed); err != nil {
			return err
		}
		out = next
		return tx.Outbox(ctx, m.stamp(ctx, evs))
	})
	if err == nil && out.Status == intent.StatusClosed {
		m.schedule(ctx, "intent-expire", out.ID, out.SelectBy)
	}
	return out, err
}

// ExpireIntent ends the selection window without a choice; active proposals lapse.
func (m *Market) ExpireIntent(ctx context.Context, intentID string) (intent.Intent, error) {
	var out intent.Intent
	err := m.Store.Tx(ctx, func(ctx context.Context, tx Tx) error {
		in, err := tx.Intent(ctx, intentID)
		if err != nil {
			return err
		}
		if in.Status != intent.StatusClosed {
			out = in
			return nil
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		next, evs, err := in.Expire(now)
		if err != nil {
			return err
		}
		if err := tx.SaveIntent(ctx, next, in.Version, GuardSelectionOver); err != nil {
			return err
		}
		ps, err := tx.Proposals(ctx, intentID)
		if err != nil {
			return err
		}
		for _, p := range ps {
			if p.Status == proposal.StatusActive {
				lapsed, err := p.Lapse()
				if err != nil {
					return err
				}
				if err := tx.SaveProposal(ctx, lapsed, p.Version); err != nil {
					return err
				}
			}
		}
		out = next
		return tx.Outbox(ctx, m.stamp(ctx, evs))
	})
	return out, err
}

// Award records the buyer's choice and forms the agreement in one
// transaction: the intent's version check and the unique index on the
// agreement's intent make a second award impossible.
func (m *Market) Award(ctx context.Context, buyerID, intentID, proposalID string) (agreement.Agreement, error) {
	var out agreement.Agreement
	err := m.Store.Tx(ctx, func(ctx context.Context, tx Tx) error {
		in, err := tx.Intent(ctx, intentID)
		if err != nil {
			return err
		}
		ps, err := tx.Proposals(ctx, intentID)
		if err != nil {
			return err
		}
		var chosen *proposal.Proposal
		for i := range ps {
			if ps[i].ID == proposalID {
				chosen = &ps[i]
			}
		}
		if chosen == nil {
			return ErrNotFound
		}
		price, err := proposal.Reveal(in, *chosen, m.Sealer)
		if err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		awarded, evs, err := in.Award(buyerID, chosen.ID, chosen.SellerID, now)
		if err != nil {
			return err
		}
		for _, p := range ps {
			var next proposal.Proposal
			switch {
			case p.ID == chosen.ID:
				next, err = p.Select()
			case p.Status == proposal.StatusActive:
				next, err = p.Reject()
			default:
				continue
			}
			if err != nil {
				return err
			}
			if err := tx.SaveProposal(ctx, next, p.Version); err != nil {
				return err
			}
			if p.ID == chosen.ID {
				*chosen = next
			}
		}
		a, aevs, err := agreement.Form(m.IDs.New(), awarded, *chosen, price, m.Tree, m.Settlement, now)
		if err != nil {
			return err
		}
		if err := tx.SaveIntent(ctx, awarded, in.Version, GuardNone); err != nil {
			return err
		}
		if err := tx.InsertAgreement(ctx, a); err != nil {
			return err
		}
		out = a
		return tx.Outbox(ctx, m.stamp(ctx, append(evs, aevs...)))
	})
	if err == nil {
		m.schedule(ctx, "agreement-funding", out.ID, out.FundBy)
	}
	return out, err
}

// Agreement returns an agreement to one of its parties or an arbiter.
func (m *Market) Agreement(ctx context.Context, viewerID, id string) (agreement.Agreement, error) {
	var a agreement.Agreement
	err := m.Store.View(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		if a, err = tx.Agreement(ctx, id); err != nil {
			return err
		}
		if viewerID == a.Terms.BuyerID || viewerID == a.Terms.SellerID {
			return nil
		}
		p, err := tx.Party(ctx, viewerID)
		if err != nil || !p.Arbiter {
			return ErrForbidden
		}
		return nil
	})
	return a, err
}

// mutateAgreement runs one agreement transition in a transaction.
func (m *Market) mutateAgreement(ctx context.Context, id string, fn func(a agreement.Agreement, now time.Time, tx Tx) (agreement.Agreement, []event.Event, error)) (agreement.Agreement, error) {
	var out agreement.Agreement
	err := m.Store.Tx(ctx, func(ctx context.Context, tx Tx) error {
		a, err := tx.Agreement(ctx, id)
		if err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		next, evs, err := fn(a, now, tx)
		if err != nil {
			return err
		}
		out = next
		if next.Version == a.Version {
			return nil // idempotent no-op
		}
		if err := tx.SaveAgreement(ctx, next, a.Version); err != nil {
			return err
		}
		return tx.Outbox(ctx, m.stamp(ctx, evs))
	})
	return out, err
}

// Ship records dispatch by the seller.
func (m *Market) Ship(ctx context.Context, sellerID, id, tracking string) (agreement.Agreement, error) {
	return m.mutateAgreement(ctx, id, func(a agreement.Agreement, now time.Time, _ Tx) (agreement.Agreement, []event.Event, error) {
		return a.Ship(sellerID, tracking, now)
	})
}

// Deliver records arrival, confirmed by the buyer.
func (m *Market) Deliver(ctx context.Context, buyerID, id string) (agreement.Agreement, error) {
	a, err := m.mutateAgreement(ctx, id, func(a agreement.Agreement, now time.Time, _ Tx) (agreement.Agreement, []event.Event, error) {
		return a.Deliver(buyerID, m.Settlement, now)
	})
	if err == nil && a.InspectUntil != nil {
		m.schedule(ctx, "agreement-inspection", a.ID, *a.InspectUntil)
	}
	return a, err
}

// Accept asks the escrow agent to release the money to the seller.
func (m *Market) Accept(ctx context.Context, buyerID, id string) (agreement.Agreement, error) {
	return m.mutateAgreement(ctx, id, func(a agreement.Agreement, now time.Time, _ Tx) (agreement.Agreement, []event.Event, error) {
		return a.Accept(buyerID, now)
	})
}

// RequestRefund asks for the money back when the seller misses the shipping deadline.
func (m *Market) RequestRefund(ctx context.Context, buyerID, id string) (agreement.Agreement, error) {
	return m.mutateAgreement(ctx, id, func(a agreement.Agreement, now time.Time, _ Tx) (agreement.Agreement, []event.Event, error) {
		return a.RequestRefund(buyerID, now)
	})
}

// OpenDispute starts a field conference.
func (m *Market) OpenDispute(ctx context.Context, buyerID, id string, claims []agreement.ClaimedField) (agreement.Agreement, error) {
	return m.mutateAgreement(ctx, id, func(a agreement.Agreement, now time.Time, _ Tx) (agreement.Agreement, []event.Event, error) {
		return a.OpenDispute(buyerID, claims, m.Tree, now)
	})
}

// Resolve applies the arbitration rule to the fields an arbiter confirmed.
func (m *Market) Resolve(ctx context.Context, arbiterID, id string, confirmed []string) (agreement.Agreement, error) {
	return m.mutateAgreement(ctx, id, func(a agreement.Agreement, now time.Time, tx Tx) (agreement.Agreement, []event.Event, error) {
		p, err := tx.Party(ctx, arbiterID)
		if err != nil || !p.Arbiter {
			return a, nil, ErrForbidden
		}
		if a.Dispute == nil {
			return a, nil, agreement.ErrNoDiscrepancy
		}
		return a.Resolve(agreement.Decide(*a.Dispute, confirmed), now)
	})
}

// Settle records the escrow agent's confirmation (from its webhook or from
// reconciliation). Duplicates are no-ops.
func (m *Market) Settle(ctx context.Context, id string, released bool) (agreement.Agreement, error) {
	a, err := m.mutateAgreement(ctx, id, func(a agreement.Agreement, now time.Time, _ Tx) (agreement.Agreement, []event.Event, error) {
		return a.Settled(released, now)
	})
	return a, err
}

// Funded records that the escrow agent holds the money.
func (m *Market) Funded(ctx context.Context, id, escrowID string) (agreement.Agreement, error) {
	return m.mutateAgreement(ctx, id, func(a agreement.Agreement, now time.Time, _ Tx) (agreement.Agreement, []event.Event, error) {
		if a.Status != agreement.StatusAwaitingFunding && a.EscrowID == escrowID {
			return a, nil, nil // duplicate confirmation
		}
		return a.Funded(escrowID, now)
	})
}

// Rate stores a rating, sealed until both parties rated or the window ends.
func (m *Market) Rate(ctx context.Context, raterID, agreementID string, stars int) error {
	return m.Store.Tx(ctx, func(ctx context.Context, tx Tx) error {
		a, err := tx.Agreement(ctx, agreementID)
		if err != nil {
			return err
		}
		if a.Status != agreement.StatusReleased && a.Status != agreement.StatusRefunded {
			return fmt.Errorf("%w: avaliação só depois da liquidação", ErrForbidden)
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		pair, err := tx.RatingPair(ctx, agreementID)
		if errors.Is(err, ErrNotFound) {
			pair = reputation.Pair{
				AgreementID: a.ID, BuyerID: a.Terms.BuyerID, SellerID: a.Terms.SellerID,
				WindowEnds: now.AddDate(0, 0, m.RatingDays),
			}
		} else if err != nil {
			return err
		}
		next, err := pair.Submit(raterID, stars, now)
		if err != nil {
			return err
		}
		if err := tx.SaveRatingPair(ctx, next); err != nil {
			return err
		}
		evs := []event.Event{event.New(reputation.EvRatingSubmitted, a.ID, now, map[string]any{"agreement_id": a.ID, "rater_id": raterID})}
		if rs, ok := next.Revealed(now); ok {
			if err := tx.InsertRatings(ctx, rs); err != nil {
				return err
			}
			evs = append(evs, event.New(reputation.EvRatingsRevealed, a.ID, now, map[string]any{"agreement_id": a.ID, "count": len(rs)}))
		}
		return tx.Outbox(ctx, m.stamp(ctx, evs))
	})
}

// Reputation is a party's posterior in both roles.
func (m *Market) Reputation(ctx context.Context, partyID string) (map[reputation.Role]reputation.Score, error) {
	out := map[reputation.Role]reputation.Score{}
	err := m.Store.View(ctx, func(ctx context.Context, tx Tx) error {
		for _, role := range []reputation.Role{reputation.AsSeller, reputation.AsBuyer} {
			rs, err := tx.Ratings(ctx, partyID, role)
			if err != nil {
				return err
			}
			out[role] = reputation.Estimate(rs, reputation.DefaultPrior, reputation.DefaultHalfLife, m.Clock.Now())
		}
		return nil
	})
	return out, err
}
