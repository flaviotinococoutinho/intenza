//intenza:paradigm csp

// Package relay moves events from the MongoDB outbox to Kafka. Two goroutines
// feed one publisher through a channel: a change stream that wakes on every
// committed insert (low latency) and a sweeper that republishes anything still
// unpublished after a grace period (the safety net for a dead stream, an
// expired resume token or a crash between publish and mark).
//
// Publishing is at least once. A crash after the broker acknowledged and
// before the outbox is marked republishes the same events, with the same ids;
// consumers deduplicate them.
package relay

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/flaviotinococoutinho/intenza/internal/adapter/kafka"
)

// Relay is the outbox-to-broker pump.
type Relay struct {
	DB       *mongo.Database
	Producer *kgo.Client
	Log      *slog.Logger
	Grace    time.Duration // how old an unpublished event must be for the sweeper
	Every    time.Duration // sweep period
	Batch    int

	// CrashAfter is a chaos knob: exit right after the broker acked a batch
	// and before marking it, to provoke duplicates. Empty is off, "*" is any
	// batch, an event type waits for a batch carrying one, so the experiment
	// knows which consumer will see the redelivery.
	CrashAfter atomic.Pointer[string]
	Exit       func(code int)

	// OnPublished is a metrics hook (count, oldest age).
	OnPublished func(n int, oldest time.Duration)
}

type doc struct {
	ID          string    `bson:"_id"`
	Type        string    `bson:"type"`
	Subject     string    `bson:"subject"`
	Time        time.Time `bson:"time"`
	Data        string    `bson:"data"`
	TraceParent string    `bson:"traceparent"`
}

func (d doc) event() kafka.Event {
	return kafka.Event{ID: d.ID, Type: d.Type, Subject: d.Subject, Time: d.Time, Data: []byte(d.Data), TraceParent: d.TraceParent}
}

// Run blocks until ctx ends.
func (r *Relay) Run(ctx context.Context) error {
	if r.Batch <= 0 {
		r.Batch = 500
	}
	if r.Every <= 0 {
		r.Every = 2 * time.Second
	}
	work := make(chan []doc, 16)
	go r.watch(ctx, work)
	go r.sweep(ctx, work)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case batch := <-work:
			if err := r.publish(ctx, batch); err != nil && ctx.Err() == nil {
				r.Log.WarnContext(ctx, "publicação falhou, a varredura tenta de novo", "n", len(batch), "err", err)
			}
		}
	}
}

func (r *Relay) publish(ctx context.Context, batch []doc) error {
	if len(batch) == 0 {
		return nil
	}
	recs := make([]*kgo.Record, len(batch))
	ids := make([]string, len(batch))
	oldest := time.Duration(0)
	for i, d := range batch {
		recs[i], ids[i] = d.event().Record(), d.ID
		if age := time.Since(d.Time); age > oldest {
			oldest = age
		}
	}
	if err := r.Producer.ProduceSync(ctx, recs...).FirstErr(); err != nil {
		return err
	}
	if r.crashDue(batch) && r.Exit != nil {
		r.Log.Warn("falha injetada: saindo depois de publicar e antes de marcar", "n", len(batch))
		r.Exit(3)
	}
	mctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := r.DB.Collection("outbox").UpdateMany(mctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "published", Value: true}, {Key: "published_at", Value: time.Now()}}}})
	if err == nil && r.OnPublished != nil {
		r.OnPublished(len(batch), oldest)
	}
	return err
}

func (r *Relay) crashDue(batch []doc) bool {
	want := r.CrashAfter.Load()
	switch {
	case want == nil || *want == "":
		return false
	case *want == "*":
		return true
	}
	return slices.ContainsFunc(batch, func(d doc) bool { return d.Type == *want })
}

// watch follows committed inserts on the outbox, resuming from a stored token.
func (r *Relay) watch(ctx context.Context, work chan<- []doc) {
	checkpoints := r.DB.Collection("relay_checkpoints")
	for ctx.Err() == nil {
		opts := options.ChangeStream().SetBatchSize(int32(r.Batch)).SetMaxAwaitTime(time.Second)
		var cp struct {
			Token bson.Raw `bson:"token"`
		}
		if err := checkpoints.FindOne(ctx, bson.D{{Key: "_id", Value: "outbox"}}).Decode(&cp); err == nil && len(cp.Token) > 0 {
			opts.SetResumeAfter(cp.Token)
		}
		pipeline := mongo.Pipeline{{{Key: "$match", Value: bson.D{{Key: "operationType", Value: "insert"}}}}}
		cs, err := r.DB.Collection("outbox").Watch(ctx, pipeline, opts)
		if err != nil {
			// An invalid token (oplog rolled over) also lands here: forget it
			// and let the sweeper cover the gap.
			r.Log.WarnContext(ctx, "change stream indisponível", "err", err)
			_, _ = checkpoints.DeleteOne(ctx, bson.D{{Key: "_id", Value: "outbox"}})
			sleep(ctx, time.Second)
			continue
		}
		r.follow(ctx, cs, work)
		_ = cs.Close(context.WithoutCancel(ctx))
	}
}

func (r *Relay) follow(ctx context.Context, cs *mongo.ChangeStream, work chan<- []doc) {
	checkpoints := r.DB.Collection("relay_checkpoints")
	var batch []doc
	for cs.Next(ctx) {
		var ev struct {
			Doc doc `bson:"fullDocument"`
		}
		if err := cs.Decode(&ev); err != nil {
			continue
		}
		batch = append(batch, ev.Doc)
		if cs.RemainingBatchLength() == 0 || len(batch) >= r.Batch {
			select {
			case work <- batch:
			case <-ctx.Done():
				return
			}
			batch = nil
			_, _ = checkpoints.UpdateOne(ctx, bson.D{{Key: "_id", Value: "outbox"}},
				bson.D{{Key: "$set", Value: bson.D{{Key: "token", Value: cs.ResumeToken()}}}}, options.UpdateOne().SetUpsert(true))
		}
	}
	if err := cs.Err(); err != nil && !errors.Is(err, context.Canceled) {
		r.Log.WarnContext(ctx, "change stream caiu", "err", err)
	}
}

// sweep republishes events still unpublished after the grace period.
func (r *Relay) sweep(ctx context.Context, work chan<- []doc) {
	t := time.NewTicker(r.Every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		cur, err := r.DB.Collection("outbox").Find(qctx,
			bson.D{{Key: "published", Value: false}, {Key: "time", Value: bson.D{{Key: "$lt", Value: time.Now().Add(-r.Grace)}}}},
			options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(r.Batch)))
		if err != nil {
			cancel()
			r.Log.WarnContext(ctx, "varredura do outbox falhou", "err", err)
			continue
		}
		var batch []doc
		err = cur.All(qctx, &batch)
		cancel()
		if err != nil || len(batch) == 0 {
			continue
		}
		select {
		case work <- batch:
		case <-ctx.Done():
			return
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

// Backlog reports how many events wait and the age of the oldest (outbox lag).
func Backlog(ctx context.Context, db *mongo.Database) (int64, time.Duration, error) {
	coll := db.Collection("outbox")
	n, err := coll.CountDocuments(ctx, bson.D{{Key: "published", Value: false}})
	if err != nil || n == 0 {
		return n, 0, err
	}
	var d doc
	if err := coll.FindOne(ctx, bson.D{{Key: "published", Value: false}}, options.FindOne().SetSort(bson.D{{Key: "_id", Value: 1}})).Decode(&d); err != nil {
		return n, 0, err
	}
	return n, time.Since(d.Time), nil
}
