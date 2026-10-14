//intenza:paradigm imperative-shell

// Package mongo is the store: every aggregate is a document with a JSON body
// (exact fidelity of the domain value) plus the projected fields queries and
// indexes need. Writes go through multi-document transactions with snapshot
// reads and majority writes, and the events of a use case land in the outbox
// collection inside the same transaction.
package mongo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"

	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
)

const (
	cIntents     = "intents"
	cProposals   = "proposals"
	cAgreements  = "agreements"
	cParties     = "parties"
	cRatingPairs = "rating_pairs"
	cRatings     = "ratings"
	cOutbox      = "outbox"
	cInbox       = "inbox"
)

// Store implements app.Store.
type Store struct {
	client *mongo.Client
	db     *mongo.Database
}

// Connect opens the client, checks it and makes sure the indexes exist.
// The operation timeout bounds every call so an unreachable primary turns
// into a fast error instead of a hung request.
func Connect(ctx context.Context, uri, database string, opTimeout time.Duration) (*Store, error) {
	opts := options.Client().ApplyURI(uri).SetTimeout(opTimeout).SetServerSelectionTimeout(opTimeout).SetAppName("intenza")
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, err
	}
	s := &Store{client: client, db: client.Database(database)}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo: ping: %w", err)
	}
	return s, s.ensureIndexes(ctx)
}

// Client exposes the driver client for components that share it (relay, inbox).
func (s *Store) Client() *mongo.Client { return s.client }

// Database exposes the database handle.
func (s *Store) Database() *mongo.Database { return s.db }

// Close disconnects.
func (s *Store) Close(ctx context.Context) error { return s.client.Disconnect(ctx) }

// Ping checks reachability (readiness probe).
func (s *Store) Ping(ctx context.Context) error { return s.client.Ping(ctx, nil) }

func (s *Store) ensureIndexes(ctx context.Context) error {
	idx := map[string][]mongo.IndexModel{
		cIntents: {
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "category_id", Value: 1}, {Key: "closes_at", Value: 1}}},
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "select_by", Value: 1}}},
			{Keys: bson.D{{Key: "buyer_id", Value: 1}, {Key: "opened_at", Value: -1}}},
		},
		cProposals: {
			{Keys: bson.D{{Key: "intent_id", Value: 1}, {Key: "seller_id", Value: 1}}, Options: options.Index().SetUnique(true)},
			{Keys: bson.D{{Key: "seller_id", Value: 1}}},
		},
		cAgreements: {
			{Keys: bson.D{{Key: "intent_id", Value: 1}}, Options: options.Index().SetUnique(true)},
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "fund_by", Value: 1}}},
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "inspect_until", Value: 1}}},
		},
		cRatings: {
			{Keys: bson.D{{Key: "agreement_id", Value: 1}, {Key: "rater_id", Value: 1}}, Options: options.Index().SetUnique(true)},
			{Keys: bson.D{{Key: "ratee_id", Value: 1}, {Key: "role", Value: 1}}},
		},
		cOutbox: {
			{
				Keys:    bson.D{{Key: "published", Value: 1}, {Key: "_id", Value: 1}},
				Options: options.Index().SetPartialFilterExpression(bson.D{{Key: "published", Value: false}}),
			},
		},
		cInbox: {
			{Keys: bson.D{{Key: "consumer", Value: 1}, {Key: "event_id", Value: 1}}, Options: options.Index().SetUnique(true)},
			{Keys: bson.D{{Key: "processed_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(7 * 24 * 3600)},
		},
	}
	for coll, models := range idx {
		if _, err := s.db.Collection(coll).Indexes().CreateMany(ctx, models); err != nil {
			return fmt.Errorf("mongo: índices de %s: %w", coll, err)
		}
	}
	return nil
}

var txnOpts = options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority())

// Tx runs fn in a transaction; the driver retries it on transient errors such
// as the write conflict two racing use cases produce on the same intent.
func (s *Store) Tx(ctx context.Context, fn func(ctx context.Context, tx app.Tx) error) error {
	sess, err := s.client.StartSession()
	if err != nil {
		return err
	}
	defer sess.EndSession(context.WithoutCancel(ctx))
	_, err = sess.WithTransaction(ctx, func(tctx context.Context) (any, error) {
		return nil, fn(tctx, &tx{s: s})
	}, txnOpts)
	return translate(err)
}

// View runs fn with plain reads, outside a transaction.
func (s *Store) View(ctx context.Context, fn func(ctx context.Context, tx app.Tx) error) error {
	return translate(fn(ctx, &tx{s: s}))
}

// OpenIntents lists open intents in a category subtree, closest deadline first.
func (s *Store) OpenIntents(ctx context.Context, categoryID string, limit int) ([]intent.Intent, error) {
	filter := bson.D{{Key: "status", Value: string(intent.StatusOpen)}}
	if categoryID != "" {
		filter = append(filter, bson.E{Key: "category_id", Value: bson.D{{Key: "$regex", Value: "^" + regexp.QuoteMeta(categoryID) + `(\.|$)`}}})
	}
	cur, err := s.db.Collection(cIntents).Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "closes_at", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	return decodeAll[intent.Intent](ctx, cur)
}

// DueIntents finds intents past a deadline by the database clock.
func (s *Store) DueIntents(ctx context.Context, limit int) (closing, expiring []string, err error) {
	if closing, err = s.ids(ctx, cIntents, bson.D{
		{Key: "status", Value: "open"},
		{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$closes_at", "$$NOW"}}}},
	}, limit); err != nil {
		return nil, nil, err
	}
	expiring, err = s.ids(ctx, cIntents, bson.D{
		{Key: "status", Value: "closed"},
		{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$select_by", "$$NOW"}}}},
	}, limit)
	return closing, expiring, err
}

// DueAgreements finds agreements whose funding or inspection window lapsed.
func (s *Store) DueAgreements(ctx context.Context, limit int) ([]string, error) {
	return s.ids(ctx, cAgreements, bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "status", Value: "awaiting-funding"}, {Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$fund_by", "$$NOW"}}}}},
		bson.D{{Key: "status", Value: "delivered"}, {Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$inspect_until", "$$NOW"}}}}},
	}}}, limit)
}

func (s *Store) ids(ctx context.Context, coll string, filter bson.D, limit int) ([]string, error) {
	cur, err := s.db.Collection(coll).Find(ctx, filter, options.Find().SetProjection(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID string `bson:"_id"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out, nil
}

type bodyDoc struct {
	Body string `bson:"body"`
}

func decodeAll[T any](ctx context.Context, cur *mongo.Cursor) ([]T, error) {
	var docs []bodyDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]T, len(docs))
	for i, d := range docs {
		if err := json.Unmarshal([]byte(d.Body), &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, mongo.ErrNoDocuments):
		return app.ErrNotFound
	case mongo.IsDuplicateKeyError(err):
		return app.ErrDuplicate
	case errors.Is(err, app.ErrUnavailable):
		return err
	case mongo.IsTimeout(err) || mongo.IsNetworkError(err) || errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %w", app.ErrUnavailable, err)
	}
	return err
}
