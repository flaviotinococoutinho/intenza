//intenza:paradigm imperative-shell

// Package load drives the stochastic market model (package market) against
// the public API: it turns the model's draws into HTTP calls at the right
// moments. Items and constraints come from the category schemas. Everything
// random comes from one seeded source, so the sequence of decisions is
// reproducible even when the system's timing is not.
//
// What it measures is what a person on the other side feels: latency and
// status of every call, per operation.
package load

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/chaos/market"
	"github.com/flaviotinococoutinho/intenza/internal/domain/schema/schemagen"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso8601"
	"github.com/flaviotinococoutinho/intenza/standards/rfb/cpf"
)

// Config shapes the market.
type Config struct {
	API        string
	Rate       float64       // intents per second
	Buyers     int           // default 8
	Sellers    int           // default 6
	ProposeP   float64       // chance a seller answers
	Window     string        // ISO 8601 bidding window
	Snipe      time.Duration // mean lead before the deadline
	Duration   time.Duration // generation time
	Settlement bool          // drive agreements to release
	Seed       uint64
	Log        *slog.Logger
}

// Summary is the client-side view of one operation.
type Summary struct {
	Count        int     `json:"count"`
	P50          float64 `json:"p50_seconds"`
	P95          float64 `json:"p95_seconds"`
	P99          float64 `json:"p99_seconds"`
	ServerErrors int     `json:"server_errors"`
	ClientErrors int     `json:"client_errors"`
	Transport    int     `json:"transport_errors"`
	ErrorRatio   float64 `json:"error_ratio"` // (5xx + transport) / count
}

type opStats struct {
	durations []time.Duration
	status    map[int]int
	transport int
}

// Stats accumulates measurements.
type Stats struct {
	mu  sync.Mutex
	ops map[string]*opStats
}

func (s *Stats) record(op string, d time.Duration, status int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.ops[op]
	if o == nil {
		o = &opStats{status: map[int]int{}}
		s.ops[op] = o
	}
	o.durations = append(o.durations, d)
	if err != nil {
		o.transport++
		return
	}
	o.status[status]++
}

func quantile(sorted []time.Duration, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(q*float64(len(sorted)))) - 1
	return sorted[max(0, min(i, len(sorted)-1))].Seconds()
}

// Summaries returns one summary per operation plus "all".
func (s *Stats) Summaries() map[string]Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]Summary{}
	all := &opStats{status: map[int]int{}}
	add := func(name string, o *opStats) {
		d := slices.Clone(o.durations)
		slices.Sort(d)
		sum := Summary{Count: len(d), P50: quantile(d, .5), P95: quantile(d, .95), P99: quantile(d, .99), Transport: o.transport}
		for code, n := range o.status {
			switch {
			case code >= 500:
				sum.ServerErrors += n
			case code >= 400:
				sum.ClientErrors += n
			}
		}
		if sum.Count > 0 {
			sum.ErrorRatio = float64(sum.ServerErrors+sum.Transport) / float64(sum.Count)
		}
		out[name] = sum
	}
	for name, o := range s.ops {
		add(name, o)
		all.durations = append(all.durations, o.durations...)
		all.transport += o.transport
		for c, n := range o.status {
			all.status[c] += n
		}
	}
	add("all", all)
	return out
}

type party struct{ id, token string }

type client struct {
	cfg   Config
	http  *http.Client
	stats *Stats
	mu    sync.Mutex
	model *market.Model
}

// draw runs fn with exclusive access to the model (rand.Rand is not concurrent).
func draw[T any](c *client, fn func(*market.Model) T) T {
	c.mu.Lock()
	defer c.mu.Unlock()
	return fn(c.model)
}

func (c *client) call(ctx context.Context, op, method, path, token string, in, out any, headers ...string) int {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.API+path, body)
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	start := time.Now()
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			c.stats.record(op, time.Since(start), 0, err)
		}
		return 0
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	c.stats.record(op, time.Since(start), res.StatusCode, nil)
	if out != nil && res.StatusCode < 300 {
		_ = json.Unmarshal(raw, out)
	}
	return res.StatusCode
}

func (c *client) register(ctx context.Context, i int, seed uint64) (party, error) {
	base := fmt.Sprintf("%09d", 100000000+int((seed*7919+uint64(i)*104729)%899999999))
	d1, d2, _ := cpf.CheckDigits(base)
	var out struct {
		Party struct {
			ID string `json:"id"`
		} `json:"party"`
		Token string `json:"token"`
	}
	code := c.call(ctx, "register", http.MethodPost, "/v1/dev/parties", "", map[string]any{
		"name": "parte " + strconv.Itoa(i), "document": base + string([]byte{d1, d2}),
	}, &out)
	if code != http.StatusCreated {
		return party{}, fmt.Errorf("cadastro %d: status %d", i, code)
	}
	return party{out.Party.ID, out.Token}, nil
}

// Run generates load until cfg.Duration and returns the measurements once
// every started lifecycle finished (or ctx ended).
func Run(ctx context.Context, cfg Config) (*Stats, error) {
	if cfg.Buyers == 0 {
		cfg.Buyers = 8
	}
	if cfg.Sellers == 0 {
		cfg.Sellers = 6
	}
	if cfg.Window == "" {
		cfg.Window = "PT12S"
	}
	if cfg.Snipe == 0 {
		cfg.Snipe = 3 * time.Second
	}
	window, err := iso8601.Parse(cfg.Window)
	if err != nil {
		return nil, err
	}
	c := &client{
		cfg: cfg, http: &http.Client{Timeout: 10 * time.Second}, stats: &Stats{ops: map[string]*opStats{}},
		model: market.New(rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x5bd1e995)), taxonomy.Default, 1, cfg.Rate, cfg.ProposeP, cfg.Snipe),
	}
	var buyers, sellers []party
	for i := range cfg.Buyers + cfg.Sellers {
		p, err := c.register(ctx, i, cfg.Seed)
		if err != nil {
			return c.stats, err
		}
		if i < cfg.Buyers {
			buyers = append(buyers, p)
		} else {
			sellers = append(sellers, p)
		}
	}
	gen, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()
	var wg sync.WaitGroup
	for n := 0; ; n++ {
		select {
		case <-gen.Done():
			wg.Wait()
			return c.stats, nil
		case <-time.After(draw(c, (*market.Model).NextArrival)):
		}
		cat := draw(c, (*market.Model).Category)
		buyer := buyers[draw(c, func(m *market.Model) int { return m.Pick(len(buyers)) })]
		fork := draw(c, (*market.Model).Fork)
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.lifecycle(ctx, n, cat, buyer, sellers, window, fork)
		}()
	}
}

type intentResp struct {
	ID       string    `json:"id"`
	Status   string    `json:"status"`
	ClosesAt time.Time `json:"closes_at"`
	SelectBy time.Time `json:"select_by"`
}

func (c *client) lifecycle(ctx context.Context, n int, cat taxonomy.Category, buyer party, sellers []party, window iso8601.Duration, r *rand.Rand) {
	m := market.New(r, taxonomy.Default, 1, c.cfg.Rate, c.cfg.ProposeP, c.cfg.Snipe)
	s := cat.Schema()
	reference := schemagen.Facts(r, s, 0.4)
	constraints := schemagen.Constraints(r, s, reference, 0.3, 0)
	var in intentResp
	if c.call(ctx, "publish", http.MethodPost, "/v1/intents", buyer.token, map[string]any{
		"category_id": cat.ID(), "title": "procura " + cat.Label(), "constraints": constraints,
		"delivery_cep": "01310-100", "window": window.String(),
	}, &in, "Idempotency-Key", fmt.Sprintf("load-%d-%d", c.cfg.Seed, n)) >= 300 {
		return
	}
	var wg sync.WaitGroup
	for _, seller := range sellers {
		if !m.Answers() {
			continue
		}
		at := in.ClosesAt.Add(-m.SnipeLead())
		facts := schemagen.Facts(r, s, 0.5)
		for k, v := range reference { // sellers mostly bring what was asked for
			if m.Keep(0.7) {
				facts[k] = v
			}
		}
		price := m.PriceCents()
		body := map[string]any{
			"attributes": facts, "price": map[string]any{"amount": fmt.Sprintf("%d.%02d", price/100, price%100), "currency": "BRL"},
			"lead_time_days": m.LeadDays(), "valid_until": in.SelectBy.Add(time.Hour).Format(time.RFC3339),
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-time.After(time.Until(at)):
			case <-ctx.Done():
				return
			}
			c.call(ctx, "propose", http.MethodPost, "/v1/intents/"+in.ID+"/proposals", seller.token, body, nil)
		}()
	}
	wg.Wait()
	if !c.waitStatus(ctx, in, "closed", time.Until(in.ClosesAt)+45*time.Second) {
		return
	}
	var ranking []struct {
		ProposalID string `json:"proposal_id"`
		Compliant  bool   `json:"compliant"`
	}
	if c.call(ctx, "rank", http.MethodGet, "/v1/intents/"+in.ID+"/ranking", buyer.token, nil, &ranking) >= 300 || len(ranking) == 0 {
		return
	}
	var agr struct {
		ID string `json:"id"`
	}
	if c.call(ctx, "award", http.MethodPost, "/v1/intents/"+in.ID+"/award", buyer.token, map[string]any{"proposal_id": ranking[0].ProposalID}, &agr) >= 300 {
		return
	}
	if c.cfg.Settlement {
		c.settle(ctx, agr.ID, buyer, sellers)
	}
}

func (c *client) waitStatus(ctx context.Context, in intentResp, want string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		var cur intentResp
		if c.call(ctx, "poll", http.MethodGet, "/v1/intents/"+in.ID, "", nil, &cur) == http.StatusOK && cur.Status == want {
			return true
		}
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return false
		}
	}
	return false
}

func (c *client) settle(ctx context.Context, id string, buyer party, sellers []party) {
	status := func() (string, string) {
		var a struct {
			Status string `json:"status"`
			Terms  struct {
				SellerID string `json:"seller_id"`
			} `json:"terms"`
		}
		c.call(ctx, "poll", http.MethodGet, "/v1/agreements/"+id, buyer.token, nil, &a)
		return a.Status, a.Terms.SellerID
	}
	wait := func(want string, within time.Duration) (string, bool) {
		deadline := time.Now().Add(within)
		for time.Now().Before(deadline) {
			st, seller := status()
			if st == want {
				return seller, true
			}
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return "", false
			}
		}
		return "", false
	}
	sellerID, ok := wait("funded", 90*time.Second)
	if !ok {
		return
	}
	var seller party
	for _, s := range sellers {
		if s.id == sellerID {
			seller = s
		}
	}
	c.call(ctx, "ship", http.MethodPost, "/v1/agreements/"+id+"/ship", seller.token, map[string]any{"tracking": "BR" + id[:8]}, nil)
	c.call(ctx, "deliver", http.MethodPost, "/v1/agreements/"+id+"/deliver", buyer.token, nil, nil)
	c.call(ctx, "accept", http.MethodPost, "/v1/agreements/"+id+"/accept", buyer.token, nil, nil)
	if _, ok := wait("released", 120*time.Second); ok {
		c.stats.record("released", 0, http.StatusOK, nil)
	}
}
