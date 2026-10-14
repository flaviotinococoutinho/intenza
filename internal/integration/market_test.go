//go:build integration

package integration

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/money"
)

var tcgItem = map[string]any{"game": "pokemon", "set_code": "BS1", "card_number": "4", "language": "pt", "condition": "NM", "grading_company": "PSA", "grade": "9.5"}

func publishBody(window string) map[string]any {
	return map[string]any{
		"category_id": "collectibles.tcg", "title": "Charizard base set em português",
		"constraints":  map[string]any{"game": "pokemon", "grade": map[string]any{"min": 9.0, "required": true}},
		"delivery_cep": "01310-100", "window": window,
	}
}

func proposalBody(validUntil time.Time, price string, lead int) map[string]any {
	return map[string]any{
		"attributes": tcgItem, "price": map[string]any{"amount": price, "currency": "BRL"},
		"lead_time_days": lead, "valid_until": validUntil.Format(time.RFC3339),
	}
}

type intentResp struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"`
	ClosesAt        time.Time `json:"closes_at"`
	SelectBy        time.Time `json:"select_by"`
	EffectiveStatus string    `json:"effective_status"`
}

func waitUntil(t time.Time) { time.Sleep(time.Until(t) + 150*time.Millisecond) }

// The whole loop over HTTP: idempotent publish, sealed proposals, close by the
// database clock, ranking, award, settlement and double-blind ratings.
func TestEndToEndMarket(t *testing.T) {
	e := newEnv(t, true)
	buyer := e.register(0)
	sellers := []party{e.register(1), e.register(2), e.register(3)}

	var in intentResp
	if code := e.call(http.MethodPost, "/v1/intents", buyer.Token, publishBody("PT3S"), &in, "Idempotency-Key", "abc-1"); code != http.StatusCreated {
		t.Fatalf("publish: %d", code)
	}
	var again intentResp
	if code := e.call(http.MethodPost, "/v1/intents", buyer.Token, publishBody("PT3S"), &again, "Idempotency-Key", "abc-1"); code != http.StatusOK || again.ID != in.ID {
		t.Fatalf("idempotent publish: %d %s vs %s", code, again.ID, in.ID)
	}

	prices := []string{"1500.00", "1200.00", "1350.00"}
	for i, s := range sellers {
		if code := e.call(http.MethodPost, "/v1/intents/"+in.ID+"/proposals", s.Token, proposalBody(in.SelectBy.Add(time.Hour), prices[i], 3+i), nil); code != http.StatusCreated {
			t.Fatalf("proposal %d: %d", i, code)
		}
	}
	var sealed []map[string]any
	e.call(http.MethodGet, "/v1/intents/"+in.ID+"/proposals", buyer.Token, nil, &sealed)
	if len(sealed) != 3 {
		t.Fatalf("buyer sees %d proposals", len(sealed))
	}
	for _, p := range sealed {
		if _, ok := p["total_price"]; ok {
			t.Fatal("price visible before the close")
		}
	}
	var own []map[string]any
	e.call(http.MethodGet, "/v1/intents/"+in.ID+"/proposals", sellers[0].Token, nil, &own)
	if len(own) != 1 || own[0]["total_price"] == nil {
		t.Fatalf("seller sees its own price only: %v", own)
	}

	waitUntil(in.ClosesAt)
	late := e.register(4)
	if code := e.call(http.MethodPost, "/v1/intents/"+in.ID+"/proposals", late.Token, proposalBody(in.SelectBy.Add(time.Hour), "900.00", 2), nil); code != http.StatusConflict {
		t.Fatalf("late proposal: %d", code)
	}
	if _, err := e.market.CloseIntent(context.Background(), in.ID); err != nil {
		t.Fatal(err)
	}
	var ranking []struct {
		ProposalID string  `json:"proposal_id"`
		Rank       int     `json:"rank"`
		Score      float64 `json:"score"`
	}
	if code := e.call(http.MethodGet, "/v1/intents/"+in.ID+"/ranking", buyer.Token, nil, &ranking); code != http.StatusOK || len(ranking) != 3 {
		t.Fatalf("ranking: %d %v", code, ranking)
	}
	var agr struct {
		ID        string `json:"id"`
		TermsHash string `json:"terms_hash"`
		Status    string `json:"status"`
	}
	if code := e.call(http.MethodPost, "/v1/intents/"+in.ID+"/award", buyer.Token, map[string]any{"proposal_id": ranking[0].ProposalID}, &agr); code != http.StatusCreated {
		t.Fatalf("award: %d", code)
	}
	if code := e.call(http.MethodPost, "/v1/intents/"+in.ID+"/award", buyer.Token, map[string]any{"proposal_id": ranking[1].ProposalID}, nil); code != http.StatusConflict {
		t.Fatalf("second award: %d", code)
	}
	if len(agr.TermsHash) != 64 || agr.Status != "awaiting-funding" {
		t.Fatalf("agreement: %+v", agr)
	}

	// Escrow confirmations arrive from the agent (webhook or reconciliation).
	ctx := context.Background()
	if _, err := e.market.Funded(ctx, agr.ID, "esc-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.market.Funded(ctx, agr.ID, "esc-1"); err != nil {
		t.Fatalf("duplicate funding confirmation must be a no-op: %v", err)
	}
	var winner party
	for _, s := range sellers {
		var a map[string]any
		if e.call(http.MethodGet, "/v1/agreements/"+agr.ID, s.Token, nil, &a) == http.StatusOK {
			winner = s
		}
	}
	e.call(http.MethodPost, "/v1/agreements/"+agr.ID+"/ship", winner.Token, map[string]any{"tracking": "BR123456789"}, nil)
	e.call(http.MethodPost, "/v1/agreements/"+agr.ID+"/deliver", buyer.Token, nil, nil)
	var accepted struct {
		Status string `json:"status"`
	}
	e.call(http.MethodPost, "/v1/agreements/"+agr.ID+"/accept", buyer.Token, nil, &accepted)
	if accepted.Status != "releasing" {
		t.Fatalf("accept: %s", accepted.Status)
	}
	if _, err := e.market.Settle(ctx, agr.ID, true); err != nil {
		t.Fatal(err)
	}
	if code := e.call(http.MethodPost, "/v1/agreements/"+agr.ID+"/ratings", buyer.Token, map[string]any{"stars": 5}, nil); code != http.StatusAccepted {
		t.Fatalf("rating: %d", code)
	}
	var rep map[string]struct {
		N int `json:"n"`
	}
	e.call(http.MethodGet, "/v1/parties/"+winner.ID+"/reputation", "", nil, &rep)
	if rep["seller"].N != 0 {
		t.Fatal("a single rating stays sealed")
	}
	e.call(http.MethodPost, "/v1/agreements/"+agr.ID+"/ratings", winner.Token, map[string]any{"stars": 4}, nil)
	e.call(http.MethodGet, "/v1/parties/"+winner.ID+"/reputation", "", nil, &rep)
	if rep["seller"].N != 1 {
		t.Fatalf("both rated: reveal, got %+v", rep)
	}

	// The outbox holds every event and no proposal event carries a price.
	cur, _ := e.store.Database().Collection("outbox").Find(ctx, bson.D{})
	var evs []struct {
		Type string `bson:"type"`
		Data string `bson:"data"`
	}
	_ = cur.All(ctx, &evs)
	types := map[string]int{}
	for _, ev := range evs {
		types[ev.Type]++
		if strings.Contains(ev.Type, "proposal") && (strings.Contains(ev.Data, "price") || strings.Contains(ev.Data, "1200")) {
			t.Fatalf("price leaked in %s: %s", ev.Type, ev.Data)
		}
	}
	for _, want := range []string{
		intent.EvPublished, intent.EvClosed, intent.EvAwarded, "intenza.proposal.submitted.v1",
		"intenza.agreement.formed.v1", "intenza.agreement.released.v1", "intenza.reputation.ratings-revealed.v1",
	} {
		if types[want] == 0 {
			t.Errorf("no %s in the outbox (%v)", want, types)
		}
	}
	if types[intent.EvPublished] != 1 {
		t.Fatalf("idempotent publish must emit one event, got %d", types[intent.EvPublished])
	}
}

// Sellers race the closer: whatever interleaving happens, the number of
// proposals stored equals the count frozen at the close.
func TestNoProposalAfterCloseUnderRace(t *testing.T) {
	e := newEnv(t, false)
	buyer := e.register(10)
	ctx := context.Background()
	in, _, err := e.market.PublishIntent(ctx, buyer.ID, "", intent.PublishCommand{
		CategoryID: "collectibles.tcg", Title: "corrida no fechamento", Constraints: map[string]any{},
		DeliveryCEP: "20040-020", Window: iso8601Must("PT2S"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var sellers []party
	for i := range 24 {
		sellers = append(sellers, e.register(100+i))
	}
	var wg sync.WaitGroup
	var closed intent.Intent
	start := time.Until(in.ClosesAt) - 600*time.Millisecond
	for i, s := range sellers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(start + time.Duration(i)*50*time.Millisecond)
			_, _ = e.market.SubmitProposal(ctx, s.ID, in.ID, app.SubmitInput{
				Attributes: tcgItem, Price: money.BRL(int64(100000 + i)), LeadTimeDays: 3, ValidUntil: in.SelectBy,
			})
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := e.market.CloseIntent(ctx, in.ID)
			if err == nil && c.Status == intent.StatusClosed {
				closed = c
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	wg.Wait()
	n, _ := e.store.Database().Collection("proposals").CountDocuments(ctx, bson.D{{Key: "intent_id", Value: in.ID}})
	final, _ := e.market.Intent(ctx, in.ID)
	if int(n) != closed.ProposalCount || final.ProposalCount != closed.ProposalCount {
		t.Fatalf("stored %d, count at close %d, final count %d", n, closed.ProposalCount, final.ProposalCount)
	}
	if n == 0 || int(n) == len(sellers) {
		t.Logf("the race did not straddle the deadline (%d of %d); the invariant still held", n, len(sellers))
	}
}

// Ten concurrent awards on one intent: exactly one agreement.
func TestSingleAwardUnderRace(t *testing.T) {
	e := newEnv(t, false)
	buyer := e.register(20)
	ctx := context.Background()
	in, _, err := e.market.PublishIntent(ctx, buyer.ID, "", intent.PublishCommand{
		CategoryID: "collectibles.tcg", Title: "corrida na escolha", Constraints: map[string]any{},
		DeliveryCEP: "20040-020", Window: iso8601Must("PT1S"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := range 2 {
		s := e.register(200 + i)
		p, err := e.market.SubmitProposal(ctx, s.ID, in.ID, app.SubmitInput{
			Attributes: tcgItem, Price: money.BRL(int64(5000 + i)),
			LeadTimeDays: 2, ValidUntil: in.SelectBy,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	waitUntil(in.ClosesAt)
	if _, err := e.market.CloseIntent(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.market.Award(ctx, buyer.ID, in.ID, ids[i%2]); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	n, _ := e.store.Database().Collection("agreements").CountDocuments(ctx, bson.D{{Key: "intent_id", Value: in.ID}})
	if ok != 1 || n != 1 {
		t.Fatalf("successful awards %d, agreements %d", ok, n)
	}
}

// A use case that fails after writing leaves neither state nor event.
func TestOutboxIsAtomicWithState(t *testing.T) {
	e := newEnv(t, false)
	buyer := e.register(30)
	ctx := context.Background()
	boom := errors.New("falha depois de escrever")
	err := e.store.Tx(ctx, func(ctx context.Context, tx app.Tx) error {
		now, _ := tx.Now(ctx)
		in, evs, err := intent.Publish(intent.PublishCommand{
			ID: "atomic-1", BuyerID: buyer.ID, CategoryID: "collectibles.tcg",
			Title: "não deve existir", Constraints: map[string]any{}, DeliveryCEP: "20040-020", Window: iso8601Must("PT1H"),
		},
			e.market.Tree, intent.DefaultPolicy, now)
		if err != nil {
			return err
		}
		if err := tx.InsertIntent(ctx, in); err != nil {
			return err
		}
		if err := tx.Outbox(ctx, []app.Envelope{{ID: "ev-atomic-1", Event: evs[0]}}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	for coll, id := range map[string]string{"intents": "atomic-1", "outbox": "ev-atomic-1"} {
		if n, _ := e.store.Database().Collection(coll).CountDocuments(ctx, bson.D{{Key: "_id", Value: id}}); n != 0 {
			t.Fatalf("%s kept a write from an aborted transaction", coll)
		}
	}
}
