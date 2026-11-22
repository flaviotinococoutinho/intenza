//intenza:paradigm reactive

// Package kafka publishes domain events as CloudEvents in binary mode (the
// envelope in record headers, the data as the record value) and runs consumer
// groups with manual commits: a record is committed only after its handler
// succeeded or the record was parked in the dead letter topic.
//
// Delivery is at least once on both sides; effects are made idempotent by the
// consumers' inbox, not by the broker.
package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Topics, one per bounded context. The key is the aggregate id, so the events
// of one intent (and its proposals) stay in order inside a partition.
const (
	TopicIntents    = "intenza.intents"
	TopicAgreements = "intenza.agreements"
	TopicReputation = "intenza.reputation"
	TopicDLQ        = "intenza.dlq"
)

// TopicFor routes an event type to its topic.
func TopicFor(eventType string) string {
	switch {
	case strings.HasPrefix(eventType, "intenza.agreement."):
		return TopicAgreements
	case strings.HasPrefix(eventType, "intenza.reputation."):
		return TopicReputation
	}
	return TopicIntents
}

// Event is the CloudEvents view of an outbox record.
type Event struct {
	ID          string
	Type        string
	Subject     string
	Time        time.Time
	Data        []byte
	TraceParent string
}

// Record turns an event into a Kafka record in CloudEvents binary mode.
func (e Event) Record() *kgo.Record {
	h := []kgo.RecordHeader{
		{Key: "ce_specversion", Value: []byte("1.0")},
		{Key: "ce_id", Value: []byte(e.ID)},
		{Key: "ce_source", Value: []byte("/intenza")},
		{Key: "ce_type", Value: []byte(e.Type)},
		{Key: "ce_subject", Value: []byte(e.Subject)},
		{Key: "ce_time", Value: []byte(e.Time.UTC().Format(time.RFC3339Nano))},
		{Key: "content-type", Value: []byte("application/json")},
	}
	if e.TraceParent != "" {
		h = append(h, kgo.RecordHeader{Key: "traceparent", Value: []byte(e.TraceParent)})
	}
	return &kgo.Record{Topic: TopicFor(e.Type), Key: []byte(e.Subject), Value: e.Data, Headers: h}
}

// FromRecord reads the CloudEvents envelope back.
func FromRecord(r *kgo.Record) Event {
	e := Event{Data: r.Value, Subject: string(r.Key)}
	for _, h := range r.Headers {
		switch h.Key {
		case "ce_id":
			e.ID = string(h.Value)
		case "ce_type":
			e.Type = string(h.Value)
		case "ce_subject":
			e.Subject = string(h.Value)
		case "ce_time":
			e.Time, _ = time.Parse(time.RFC3339Nano, string(h.Value))
		case "traceparent":
			e.TraceParent = string(h.Value)
		}
	}
	return e
}

// NewProducer builds an idempotent producer (acks from all in-sync replicas).
func NewProducer(brokers []string, opts ...kgo.Opt) (*kgo.Client, error) {
	return kgo.NewClient(append([]kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.ProducerLinger(5 * time.Millisecond),
		kgo.RecordRetries(10),
		kgo.ProduceRequestTimeout(5 * time.Second),
		kgo.RetryTimeout(10 * time.Second),
	}, opts...)...)
}

// EnsureTopics creates the topics if missing (idempotent).
func EnsureTopics(ctx context.Context, cl *kgo.Client, partitions int32) error {
	adm := kadm.NewClient(cl)
	res, err := adm.CreateTopics(ctx, partitions, 1, nil, TopicIntents, TopicAgreements, TopicReputation, TopicDLQ)
	if err != nil {
		return err
	}
	for _, t := range res.Sorted() {
		if t.Err != nil && !strings.Contains(t.Err.Error(), "TOPIC_ALREADY_EXISTS") {
			return fmt.Errorf("tópico %s: %w", t.Topic, t.Err)
		}
	}
	return nil
}

// Handler processes one event. Returning an error retries it; ErrPoison skips
// the retries and parks it right away.
type Handler func(ctx context.Context, e Event) error

// ErrPoison marks an event no retry will fix (bad schema, unknown type).
var ErrPoison = errors.New("kafka: poison event")

// Consumer runs one consumer group.
type Consumer struct {
	Group    string
	Topics   []string
	Brokers  []string
	Handle   Handler
	Retries  int           // attempts per record before parking it (default 3)
	Backoff  time.Duration // base of the full-jitter backoff (default 100 ms)
	Log      *slog.Logger
	OnResult func(topic, result string) // metrics hook: ok | retried | dead-lettered
	Opts     []kgo.Opt
}

// producer is the one thing process needs from the client: parking a record.
type producer interface {
	ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults
}

// Run polls until ctx ends.
func (c *Consumer) Run(ctx context.Context) error {
	cl, err := kgo.NewClient(append([]kgo.Opt{
		kgo.SeedBrokers(c.Brokers...),
		kgo.ConsumerGroup(c.Group),
		kgo.ConsumeTopics(c.Topics...),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
		kgo.FetchMaxWait(500 * time.Millisecond),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	}, c.Opts...)...)
	if err != nil {
		return err
	}
	defer cl.Close()
	retries := c.Retries
	if retries <= 0 {
		retries = 3
	}
	for {
		fetches := cl.PollRecords(ctx, 200)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			cl.AllowRebalance()
			return ctx.Err()
		}
		fetches.EachError(func(t string, p int32, err error) {
			c.Log.WarnContext(ctx, "erro de fetch", "topic", t, "partition", p, "err", err)
		})
		var done []*kgo.Record
		var failed error
		for r := range fetches.RecordsAll() {
			if failed = c.process(ctx, cl, r, retries); failed != nil {
				break
			}
			done = append(done, r)
		}
		if len(done) > 0 {
			if err := cl.CommitRecords(ctx, done...); err != nil && ctx.Err() == nil {
				c.Log.WarnContext(ctx, "commit falhou, haverá reentrega", "err", err)
			}
		}
		cl.AllowRebalance()
		if failed != nil {
			// The client already moved past the failed record in memory: start
			// over from the last committed offset instead of skipping it.
			return failed
		}
	}
}

// Loop runs the consumer forever, restarting it after failures.
func (c *Consumer) Loop(ctx context.Context) {
	for ctx.Err() == nil {
		if err := c.Run(ctx); err != nil && ctx.Err() == nil {
			c.Log.WarnContext(ctx, "consumidor reiniciando", "group", c.Group, "err", err)
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
			}
		}
	}
}

func (c *Consumer) result(topic, r string) {
	if c.OnResult != nil {
		c.OnResult(topic, r)
	}
}

// process tries the handler up to retries times, with full-jitter backoff
// between attempts (a poison event gets a single one), then parks the record
// in the DLQ. Nil means handled or parked, so the offset may be committed; any
// other result means the record must not be skipped.
func (c *Consumer) process(ctx context.Context, p producer, r *kgo.Record, retries int) error {
	e := FromRecord(r)
	base := c.Backoff
	if base <= 0 {
		base = 100 * time.Millisecond
	}
	var err error
	for attempt := range retries {
		if err = c.Handle(ctx, e); err == nil {
			c.result(r.Topic, "ok")
			return nil
		}
		if errors.Is(err, ErrPoison) || ctx.Err() != nil || attempt == retries-1 {
			break
		}
		c.result(r.Topic, "retried")
		backoff := time.Duration(rand.Int64N(int64(base << min(attempt, 10))))
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	dead := &kgo.Record{Topic: TopicDLQ, Key: r.Key, Value: r.Value, Headers: append(r.Headers,
		kgo.RecordHeader{Key: "dlq_reason", Value: []byte(err.Error())},
		kgo.RecordHeader{Key: "dlq_origin", Value: fmt.Appendf(nil, "%s/%d/%d", r.Topic, r.Partition, r.Offset)},
		kgo.RecordHeader{Key: "dlq_group", Value: []byte(c.Group)})}
	if perr := p.ProduceSync(ctx, dead).FirstErr(); perr != nil {
		c.Log.ErrorContext(ctx, "não conseguiu estacionar na DLQ", "err", perr, "event", e.ID)
		return perr
	}
	c.Log.WarnContext(ctx, "evento estacionado na DLQ", "event", e.ID, "type", e.Type, "err", err)
	c.result(r.Topic, "dead-lettered")
	return nil
}
