package kafka

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// parking stands in for the client: it keeps what would go to the DLQ.
type parking struct {
	records []*kgo.Record
	err     error
}

func (p *parking) ProduceSync(_ context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	out := make(kgo.ProduceResults, 0, len(rs))
	for _, r := range rs {
		if p.err == nil {
			p.records = append(p.records, r)
		}
		out = append(out, kgo.ProduceResult{Record: r, Err: p.err})
	}
	return out
}

func newConsumer(handle Handler) (*Consumer, *[]string) {
	results := &[]string{}
	return &Consumer{
		Group:    "settlement",
		Handle:   handle,
		Backoff:  time.Millisecond,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnResult: func(_, r string) { *results = append(*results, r) },
	}, results
}

func fetched() *kgo.Record {
	r := Event{ID: "e-1", Type: "intenza.agreement.formed.v1", Subject: "a-1", Data: []byte(`{}`)}.Record()
	r.Partition, r.Offset = 2, 41
	return r
}

func header(r *kgo.Record, key string) string {
	for _, h := range r.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func TestTransientFailureRetriesThenParksInTheDLQ(t *testing.T) {
	calls := 0
	c, results := newConsumer(func(context.Context, Event) error {
		calls++
		return errors.New("agreement not found")
	})
	p := &parking{}
	if err := c.process(t.Context(), p, fetched(), 3); err != nil {
		t.Fatalf("process = %v, want nil so the offset is committed and the partition moves on", err)
	}
	if calls != 3 {
		t.Errorf("handler ran %d times, want 3", calls)
	}
	if want := []string{"retried", "retried", "dead-lettered"}; !slices.Equal(*results, want) {
		t.Errorf("results %v, want %v", *results, want)
	}
	if len(p.records) != 1 {
		t.Fatalf("parked %d records, want 1", len(p.records))
	}
	d := p.records[0]
	if d.Topic != TopicDLQ || string(d.Key) != "a-1" || string(d.Value) != "{}" {
		t.Errorf("parked topic=%s key=%s value=%s", d.Topic, d.Key, d.Value)
	}
	for key, want := range map[string]string{
		"dlq_reason": "agreement not found",
		"dlq_origin": "intenza.agreements/2/41",
		"dlq_group":  "settlement",
		"ce_id":      "e-1",
	} {
		if got := header(d, key); got != want {
			t.Errorf("header %s = %q, want %q", key, got, want)
		}
	}
}

func TestPoisonSkipsTheRetries(t *testing.T) {
	calls := 0
	c, results := newConsumer(func(context.Context, Event) error {
		calls++
		return fmt.Errorf("unknown type: %w", ErrPoison)
	})
	p := &parking{}
	if err := c.process(t.Context(), p, fetched(), 3); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(p.records) != 1 || !slices.Equal(*results, []string{"dead-lettered"}) {
		t.Errorf("calls=%d parked=%d results=%v, want one call straight to the DLQ", calls, len(p.records), *results)
	}
}

func TestRecoveryAfterARetryIsNotParked(t *testing.T) {
	calls := 0
	c, results := newConsumer(func(context.Context, Event) error {
		calls++
		if calls == 1 {
			return errors.New("mongo timeout")
		}
		return nil
	})
	p := &parking{}
	if err := c.process(t.Context(), p, fetched(), 3); err != nil {
		t.Fatal(err)
	}
	if len(p.records) != 0 || !slices.Equal(*results, []string{"retried", "ok"}) {
		t.Errorf("parked=%d results=%v, want a retry and then ok", len(p.records), *results)
	}
}

func TestShutdownNeverParks(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	c, _ := newConsumer(func(context.Context, Event) error {
		cancel()
		return errors.New("interrupted")
	})
	p := &parking{}
	if err := c.process(ctx, p, fetched(), 3); !errors.Is(err, context.Canceled) {
		t.Fatalf("process = %v, want context.Canceled so the record is delivered again", err)
	}
	if len(p.records) != 0 {
		t.Errorf("parked %d records on shutdown, want 0", len(p.records))
	}
}

func TestDLQOutageStopsInsteadOfSkipping(t *testing.T) {
	c, _ := newConsumer(func(context.Context, Event) error { return fmt.Errorf("bad: %w", ErrPoison) })
	down := errors.New("broker down")
	if err := c.process(t.Context(), &parking{err: down}, fetched(), 3); !errors.Is(err, down) {
		t.Fatalf("process = %v, want the produce error so the offset is not committed", err)
	}
}

func TestRecordRoundTripsTheEnvelope(t *testing.T) {
	in := Event{
		ID: "e-9", Type: "intenza.intent.published.v1", Subject: "i-1",
		Time:        time.Date(2026, 10, 14, 8, 0, 0, 123_000_000, time.UTC),
		Data:        []byte(`{"a":1}`),
		TraceParent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}
	r := in.Record()
	if r.Topic != TopicIntents || string(r.Key) != "i-1" || header(r, "ce_specversion") != "1.0" {
		t.Fatalf("topic=%s key=%s specversion=%q", r.Topic, r.Key, header(r, "ce_specversion"))
	}
	out := FromRecord(r)
	if out.ID != in.ID || out.Type != in.Type || out.Subject != in.Subject || !out.Time.Equal(in.Time) ||
		string(out.Data) != string(in.Data) || out.TraceParent != in.TraceParent {
		t.Errorf("round trip changed the event:\n in %+v\nout %+v", in, out)
	}
}
