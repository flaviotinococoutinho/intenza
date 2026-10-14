package mongo

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/domain/agreement"
)

// Once applies fn at most once per (consumer, event): the inbox record and
// the effect commit in the same transaction, so a redelivered event finds the
// record and is skipped. It reports whether fn ran.
func (s *Store) Once(ctx context.Context, consumer, eventID string, fn func(ctx context.Context, tx app.Tx) error) (bool, error) {
	ran := false
	err := s.Tx(ctx, func(ctx context.Context, t app.Tx) error {
		ran = false
		_, err := s.db.Collection(cInbox).InsertOne(ctx, bson.D{
			{Key: "consumer", Value: consumer},
			{Key: "event_id", Value: eventID},
			{Key: "processed_at", Value: time.Now()},
		})
		if err != nil {
			return translate(err)
		}
		ran = true
		return fn(ctx, t)
	})
	if errors.Is(err, app.ErrDuplicate) {
		return false, nil
	}
	return ran, err
}

// PendingSettlements lists agreements waiting on the escrow agent: opened but
// unpaid, or with a release or refund requested and not yet confirmed.
func (s *Store) PendingSettlements(ctx context.Context, olderThan time.Duration, limit int) ([]agreement.Agreement, error) {
	cur, err := s.db.Collection(cAgreements).Find(ctx, bson.D{
		{Key: "status", Value: bson.D{{Key: "$in", Value: bson.A{"awaiting-funding", "releasing", "refunding"}}}},
		{Key: "escrow_id", Value: bson.D{{Key: "$ne", Value: ""}}},
		{Key: "updated_at", Value: bson.D{{Key: "$lt", Value: time.Now().Add(-olderThan)}}},
	}, options.Find().SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	return decodeAll[agreement.Agreement](ctx, cur)
}

// FormedWithoutEscrow lists agreements whose escrow was never opened (the
// settlement consumer missed them), for the reconciler.
func (s *Store) FormedWithoutEscrow(ctx context.Context, olderThan time.Duration, limit int) ([]agreement.Agreement, error) {
	cur, err := s.db.Collection(cAgreements).Find(ctx, bson.D{
		{Key: "status", Value: "awaiting-funding"},
		{Key: "escrow_id", Value: ""},
		{Key: "updated_at", Value: bson.D{{Key: "$lt", Value: time.Now().Add(-olderThan)}}},
	}, options.Find().SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	return decodeAll[agreement.Agreement](ctx, cur)
}

// EligibleSellers lists parties holding every credential, up to limit.
func (s *Store) EligibleSellers(ctx context.Context, required []string, exclude string, limit int) ([]app.Party, error) {
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$ne", Value: exclude}}}}
	if len(required) > 0 {
		filter = append(filter, bson.E{Key: "credentials", Value: bson.D{{Key: "$all", Value: required}}})
	}
	cur, err := s.db.Collection(cParties).Find(ctx, filter, options.Find().SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	return decodeAll[app.Party](ctx, cur)
}

// Notify stores notifications for parties (fan-out results).
func (s *Store) Notify(ctx context.Context, intentID string, partyIDs []string) error {
	if len(partyIDs) == 0 {
		return nil
	}
	docs := make([]any, len(partyIDs))
	for i, p := range partyIDs {
		docs[i] = bson.D{{Key: "party_id", Value: p}, {Key: "intent_id", Value: intentID}, {Key: "created_at", Value: time.Now()}}
	}
	_, err := s.db.Collection("notifications").InsertMany(ctx, docs)
	return translate(err)
}

// Notifications returns a party's latest notifications.
func (s *Store) Notifications(ctx context.Context, partyID string, limit int) ([]app.Notification, error) {
	cur, err := s.db.Collection("notifications").Find(ctx, bson.D{{Key: "party_id", Value: partyID}},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	var rows []struct {
		PartyID   string    `bson:"party_id"`
		IntentID  string    `bson:"intent_id"`
		CreatedAt time.Time `bson:"created_at"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]app.Notification, len(rows))
	for i, r := range rows {
		out[i] = app.Notification(r)
	}
	return out, nil
}
