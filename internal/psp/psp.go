//intenza:paradigm imperative-shell

// Package psp simulates the authorized payment institution that custodies the
// money. It is deliberately unreliable on demand: latency drawn from a
// lognormal distribution, random 503s, webhooks dropped, duplicated or
// delayed. Commands are idempotent by key, like a real institution's API,
// and every state change produces a signed webhook delivered with retries.
package psp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/adapter/escrow"
	"github.com/flaviotinococoutinho/intenza/standards/bcb/brcode"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso18245"
)

// Escrow is one custody account.
type Escrow struct {
	ID          string    `json:"id"`
	Reference   string    `json:"reference"`
	Status      string    `json:"status"` // pending | held | released | refunded
	AmountMinor int64     `json:"amount_minor"`
	Currency    string    `json:"currency"`
	MCC         string    `json:"mcc"`
	Payer       string    `json:"payer"`
	Payee       string    `json:"payee"`
	PaymentCode string    `json:"payment_code"`
	CreatedAt   time.Time `json:"created_at"`
}

// Faults are the knobs of the simulator.
type Faults struct {
	LatencyMedian time.Duration // lognormal median of each API call
	LatencySigma  float64       // lognormal shape: 0 disables the tail
	ErrorRate     float64       // probability of a 503
	WebhookDrop   float64       // probability a webhook is never sent
	WebhookDup    float64       // probability it is sent twice
	WebhookDelay  time.Duration // extra delay before sending
	AutoPayAfter  time.Duration // pay pending escrows automatically (0 = off)
}

// Server is the simulator.
type Server struct {
	mu      sync.Mutex
	escrows map[string]*Escrow
	byKey   map[string]string
	seq     int
	faults  Faults
	rng     *rand.Rand

	WebhookURL string
	Secret     []byte
	Log        *slog.Logger
	client     *http.Client
	hooks      chan hook
}

type hook struct {
	body []byte
}

// New builds a simulator with a seeded random source.
func New(webhookURL string, secret []byte, seed uint64, log *slog.Logger) *Server {
	return &Server{
		escrows: map[string]*Escrow{}, byKey: map[string]string{}, rng: rand.New(rand.NewPCG(seed, seed+1)),
		WebhookURL: webhookURL, Secret: secret, Log: log, client: &http.Client{Timeout: 3 * time.Second}, hooks: make(chan hook, 4096),
	}
}

// SetFaults replaces the knobs.
func (s *Server) SetFaults(f Faults) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults = f
}

// GetFaults returns the knobs.
func (s *Server) GetFaults() Faults {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.faults
}

func (s *Server) chance(p float64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return p > 0 && s.rng.Float64() < p
}

// latency draws exp(μ + σZ) with μ = ln(median): a long right tail when σ grows.
func (s *Server) latency() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.faults.LatencyMedian <= 0 {
		return 0
	}
	mu := math.Log(float64(s.faults.LatencyMedian))
	return time.Duration(math.Exp(mu + s.faults.LatencySigma*s.rng.NormFloat64()))
}

// Handler is the HTTP API of the simulator.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /escrows", s.open)
	mux.HandleFunc("GET /escrows/{id}", s.get)
	mux.HandleFunc("POST /escrows/{id}/pay", s.transition("pending", "held", "escrow.held"))
	mux.HandleFunc("POST /escrows/{id}/release", s.transition("held", "released", "escrow.released"))
	mux.HandleFunc("POST /escrows/{id}/refund", s.transition("held", "refunded", "escrow.refunded"))
	return s.faulty(mux)
}

func (s *Server) faulty(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(s.latency())
		if s.chance(s.GetFaults().ErrorRate) {
			http.Error(w, `{"error":"indisponível (falha injetada)"}`, http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	var in Escrow
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || key == "" || in.AmountMinor <= 0 || in.Currency != "BRL" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pedido inválido (chave, valor em BRL)"})
		return
	}
	s.mu.Lock()
	if id, ok := s.byKey[key]; ok {
		e := *s.escrows[id]
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, e)
		return
	}
	s.seq++
	e := &Escrow{
		ID: fmt.Sprintf("esc-%06d", s.seq), Reference: in.Reference, Status: "pending", AmountMinor: in.AmountMinor,
		Currency: in.Currency, MCC: in.MCC, Payer: in.Payer, Payee: in.Payee, CreatedAt: time.Now(),
	}
	s.escrows[e.ID], s.byKey[key] = e, e.ID
	s.mu.Unlock()
	mcc, _ := iso18245.Parse(in.MCC)
	code, err := brcode.Encode(brcode.Pix{
		Location: "psp.intenza.local/qr/" + e.ID, MCC: mcc, AmountCents: in.AmountMinor,
		MerchantName: "INTENZA CUSTODIA", MerchantCity: "SAO PAULO", TxID: e.ID[len(e.ID)-6:], Dynamic: true,
	})
	if err == nil {
		s.mu.Lock()
		e.PaymentCode = code
		s.mu.Unlock()
	}
	s.mu.Lock()
	out := *e
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	e, ok := s.escrows[r.PathValue("id")]
	var out Escrow
	if ok {
		out = *e
	}
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "escrow inexistente"})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// transition moves an escrow; repeating a transition already done is a no-op
// that answers 200 (idempotency), a transition from another state is a 409.
func (s *Server) transition(from, to, hookType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		e, ok := s.escrows[r.PathValue("id")]
		if !ok {
			s.mu.Unlock()
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "escrow inexistente"})
			return
		}
		switch e.Status {
		case to:
			out := *e
			s.mu.Unlock()
			writeJSON(w, http.StatusOK, out)
			return
		case from:
			e.Status = to
		default:
			st := e.Status
			s.mu.Unlock()
			writeJSON(w, http.StatusConflict, map[string]string{"error": "escrow em " + st})
			return
		}
		out := *e
		s.mu.Unlock()
		s.emit(hookType, out)
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Server) emit(typ string, e Escrow) {
	body, _ := json.Marshal(map[string]any{
		"id": e.ID + ":" + typ, "type": typ, "escrow_id": e.ID, "reference": e.Reference, "status": e.Status,
		"occurred_at": time.Now().UTC(),
	})
	select {
	case s.hooks <- hook{body: body}:
	default:
		s.Log.Warn("fila de webhooks cheia, descartando", "escrow", e.ID)
	}
}

// Run delivers webhooks and auto-pays pending escrows until ctx ends.
func (s *Server) Run(ctx context.Context) {
	go s.autopay(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case h := <-s.hooks:
			f := s.GetFaults()
			if s.chance(f.WebhookDrop) {
				s.Log.Info("webhook descartado (falha injetada)")
				continue
			}
			copies := 1
			if s.chance(f.WebhookDup) {
				copies = 2
			}
			go func() {
				time.Sleep(f.WebhookDelay)
				for range copies {
					s.deliver(ctx, h.body)
				}
			}()
		}
	}
}

func (s *Server) deliver(ctx context.Context, body []byte) {
	for attempt := range 6 {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.WebhookURL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Intenza-Signature", escrow.Sign(s.Secret, time.Now(), body))
		res, err := s.client.Do(req)
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode < 300 {
				return
			}
		}
		select {
		case <-time.After(time.Duration(500<<attempt) * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
	s.Log.Warn("webhook desistido depois de 6 tentativas")
}

func (s *Server) autopay(ctx context.Context) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		after := s.GetFaults().AutoPayAfter
		if after <= 0 {
			continue
		}
		var due []Escrow
		s.mu.Lock()
		for _, e := range s.escrows {
			if e.Status == "pending" && time.Since(e.CreatedAt) >= after {
				e.Status = "held"
				due = append(due, *e)
			}
		}
		s.mu.Unlock()
		for _, e := range due {
			s.emit("escrow.held", e)
		}
	}
}

// ParseFaults reads knobs from strings (admin endpoint).
func ParseFaults(cur Faults, name, value string) (Faults, error) {
	var err error
	switch name {
	case "latency_median":
		cur.LatencyMedian, err = time.ParseDuration(value)
	case "latency_sigma":
		cur.LatencySigma, err = strconv.ParseFloat(value, 64)
	case "error_rate":
		cur.ErrorRate, err = strconv.ParseFloat(value, 64)
	case "webhook_drop":
		cur.WebhookDrop, err = strconv.ParseFloat(value, 64)
	case "webhook_dup":
		cur.WebhookDup, err = strconv.ParseFloat(value, 64)
	case "webhook_delay":
		cur.WebhookDelay, err = time.ParseDuration(value)
	case "auto_pay_after":
		cur.AutoPayAfter, err = time.ParseDuration(value)
	default:
		err = fmt.Errorf("falha desconhecida %q", name)
	}
	return cur, err
}
