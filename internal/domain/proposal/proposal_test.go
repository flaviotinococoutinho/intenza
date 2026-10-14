package proposal

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/money"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso4217"
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

func sealer(t testing.TB) *Sealer {
	t.Helper()
	s, err := NewSealer(map[int][]byte{1: bytes.Repeat([]byte{7}, 32)}, 1, entropy{rand.New(rand.NewPCG(1, 1))})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func openIntent(t testing.TB) intent.Intent {
	t.Helper()
	in, _, err := intent.Publish(intent.PublishCommand{
		ID: "int-1", BuyerID: "buyer-1", CategoryID: "collectibles.tcg", Title: "Charizard base set",
		Constraints: map[string]any{"game": "pokemon"}, DeliveryCEP: "20040-020", Window: iso8601.MustParse("P2D"),
	}, taxonomy.Default, intent.DefaultPolicy, t0)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

var attrs = map[string]any{"game": "pokemon", "set_code": "BS1", "card_number": "4", "language": "pt", "condition": "NM", "grading_company": "none"}

func submit(t testing.TB, in intent.Intent, s *Sealer, id, seller string, price int64) Proposal {
	t.Helper()
	p, _, err := Submit(SubmitCommand{
		ID: id, SellerID: seller, Credentials: []taxonomy.Credential{taxonomy.KYCBasic}, Attributes: attrs,
		Price: money.BRL(price), LeadTimeDays: 5, ValidUntil: in.SelectBy.Add(24 * time.Hour),
	}, in, taxonomy.Default, s, t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// No event about a proposal carries its price, in any field, at any depth.
func TestEventsCarryNoPrice(t *testing.T) {
	in, s := openIntent(t), sealer(t)
	p, evs, err := Submit(SubmitCommand{
		ID: "p1", SellerID: "s1", Credentials: []taxonomy.Credential{taxonomy.KYCBasic},
		Attributes: attrs, Price: money.BRL(123456), LeadTimeDays: 5, ValidUntil: in.SelectBy,
	}, in, taxonomy.Default, s, t0)
	if err != nil {
		t.Fatal(err)
	}
	rev, revEvs, _ := p.Revise(SubmitCommand{
		ID: "p1", SellerID: "s1", Credentials: []taxonomy.Credential{taxonomy.KYCBasic},
		Attributes: attrs, Price: money.BRL(99999), LeadTimeDays: 4, ValidUntil: in.SelectBy,
	}, in, taxonomy.Default, s, t0)
	_, wEvs, _ := rev.Withdraw("s1", in, t0)
	for _, ev := range append(append(evs, revEvs...), wEvs...) {
		raw, _ := json.Marshal(ev.Data)
		low := strings.ToLower(string(raw))
		for _, leak := range []string{"price", "amount", "1234.56", "123456", "99999", "ciphertext"} {
			if strings.Contains(low, leak) {
				t.Fatalf("%s leaks %q: %s", ev.Type, leak, raw)
			}
		}
	}
}

// The persisted phase gates the reveal, not the caller's clock: an open intent
// whose deadline has passed on this node still keeps prices sealed.
func TestRevealIgnoresTheLocalClock(t *testing.T) {
	in, s := openIntent(t), sealer(t)
	p := submit(t, in, s, "p1", "s1", 150000)
	skewed := in.ClosesAt.Add(time.Hour)
	if in.EffectiveStatus(skewed) != intent.StatusClosed {
		t.Fatal("precondition: the skewed node displays closed")
	}
	if _, err := Reveal(in, p, s); !errors.Is(err, ErrSealedNow) {
		t.Fatalf("revealed before the persisted close: %v", err)
	}
	closed, _, _ := in.Close(in.ClosesAt)
	price, err := Reveal(closed, p, s)
	if err != nil || price != money.BRL(150000) {
		t.Fatalf("%v %v", price, err)
	}
}

// AES-GCM with associated data binds a ciphertext to its proposal: moving it
// to another proposal, seller or revision makes it unreadable.
func TestSealedPriceIsBoundToItsProposal(t *testing.T) {
	in, s := openIntent(t), sealer(t)
	a := submit(t, in, s, "p-a", "s-a", 100000)
	b := submit(t, in, s, "p-b", "s-b", 200000)
	b.Price = a.Price
	closed, _, _ := in.Close(in.ClosesAt)
	if _, err := Reveal(closed, b, s); !errors.Is(err, ErrSealOpen) {
		t.Fatal("a ciphertext swapped between proposals must not open")
	}
	a.Revision++
	if _, err := Reveal(closed, a, s); !errors.Is(err, ErrSealOpen) {
		t.Fatal("a ciphertext must not open under another revision")
	}
	if bytes.Contains(a.Price.Ciphertext, []byte{0x01, 0x86, 0xA0}) {
		t.Fatal("plaintext amount visible in the ciphertext")
	}
}

func TestKeyRotationKeepsOldPricesReadable(t *testing.T) {
	in := openIntent(t)
	old := sealer(t)
	p := submit(t, in, old, "p1", "s1", 4200)
	rotated, err := NewSealer(map[int][]byte{1: bytes.Repeat([]byte{7}, 32), 2: bytes.Repeat([]byte{9}, 32)}, 2, entropy{rand.New(rand.NewPCG(2, 2))})
	if err != nil {
		t.Fatal(err)
	}
	closed, _, _ := in.Close(in.ClosesAt)
	if price, err := Reveal(closed, p, rotated); err != nil || price.Minor != 4200 {
		t.Fatalf("old key version: %v %v", price, err)
	}
	if q := submit(t, in, rotated, "p2", "s2", 1); q.Price.KeyVersion != 2 {
		t.Fatal("new seals use the current version")
	}
}

func TestSubmitRejects(t *testing.T) {
	in, s := openIntent(t), sealer(t)
	ok := SubmitCommand{
		ID: "p", SellerID: "s", Credentials: []taxonomy.Credential{taxonomy.KYCBasic}, Attributes: attrs,
		Price: money.BRL(100), LeadTimeDays: 3, ValidUntil: in.SelectBy,
	}
	cases := map[string]struct {
		mutate func(*SubmitCommand)
		at     time.Time
		want   error
	}{
		"self dealing":     {func(c *SubmitCommand) { c.SellerID = "buyer-1" }, t0, ErrSelfDealing},
		"no credentials":   {func(c *SubmitCommand) { c.Credentials = nil }, t0, ErrIneligible},
		"zero price":       {func(c *SubmitCommand) { c.Price = money.BRL(0) }, t0, ErrPrice},
		"dollars":          {func(c *SubmitCommand) { c.Price = money.Money{Minor: 1, Currency: iso4217.USD} }, t0, ErrPrice},
		"lead time":        {func(c *SubmitCommand) { c.LeadTimeDays = 90 }, t0, ErrLeadTime},
		"short validity":   {func(c *SubmitCommand) { c.ValidUntil = in.ClosesAt }, t0, ErrValidity},
		"after the window": {func(*SubmitCommand) {}, in.ClosesAt, intent.ErrNotOpen},
	}
	for name, tc := range cases {
		cmd := ok
		tc.mutate(&cmd)
		if _, _, err := Submit(cmd, in, taxonomy.Default, s, tc.at); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
}

func TestOwnerReadsOwnPriceAnytime(t *testing.T) {
	in, s := openIntent(t), sealer(t)
	p := submit(t, in, s, "p1", "s1", 777)
	if m, err := OwnPrice("s1", p, s); err != nil || m.Minor != 777 {
		t.Fatalf("%v %v", m, err)
	}
	if _, err := OwnPrice("s2", p, s); !errors.Is(err, ErrNotOwner) {
		t.Fatal("competitors never read it")
	}
}

func TestMachineMatchesTheOntologyPhases(t *testing.T) {
	want := []Status{StatusActive, StatusLapsed, StatusRejected, StatusSelected, StatusWithdrawn}
	got := Machine.States()
	if len(got) != len(want) {
		t.Fatalf("states %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("states %v, want %v", got, want)
		}
	}
	for _, s := range []Status{StatusWithdrawn, StatusSelected, StatusRejected, StatusLapsed} {
		if !Machine.Terminal(s) {
			t.Errorf("%s should be terminal", s)
		}
	}
}
