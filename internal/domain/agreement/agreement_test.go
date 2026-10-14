package agreement

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/domain/fsm"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/money"
	"github.com/flaviotinococoutinho/intenza/internal/domain/proposal"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso8601"
)

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

type entropy struct{ r *rand.Rand }

func (e entropy) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(e.r.Uint32())
	}
	return len(p), nil
}

// formed walks the whole demand side: publish, submit, close, award, reveal, form.
func formed(t *testing.T) Agreement {
	t.Helper()
	in, _, err := intent.Publish(intent.PublishCommand{
		ID: "int-1", BuyerID: "buyer", CategoryID: "collectibles.tcg", Title: "Charizard base set",
		Constraints: map[string]any{"game": "pokemon"}, DeliveryCEP: "01310-100", Window: iso8601.MustParse("P1D"),
	}, taxonomy.Default, intent.DefaultPolicy, t0)
	if err != nil {
		t.Fatal(err)
	}
	sealer, _ := proposal.NewSealer(map[int][]byte{1: bytes.Repeat([]byte{3}, 32)}, 1, entropy{rand.New(rand.NewPCG(5, 5))})
	p, _, err := proposal.Submit(proposal.SubmitCommand{
		ID: "prop-1", SellerID: "seller", Credentials: []taxonomy.Credential{taxonomy.KYCBasic},
		Attributes: map[string]any{
			"game": "pokemon", "set_code": "BS1", "card_number": "4", "language": "pt",
			"condition": "NM", "grading_company": "PSA", "grade": "9.0", "first_edition": true,
		},
		Price: money.BRL(250000), LeadTimeDays: 3, ValidUntil: in.SelectBy,
	}, in, taxonomy.Default, sealer, t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	in, _ = in.CountProposal(t0.Add(time.Hour))
	in, _, _ = in.Close(in.ClosesAt)
	price, err := proposal.Reveal(in, p, sealer)
	if err != nil {
		t.Fatal(err)
	}
	in, _, _ = in.Award("buyer", p.ID, p.SellerID, in.ClosesAt.Add(time.Hour))
	p, _ = p.Select()
	a, evs, err := Form("agr-1", in, p, price, taxonomy.Default, DefaultPolicy, in.ClosesAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != EvFormed || a.Status != StatusAwaitingFunding {
		t.Fatalf("%v %+v", evs, a)
	}
	return a
}

func delivered(t *testing.T) Agreement {
	t.Helper()
	a := formed(t)
	a, _, _ = a.Funded("esc-1", t0.Add(30*time.Hour))
	a, _, _ = a.Ship("seller", "BR123456789", t0.Add(40*time.Hour))
	a, _, err := a.Deliver("buyer", DefaultPolicy, t0.Add(60*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestTermsHashDetectsTampering(t *testing.T) {
	a := formed(t)
	if err := a.VerifyTerms(); err != nil {
		t.Fatal(err)
	}
	if len(a.TermsHash) != 64 {
		t.Fatalf("hash %q", a.TermsHash)
	}
	tampered := a
	tampered.Terms.Attributes = map[string]string{}
	for k, v := range a.Terms.Attributes {
		tampered.Terms.Attributes[k] = v
	}
	tampered.Terms.Attributes["grade"] = "10.0"
	if err := tampered.VerifyTerms(); !errors.Is(err, ErrTampered) {
		t.Fatal("a changed grade must change the hash")
	}
	again := formed(t)
	if again.TermsHash != a.TermsHash {
		t.Fatal("the same deal must hash the same: canonical JSON is deterministic")
	}
}

func TestHappyPathReleases(t *testing.T) {
	a := delivered(t)
	a, evs, err := a.Accept("buyer", t0.Add(61*time.Hour))
	if err != nil || a.Status != StatusReleasing || evs[0].Type != EvReleaseRequested {
		t.Fatalf("%v %s", err, a.Status)
	}
	key := evs[0].Data.(map[string]any)["idempotency_key"]
	if key != "release:agr-1" {
		t.Fatalf("idempotency key %v", key)
	}
	a, _, err = a.Settled(true, t0.Add(62*time.Hour))
	if err != nil || a.Status != StatusReleased {
		t.Fatalf("%v %s", err, a.Status)
	}
}

// A duplicated confirmation is a no-op; a contradicting one is refused; a
// confirmation that arrives before its request does not apply.
func TestSettledIsIdempotentAndOrderAware(t *testing.T) {
	a := delivered(t)
	if _, _, err := a.Settled(true, t0); !errors.Is(err, fsm.ErrTransition) {
		t.Fatalf("release confirmation before the request: %v", err)
	}
	a, _, _ = a.Accept("buyer", t0.Add(61*time.Hour))
	a, _, _ = a.Settled(true, t0.Add(62*time.Hour))
	same, evs, err := a.Settled(true, t0.Add(63*time.Hour))
	if err != nil || len(evs) != 0 || same.Version != a.Version {
		t.Fatalf("duplicate webhook changed state: %v %v", evs, err)
	}
	if _, _, err := a.Settled(false, t0.Add(64*time.Hour)); !errors.Is(err, ErrConflict) {
		t.Fatalf("refund after release: %v", err)
	}
}

// Proof by exhaustion on the saga graph: once a release is requested no path
// reaches refunded, and vice versa.
func TestSettlementOutcomesAreExclusive(t *testing.T) {
	reach := func(from Status) map[Status]bool {
		seen := map[Status]bool{from: true}
		queue := []Status{from}
		triggers := []Trigger{
			TriggerFund, TriggerCancel, TriggerShip, TriggerDeliver, TriggerAccept, TriggerDispute,
			TriggerRuleRelease, TriggerRuleRefund, TriggerRequestRefund, TriggerReleased, TriggerRefunded,
		}
		for len(queue) > 0 {
			s := queue[0]
			queue = queue[1:]
			for _, tr := range triggers {
				if to, err := Machine.Next(s, tr); err == nil && !seen[to] {
					seen[to] = true
					queue = append(queue, to)
				}
			}
		}
		return seen
	}
	if reach(StatusReleasing)[StatusRefunded] || reach(StatusRefunding)[StatusReleased] {
		t.Fatal("release and refund must be exclusive")
	}
	for _, s := range []Status{StatusReleased, StatusRefunded, StatusCancelled} {
		if !Machine.Terminal(s) {
			t.Errorf("%s should be terminal", s)
		}
	}
	if len(Machine.States()) != 10 || len(Machine.Reachable()) != 10 {
		t.Fatalf("states %v", Machine.States())
	}
}

func TestDisputeNeedsADiscrepancyOnAFixedField(t *testing.T) {
	a := delivered(t)
	at := t0.Add(70 * time.Hour)
	if _, _, err := a.OpenDispute("buyer", []ClaimedField{{Field: "condition", Observed: "NM"}}, taxonomy.Default, at); !errors.Is(err, ErrNoDiscrepancy) {
		t.Fatalf("claim that agrees with the terms: %v", err)
	}
	if _, _, err := a.OpenDispute("buyer", []ClaimedField{{Field: "finish", Observed: "holo"}}, taxonomy.Default, at); err == nil {
		t.Fatal("finish was not fixed by the terms")
	}
	d, evs, err := a.OpenDispute("buyer", []ClaimedField{{Field: "grade", Observed: 7.5}, {Field: "first_edition", Observed: true}}, taxonomy.Default, at)
	if err != nil || d.Status != StatusDisputed || len(d.Dispute.Discrepancies) != 1 || evs[0].Type != EvDisputeOpened {
		t.Fatalf("%v %+v", err, d.Dispute)
	}
	if got := d.Dispute.Discrepancies[0]; got.Field != "grade" || got.Promised != "9.0" || got.Observed != "7.5" {
		t.Fatalf("%+v", got)
	}
	if v := Decide(*d.Dispute, []string{"grade"}); v != VerdictRefund {
		t.Fatal("confirmed discrepancy means refund")
	}
	if v := Decide(*d.Dispute, nil); v != VerdictRelease {
		t.Fatal("unconfirmed claims pay the seller")
	}
	r, evs, err := d.Resolve(VerdictRefund, at.Add(time.Hour))
	if err != nil || r.Status != StatusRefunding || len(evs) != 2 || evs[1].Type != EvRefundRequested {
		t.Fatalf("%v %s %v", err, r.Status, evs)
	}
	late := t0.Add(60*time.Hour + 8*24*time.Hour)
	if _, _, err := a.OpenDispute("buyer", []ClaimedField{{Field: "grade", Observed: 7.5}}, taxonomy.Default, late); !errors.Is(err, ErrTooLate) {
		t.Fatal("dispute after the inspection window")
	}
}

func TestDeadlines(t *testing.T) {
	a := formed(t)
	if _, _, err := a.Cancel(a.FundBy.Add(-time.Second)); !errors.Is(err, ErrTooEarly) {
		t.Fatal("cancel before the funding deadline")
	}
	if c, _, err := a.Cancel(a.FundBy); err != nil || c.Status != StatusCancelled {
		t.Fatal(err)
	}
	f, _, _ := a.Funded("esc", t0)
	if _, _, err := f.RequestRefund("buyer", f.ShipBy.Add(-time.Second)); !errors.Is(err, ErrTooEarly) {
		t.Fatal("refund before the shipping deadline")
	}
	if r, _, err := f.RequestRefund("buyer", *f.ShipBy); err != nil || r.Status != StatusRefunding {
		t.Fatal(err)
	}
	d := delivered(t)
	if _, _, err := d.AutoAccept(d.InspectUntil.Add(-time.Second)); !errors.Is(err, ErrTooEarly) {
		t.Fatal("auto-accept before the inspection window ends")
	}
	if x, _, err := d.AutoAccept(*d.InspectUntil); err != nil || x.Status != StatusReleasing {
		t.Fatal(err)
	}
	if _, _, err := d.Ship("intruder", "x", t0); !errors.Is(err, ErrNotParty) {
		t.Fatal("only the seller ships")
	}
}

func TestAttachEscrowOnceAndConsistently(t *testing.T) {
	a := formed(t)
	b, err := a.AttachEscrow("esc-9", "000201...")
	if err != nil || b.EscrowID != "esc-9" || b.Version != a.Version+1 {
		t.Fatalf("%v %+v", err, b)
	}
	if same, err := b.AttachEscrow("esc-9", "000201..."); err != nil || same.Version != b.Version {
		t.Fatal("attaching the same escrow is a no-op")
	}
	if _, err := b.AttachEscrow("esc-other", ""); !errors.Is(err, ErrConflict) {
		t.Fatal("a second escrow for the same agreement is a conflict")
	}
	if _, _, err := b.Funded("esc-other", t0); !errors.Is(err, ErrConflict) {
		t.Fatal("funding confirmation for another escrow")
	}
}
