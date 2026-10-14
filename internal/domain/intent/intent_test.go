package intent

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/domain/fsm"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso8601"
)

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func publish(t *testing.T) Intent {
	t.Helper()
	in, evs, err := Publish(PublishCommand{
		ID: "int-1", BuyerID: "buyer-1", CategoryID: "collectibles.tcg", Title: "Charizard base set PT",
		Constraints: map[string]any{"game": "pokemon", "grade": map[string]any{"min": 9.0, "required": true}},
		DeliveryCEP: "01310-100", Window: iso8601.MustParse("P2D"),
	}, taxonomy.Default, DefaultPolicy, t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != EvPublished {
		t.Fatalf("events %v", evs)
	}
	return in
}

func TestPublish(t *testing.T) {
	in := publish(t)
	if in.Status != StatusOpen || in.DeliveryUF != "SP" || !in.ClosesAt.Equal(t0.Add(48*time.Hour)) ||
		!in.SelectBy.Equal(t0.Add(5*24*time.Hour)) || len(in.Predicates) != 2 {
		t.Fatalf("%+v", in)
	}
}

func TestPublishRejects(t *testing.T) {
	base := PublishCommand{
		ID: "x", BuyerID: "b", CategoryID: "collectibles.tcg", Title: "Um título ok",
		Constraints: map[string]any{}, DeliveryCEP: "01310-100", Window: iso8601.MustParse("P1D"),
	}
	cases := map[string]struct {
		mutate func(*PublishCommand)
		want   error
	}{
		"forbidden category": {func(c *PublishCommand) { c.CategoryID = "collectibles.firearms" }, taxonomy.ErrUnknownCategory},
		"wave not launched":  {func(c *PublishCommand) { c.CategoryID = "auto-parts.body-panels" }, taxonomy.ErrNotLaunched},
		"window too short":   {func(c *PublishCommand) { c.Window = iso8601.MustParse("PT10M") }, ErrWindow},
		"window too long":    {func(c *PublishCommand) { c.Window = iso8601.MustParse("P30D") }, ErrWindow},
		"unassigned CEP":     {func(c *PublishCommand) { c.DeliveryCEP = "78900-000" }, ErrDelivery},
		"short title":        {func(c *PublishCommand) { c.Title = "oi" }, ErrTitle},
	}
	for name, tc := range cases {
		cmd := base
		tc.mutate(&cmd)
		if _, _, err := Publish(cmd, taxonomy.Default, DefaultPolicy, t0); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
}

// A late timer never makes a read lie: an open intent past its deadline
// reads as closed, and past the selection window as expired.
func TestEffectiveStatusIsAFunctionOfTime(t *testing.T) {
	in := publish(t)
	cases := []struct {
		at   time.Time
		want Status
	}{
		{in.ClosesAt.Add(-time.Nanosecond), StatusOpen},
		{in.ClosesAt, StatusClosed},
		{in.SelectBy, StatusExpired},
	}
	for _, c := range cases {
		if got := in.EffectiveStatus(c.at); got != c.want {
			t.Errorf("at %v: %s, want %s", c.at, got, c.want)
		}
	}
}

// The window is half-open: at the closing instant no proposal is accepted.
func TestNoProposalAtOrAfterTheDeadline(t *testing.T) {
	in := publish(t)
	if err := in.AcceptsProposals(in.ClosesAt.Add(-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if err := in.AcceptsProposals(in.ClosesAt); !errors.Is(err, ErrNotOpen) {
		t.Fatal("deadline instant must refuse")
	}
	closed, _, err := in.Close(in.ClosesAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closed.CountProposal(in.ClosesAt.Add(-time.Hour)); !errors.Is(err, ErrNotOpen) {
		t.Fatal("a closed intent refuses proposals whatever clock the caller has")
	}
}

func TestLifecycle(t *testing.T) {
	in := publish(t)
	if _, _, err := in.Close(in.ClosesAt.Add(-time.Second)); !errors.Is(err, ErrTooEarly) {
		t.Fatal("close before the deadline")
	}
	closed, _, _ := in.Close(in.ClosesAt)
	if _, _, err := closed.Award("intruder", "p1", "s1", in.ClosesAt); !errors.Is(err, ErrNotBuyer) {
		t.Fatal("only the buyer awards")
	}
	awarded, evs, err := closed.Award("buyer-1", "p1", "s1", in.ClosesAt.Add(time.Hour))
	if err != nil || awarded.Status != StatusAwarded || awarded.AwardedProposal != "p1" || evs[0].Type != EvAwarded {
		t.Fatalf("%v %+v", err, awarded)
	}
	if _, _, err := awarded.Award("buyer-1", "p2", "s2", in.ClosesAt.Add(time.Hour)); !errors.Is(err, fsm.ErrTransition) {
		t.Fatal("an awarded intent cannot be awarded again")
	}
	if _, _, err := closed.Award("buyer-1", "p1", "s1", in.SelectBy); !errors.Is(err, ErrTooLate) {
		t.Fatal("award after the selection window")
	}
	expired, _, err := closed.Expire(in.SelectBy)
	if err != nil || expired.Status != StatusExpired {
		t.Fatal(err)
	}
}

func TestMachineMatchesTheOntologyPhases(t *testing.T) {
	want := []Status{StatusAwarded, StatusCancelled, StatusClosed, StatusExpired, StatusOpen}
	if !slices.Equal(Machine.States(), want) || !slices.Equal(Machine.Reachable(), want) {
		t.Fatalf("states %v", Machine.States())
	}
	for _, s := range []Status{StatusAwarded, StatusExpired, StatusCancelled} {
		if !Machine.Terminal(s) {
			t.Errorf("%s should be terminal", s)
		}
	}
}
