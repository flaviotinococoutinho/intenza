//go:build integration

package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/flaviotinococoutinho/intenza/internal/adapter/escrow"
	"github.com/flaviotinococoutinho/intenza/internal/adapter/kafka"
	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/domain/agreement"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/money"
	"github.com/flaviotinococoutinho/intenza/internal/psp"
	"github.com/flaviotinococoutinho/intenza/internal/relay"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// The relay publishes every outbox event; a crash between publish and mark
// republishes them (same ids), and the inbox keeps the effect single.
func TestRelayAtLeastOnceAndInboxEffectivelyOnce(t *testing.T) {
	e := newEnv(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	buyer := e.register(40)
	for i := range 3 {
		if _, _, err := e.market.PublishIntent(ctx, buyer.ID, "k"+string(rune('a'+i)), intent.PublishCommand{
			CategoryID: "collectibles.vinyl", Title: "disco raro de vinil", Constraints: map[string]any{},
			DeliveryCEP: "20040-020", Window: iso8601Must("PT1H"),
		}); err != nil {
			t.Fatal(err)
		}
	}
	producer, err := kafka.NewProducer([]string{broker})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	if err := kafka.EnsureTopics(ctx, producer, 3); err != nil {
		t.Fatal(err)
	}
	topic := kafka.TopicIntents
	r := &relay.Relay{DB: e.store.Database(), Producer: producer, Log: quiet(), Grace: 0, Every: 200 * time.Millisecond}
	go func() { _ = r.Run(ctx) }()
	waitFor(t, 15*time.Second, func() bool {
		n, _, _ := relay.Backlog(ctx, e.store.Database())
		return n == 0
	})
	// crash before the mark: everything goes back to unpublished
	_, _ = e.store.Database().Collection("outbox").UpdateMany(ctx, bson.D{}, bson.D{{Key: "$set", Value: bson.D{{Key: "published", Value: false}}}})
	waitFor(t, 15*time.Second, func() bool {
		n, _, _ := relay.Backlog(ctx, e.store.Database())
		return n == 0
	})

	cl, err := kgo.NewClient(kgo.SeedBrokers(broker), kgo.ConsumeTopics(topic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	seen := map[string]int{}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		pctx, pcancel := context.WithTimeout(ctx, time.Second)
		fs := cl.PollFetches(pctx)
		pcancel()
		fs.EachRecord(func(rec *kgo.Record) {
			ev := kafka.FromRecord(rec)
			if ev.Type == intent.EvPublished {
				seen[ev.ID]++
			}
		})
		dup := 0
		for _, n := range seen {
			if n >= 2 {
				dup++
			}
		}
		if len(seen) >= 3 && dup >= 3 {
			break
		}
	}
	if len(seen) < 3 {
		t.Fatalf("published events seen: %v", seen)
	}
	// the same event applied twice through the inbox runs its effect once
	applied := 0
	for id, n := range seen {
		for range n {
			ran, err := e.store.Once(ctx, "test-consumer", id, func(ctx context.Context, tx app.Tx) error { applied++; return nil })
			if err != nil {
				t.Fatal(err)
			}
			_ = ran
		}
	}
	if applied != len(seen) {
		t.Fatalf("effects %d for %d distinct events (%v)", applied, len(seen), seen)
	}
}

// The settlement saga against the simulated escrow agent: escrow opened with
// a Pix BR Code, webhooks move the agreement, a lost release webhook is
// recovered by reconciliation, and duplicated webhooks change nothing.
func TestSettlementSagaAgainstSimulatedAgent(t *testing.T) {
	e := newEnv(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sim := psp.New(e.srv.URL+"/v1/webhooks/escrow", []byte(webhookSecret), 3, quiet())
	pspSrv := httptest.NewServer(sim.Handler())
	defer pspSrv.Close()
	go sim.Run(ctx)
	settlement := &app.Settlement{Market: e.market, Escrow: escrow.New(pspSrv.URL, time.Second, quiet()), Back: e.store, Log: quiet()}

	a := formAgreement(t, e)
	if err := settlement.Handle(ctx, agreement.EvFormed, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := settlement.Handle(ctx, agreement.EvFormed, a.ID); err != nil {
		t.Fatalf("redelivered formed event: %v", err)
	}
	opened, _ := e.market.Agreement(ctx, a.Terms.BuyerID, a.ID)
	if opened.EscrowID == "" || len(opened.PaymentCode) < 50 || opened.PaymentCode[:6] != "000201" {
		t.Fatalf("escrow %q code %q", opened.EscrowID, opened.PaymentCode)
	}
	sim.SetFaults(psp.Faults{WebhookDup: 1}) // every webhook arrives twice
	pay(t, pspSrv.URL, opened.EscrowID)
	waitFor(t, 10*time.Second, func() bool {
		x, _ := e.market.Agreement(ctx, a.Terms.BuyerID, a.ID)
		return x.Status == agreement.StatusFunded
	})
	_, _ = e.market.Ship(ctx, a.Terms.SellerID, a.ID, "BR555")
	_, _ = e.market.Deliver(ctx, a.Terms.BuyerID, a.ID)
	if _, err := e.market.Accept(ctx, a.Terms.BuyerID, a.ID); err != nil {
		t.Fatal(err)
	}
	sim.SetFaults(psp.Faults{WebhookDrop: 1}) // the release confirmation is lost
	if err := settlement.Handle(ctx, agreement.EvReleaseRequested, a.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if x, _ := e.market.Agreement(ctx, a.Terms.BuyerID, a.ID); x.Status != agreement.StatusReleasing {
		t.Fatalf("without the webhook the agreement waits: %s", x.Status)
	}
	if _, err := settlement.Reconcile(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if x, _ := e.market.Agreement(ctx, a.Terms.BuyerID, a.ID); x.Status != agreement.StatusReleased {
		t.Fatalf("reconciliation converged to %s", x.Status)
	}
}

func pay(t *testing.T, base, escrowID string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/escrows/"+escrowID+"/pay", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
}

// Without Redis the sweep closes overdue intents by the database clock.
func TestDeadlinesSweepWithoutTheIndex(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	buyer := e.register(50)
	in, _, err := e.market.PublishIntent(ctx, buyer.ID, "", intent.PublishCommand{
		CategoryID: "collectibles.coins",
		Title:      "moeda do império", Constraints: map[string]any{}, DeliveryCEP: "20040-020", Window: iso8601Must("PT1S"),
	})
	if err != nil {
		t.Fatal(err)
	}
	d := &app.Deadlines{Market: e.market, Log: quiet()}
	waitUntil(in.ClosesAt)
	if err := d.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.market.Intent(ctx, in.ID); v.Status != intent.StatusClosed {
		t.Fatalf("status %s", v.Status)
	}
}

// The index and the sweep can both reach a deadline. Lateness belongs to the
// trigger that made the transition: an index back from an outage replays what
// the sweep already closed, and that replay is not lateness.
func TestDeadlineLatenessCountedOncePerTransition(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	buyer := e.register(52)
	in, _, err := e.market.PublishIntent(ctx, buyer.ID, "", intent.PublishCommand{
		CategoryID: "collectibles.coins",
		Title:      "moeda da república", Constraints: map[string]any{}, DeliveryCEP: "20040-020", Window: iso8601Must("PT1S"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var observed []string
	d := &app.Deadlines{
		Market: e.market, Log: quiet(),
		// the index still holds the entry the sweep is about to handle
		Claim: func(context.Context, time.Time, int) ([]string, error) { return []string{"intent-close:" + in.ID}, nil },
		OnFire: func(kind, source string, _ time.Duration) {
			if kind == "intent-close" {
				observed = append(observed, source)
			}
		},
	}
	waitUntil(in.ClosesAt)
	if err := d.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(observed, []string{"sweep"}) {
		t.Fatalf("lateness observed by %v, want only the sweep, which made the transition", observed)
	}
}

func formAgreement(t *testing.T, e *env) agreement.Agreement {
	t.Helper()
	ctx := context.Background()
	buyer, seller := e.register(60), e.register(61)
	in, _, err := e.market.PublishIntent(ctx, buyer.ID, "", intent.PublishCommand{
		CategoryID: "collectibles.tcg",
		Title:      "carta para o escrow", Constraints: map[string]any{}, DeliveryCEP: "20040-020", Window: iso8601Must("PT1S"),
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.market.SubmitProposal(ctx, seller.ID, in.ID, app.SubmitInput{
		Attributes: tcgItem, Price: money.BRL(98765),
		LeadTimeDays: 2, ValidUntil: in.SelectBy,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(in.ClosesAt)
	if _, err := e.market.CloseIntent(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	a, err := e.market.Award(ctx, buyer.ID, in.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("condition not reached in time")
}
