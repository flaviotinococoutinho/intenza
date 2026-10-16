package chaos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/flaviotinococoutinho/intenza/standards/rfb/cpf"
)

// scenario runs a built-in multi-step probe and returns a number to compare
// with the bounds (1 = property held, counts for violations).
func (r *Runner) scenario(ctx context.Context, name string) (float64, error) {
	switch name {
	case "sealed-under-skew":
		return r.sealedUnderSkew(ctx)
	case "idempotent-publish":
		return r.idempotentPublish(ctx)
	case "late-bids":
		return r.lateBids(ctx)
	}
	return 0, fmt.Errorf("cenário desconhecido %q", name)
}

func (r *Runner) api(ctx context.Context, method, path, token string, in, out any, headers ...string) (int, error) {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(ctx, method, r.Env.API+path, body)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := r.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if out != nil && res.StatusCode < 300 {
		_ = json.Unmarshal(raw, out)
	}
	return res.StatusCode, nil
}

func (r *Runner) party(ctx context.Context, name string) (token string, err error) {
	base := fmt.Sprintf("%09d", 100000000+time.Now().UnixNano()%899999999)
	d1, d2, _ := cpf.CheckDigits(base)
	var out struct {
		Token string `json:"token"`
	}
	code, err := r.api(ctx, http.MethodPost, "/v1/dev/parties", "", map[string]any{"name": name, "document": base + string([]byte{d1, d2})}, &out)
	if err != nil || code != http.StatusCreated {
		return "", fmt.Errorf("cadastro: %d: %w", code, err)
	}
	return out.Token, nil
}

var cardItem = map[string]any{"game": "pokemon", "set_code": "BS1", "card_number": "4", "language": "pt", "condition": "NM", "grading_company": "none"}

// sealedUnderSkew: with the API clock pushed into the future, the intent
// displays as closed but the buyer still cannot read the price. Returns 1 when
// the seal held, 0 when the price leaked.
func (r *Runner) sealedUnderSkew(ctx context.Context) (float64, error) {
	buyer, err := r.party(ctx, "comprador do relógio")
	if err != nil {
		return 0, err
	}
	seller, err := r.party(ctx, "vendedor do relógio")
	if err != nil {
		return 0, err
	}
	var in struct {
		ID       string    `json:"id"`
		SelectBy time.Time `json:"select_by"`
	}
	if code, err := r.api(ctx, http.MethodPost, "/v1/intents", buyer, map[string]any{
		"category_id": "collectibles.tcg", "title": "carta sob relógio adiantado",
		"constraints": map[string]any{}, "delivery_cep": "01310-100", "window": "PT10M",
	}, &in); err != nil || code != http.StatusCreated {
		return 0, fmt.Errorf("publicação: %d: %w", code, err)
	}
	if code, err := r.api(ctx, http.MethodPost, "/v1/intents/"+in.ID+"/proposals", seller, map[string]any{
		"attributes": cardItem,
		"price":      map[string]any{"amount": "777.00", "currency": "BRL"}, "lead_time_days": 2,
		"valid_until": in.SelectBy.Add(time.Hour).Format(time.RFC3339),
	}, nil); err != nil || code != http.StatusCreated {
		return 0, fmt.Errorf("proposta: %d: %w", code, err)
	}
	var view struct {
		EffectiveStatus string `json:"effective_status"`
	}
	if _, err := r.api(ctx, http.MethodGet, "/v1/intents/"+in.ID, "", nil, &view); err != nil {
		return 0, err
	}
	// closed or expired both mean the local clock is past the deadline that
	// the database has not reached; anything else and the fault is not there
	if view.EffectiveStatus != "closed" && view.EffectiveStatus != "expired" {
		return 0, inconclusive("o relógio da api não estava adiantado (efetivo %q)", view.EffectiveStatus)
	}
	var ps []map[string]any
	if _, err := r.api(ctx, http.MethodGet, "/v1/intents/"+in.ID+"/proposals", buyer, nil, &ps); err != nil {
		return 0, err
	}
	for _, p := range ps {
		if _, leaked := p["total_price"]; leaked {
			return 0, nil
		}
	}
	return 1, nil
}

// idempotentPublish publishes twice with the same key: 1 when both answers
// carry the same intent.
func (r *Runner) idempotentPublish(ctx context.Context) (float64, error) {
	buyer, err := r.party(ctx, "comprador idempotente")
	if err != nil {
		return 0, err
	}
	key := fmt.Sprintf("chaos-%d", time.Now().UnixNano())
	body := map[string]any{
		"category_id": "collectibles.coins", "title": "moeda para a sonda", "constraints": map[string]any{},
		"delivery_cep": "20040-020", "window": "PT1H",
	}
	var a, b struct {
		ID string `json:"id"`
	}
	c1, err := r.api(ctx, http.MethodPost, "/v1/intents", buyer, body, &a, "Idempotency-Key", key)
	if err != nil {
		return 0, err
	}
	c2, err := r.api(ctx, http.MethodPost, "/v1/intents", buyer, body, &b, "Idempotency-Key", key)
	if err != nil {
		return 0, err
	}
	if c1 != http.StatusCreated || c2 != http.StatusOK || a.ID == "" || a.ID != b.ID {
		return 0, nil
	}
	return 1, nil
}

// lateBids counts proposals stored with submitted_at at or after the
// deadline of their intent (both read back from the database).
func (r *Runner) lateBids(ctx context.Context) (float64, error) {
	if r.mongo == nil {
		return 0, fmt.Errorf("sem mongo")
	}
	db := r.mongo.Database("intenza")
	deadlines := map[string]time.Time{}
	cur, err := db.Collection("intents").Find(ctx, bson.D{})
	if err != nil {
		return 0, err
	}
	var intents []struct {
		ID       string    `bson:"_id"`
		ClosesAt time.Time `bson:"closes_at"`
	}
	if err := cur.All(ctx, &intents); err != nil {
		return 0, err
	}
	for _, in := range intents {
		deadlines[in.ID] = in.ClosesAt
	}
	cur, err = db.Collection("proposals").Find(ctx, bson.D{})
	if err != nil {
		return 0, err
	}
	var docs []struct {
		Body string `bson:"body"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return 0, err
	}
	late := 0
	for _, d := range docs {
		var p struct {
			IntentID    string    `json:"intent_id"`
			SubmittedAt time.Time `json:"submitted_at"`
		}
		if err := json.NewDecoder(strings.NewReader(d.Body)).Decode(&p); err != nil {
			continue
		}
		if dl, ok := deadlines[p.IntentID]; ok && !p.SubmittedAt.Before(dl) {
			late++
		}
	}
	return float64(late), nil
}
