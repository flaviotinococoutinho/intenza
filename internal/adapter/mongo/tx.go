package mongo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/domain/agreement"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/proposal"
	"github.com/flaviotinococoutinho/intenza/internal/domain/reputation"
)

type tx struct{ s *Store }

// Now reads the server's clock. It runs outside the session (hello is not
// allowed inside a transaction), with its own short deadline.
func (t *tx) Now(ctx context.Context) (time.Time, error) {
	hctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if d, ok := ctx.Deadline(); ok {
		hctx, cancel = context.WithDeadline(context.Background(), d)
		defer cancel()
	}
	var res struct {
		LocalTime time.Time `bson:"localTime"`
	}
	if err := t.s.db.RunCommand(hctx, bson.D{{Key: "hello", Value: 1}}).Decode(&res); err != nil {
		return time.Time{}, err
	}
	return res.LocalTime.UTC(), nil
}

func (t *tx) get(ctx context.Context, coll, id string, dst any) error {
	var d bodyDoc
	if err := t.s.db.Collection(coll).FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&d); err != nil {
		return translate(err)
	}
	return json.Unmarshal([]byte(d.Body), dst)
}

func body(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func (t *tx) insert(ctx context.Context, coll string, doc bson.D) error {
	_, err := t.s.db.Collection(coll).InsertOne(ctx, doc)
	return translate(err)
}

// save replaces projection and body if the stored version is still prev.
func (t *tx) save(ctx context.Context, coll, id string, prev int64, extra bson.D, set bson.D) error {
	filter := append(bson.D{{Key: "_id", Value: id}, {Key: "version", Value: prev}}, extra...)
	res, err := t.s.db.Collection(coll).UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: set}})
	if err != nil {
		return translate(err)
	}
	if res.MatchedCount == 1 {
		return nil
	}
	var cur struct {
		Version int64 `bson:"version"`
	}
	if err := t.s.db.Collection(coll).FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&cur); err != nil {
		return translate(err)
	}
	if cur.Version != prev {
		return app.ErrConflict
	}
	return errGuard
}

// errGuard means the version matched but the clock guard did not.
var errGuard = errors.New("mongo: clock guard not satisfied")

func intentDoc(in intent.Intent) (bson.D, error) {
	b, err := body(in)
	return bson.D{
		{Key: "_id", Value: in.ID},
		{Key: "version", Value: in.Version},
		{Key: "status", Value: string(in.Status)},
		{Key: "category_id", Value: in.CategoryID},
		{Key: "buyer_id", Value: in.BuyerID},
		{Key: "opened_at", Value: in.OpenedAt},
		{Key: "closes_at", Value: in.ClosesAt},
		{Key: "select_by", Value: in.SelectBy},
		{Key: "proposal_count", Value: in.ProposalCount},
		{Key: "body", Value: b},
	}, err
}

func (t *tx) Intent(ctx context.Context, id string) (intent.Intent, error) {
	var in intent.Intent
	return in, t.get(ctx, cIntents, id, &in)
}

func (t *tx) InsertIntent(ctx context.Context, in intent.Intent) error {
	doc, err := intentDoc(in)
	if err != nil {
		return err
	}
	return t.insert(ctx, cIntents, doc)
}

func guardFilter(g app.Guard) bson.D {
	expr := func(op, field string) bson.D {
		return bson.D{{Key: "$expr", Value: bson.D{{Key: op, Value: bson.A{field, "$$NOW"}}}}}
	}
	switch g {
	case app.GuardOpenBeforeEnd:
		return append(bson.D{{Key: "status", Value: "open"}}, expr("$gt", "$closes_at")...)
	case app.GuardDeadlinePassed:
		return expr("$lte", "$closes_at")
	case app.GuardSelectionOver:
		return expr("$lte", "$select_by")
	case app.GuardNone:
	}
	return nil
}

func (t *tx) SaveIntent(ctx context.Context, in intent.Intent, prev int64, g app.Guard) error {
	doc, err := intentDoc(in)
	if err != nil {
		return err
	}
	err = t.save(ctx, cIntents, in.ID, prev, guardFilter(g), doc[1:])
	if errors.Is(err, errGuard) && g == app.GuardOpenBeforeEnd {
		return intent.ErrNotOpen
	}
	if errors.Is(err, errGuard) {
		return intent.ErrTooEarly
	}
	return err
}

func proposalDoc(p proposal.Proposal) (bson.D, error) {
	b, err := body(p)
	return bson.D{
		{Key: "_id", Value: p.ID},
		{Key: "version", Value: p.Version},
		{Key: "status", Value: string(p.Status)},
		{Key: "intent_id", Value: p.IntentID},
		{Key: "seller_id", Value: p.SellerID},
		{Key: "body", Value: b},
	}, err
}

func (t *tx) Proposal(ctx context.Context, id string) (proposal.Proposal, error) {
	var p proposal.Proposal
	return p, t.get(ctx, cProposals, id, &p)
}

func (t *tx) Proposals(ctx context.Context, intentID string) ([]proposal.Proposal, error) {
	cur, err := t.s.db.Collection(cProposals).Find(ctx, bson.D{{Key: "intent_id", Value: intentID}}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	return decodeAll[proposal.Proposal](ctx, cur)
}

func (t *tx) InsertProposal(ctx context.Context, p proposal.Proposal) error {
	doc, err := proposalDoc(p)
	if err != nil {
		return err
	}
	return t.insert(ctx, cProposals, doc)
}

func (t *tx) SaveProposal(ctx context.Context, p proposal.Proposal, prev int64) error {
	doc, err := proposalDoc(p)
	if err != nil {
		return err
	}
	return t.save(ctx, cProposals, p.ID, prev, nil, doc[1:])
}

func agreementDoc(a agreement.Agreement) (bson.D, error) {
	b, err := body(a)
	doc := bson.D{
		{Key: "_id", Value: a.ID},
		{Key: "version", Value: a.Version},
		{Key: "status", Value: string(a.Status)},
		{Key: "intent_id", Value: a.Terms.IntentID},
		{Key: "buyer_id", Value: a.Terms.BuyerID},
		{Key: "seller_id", Value: a.Terms.SellerID},
		{Key: "escrow_id", Value: a.EscrowID},
		{Key: "fund_by", Value: a.FundBy},
		{Key: "updated_at", Value: time.Now()},
		{Key: "body", Value: b},
	}
	if a.InspectUntil != nil {
		doc = append(doc, bson.E{Key: "inspect_until", Value: *a.InspectUntil})
	}
	return doc, err
}

func (t *tx) Agreement(ctx context.Context, id string) (agreement.Agreement, error) {
	var a agreement.Agreement
	return a, t.get(ctx, cAgreements, id, &a)
}

func (t *tx) InsertAgreement(ctx context.Context, a agreement.Agreement) error {
	doc, err := agreementDoc(a)
	if err != nil {
		return err
	}
	return t.insert(ctx, cAgreements, doc)
}

func (t *tx) SaveAgreement(ctx context.Context, a agreement.Agreement, prev int64) error {
	doc, err := agreementDoc(a)
	if err != nil {
		return err
	}
	return t.save(ctx, cAgreements, a.ID, prev, nil, doc[1:])
}

func (t *tx) Party(ctx context.Context, id string) (app.Party, error) {
	var p app.Party
	return p, t.get(ctx, cParties, id, &p)
}

func (t *tx) InsertParty(ctx context.Context, p app.Party) error {
	b, err := body(p)
	if err != nil {
		return err
	}
	creds := make([]string, len(p.Credentials))
	for i, c := range p.Credentials {
		creds[i] = string(c)
	}
	return t.insert(ctx, cParties, bson.D{
		{Key: "_id", Value: p.ID},
		{Key: "document", Value: p.Document},
		{Key: "credentials", Value: creds},
		{Key: "body", Value: b},
	})
}

func (t *tx) RatingPair(ctx context.Context, agreementID string) (reputation.Pair, error) {
	var p reputation.Pair
	return p, t.get(ctx, cRatingPairs, agreementID, &p)
}

func (t *tx) SaveRatingPair(ctx context.Context, p reputation.Pair) error {
	b, err := body(p)
	if err != nil {
		return err
	}
	_, err = t.s.db.Collection(cRatingPairs).UpdateOne(ctx, bson.D{{Key: "_id", Value: p.AgreementID}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "body", Value: b}}}}, options.UpdateOne().SetUpsert(true))
	return translate(err)
}

func (t *tx) InsertRatings(ctx context.Context, rs []reputation.Rating) error {
	docs := make([]any, len(rs))
	for i, r := range rs {
		docs[i] = bson.D{
			{Key: "agreement_id", Value: r.AgreementID},
			{Key: "rater_id", Value: r.RaterID},
			{Key: "ratee_id", Value: r.RateeID},
			{Key: "role", Value: string(r.Role)},
			{Key: "stars", Value: r.Stars},
			{Key: "submitted_at", Value: r.SubmittedAt},
		}
	}
	_, err := t.s.db.Collection(cRatings).InsertMany(ctx, docs)
	return translate(err)
}

func (t *tx) Ratings(ctx context.Context, rateeID string, role reputation.Role) ([]reputation.Rating, error) {
	cur, err := t.s.db.Collection(cRatings).Find(ctx, bson.D{{Key: "ratee_id", Value: rateeID}, {Key: "role", Value: string(role)}})
	if err != nil {
		return nil, err
	}
	var rows []struct {
		AgreementID string    `bson:"agreement_id"`
		RaterID     string    `bson:"rater_id"`
		RateeID     string    `bson:"ratee_id"`
		Role        string    `bson:"role"`
		Stars       int       `bson:"stars"`
		SubmittedAt time.Time `bson:"submitted_at"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]reputation.Rating, len(rows))
	for i, r := range rows {
		out[i] = reputation.Rating{
			AgreementID: r.AgreementID, RaterID: r.RaterID, RateeID: r.RateeID,
			Role: reputation.Role(r.Role), Stars: r.Stars, SubmittedAt: r.SubmittedAt,
		}
	}
	return out, nil
}

// Outbox writes the events in the same transaction as the state change.
func (t *tx) Outbox(ctx context.Context, evs []app.Envelope) error {
	if len(evs) == 0 {
		return nil
	}
	docs := make([]any, len(evs))
	for i, e := range evs {
		data, err := json.Marshal(e.Data)
		if err != nil {
			return err
		}
		docs[i] = bson.D{
			{Key: "_id", Value: e.ID},
			{Key: "type", Value: e.Type},
			{Key: "subject", Value: e.Subject},
			{Key: "time", Value: e.Time},
			{Key: "data", Value: string(data)},
			{Key: "traceparent", Value: e.TraceParent},
			{Key: "published", Value: false},
		}
	}
	_, err := t.s.db.Collection(cOutbox).InsertMany(ctx, docs)
	return translate(err)
}
