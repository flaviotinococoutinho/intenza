//intenza:paradigm imperative-shell

// Package escrow is the client of the authorized payment institution (in the
// lab, the simulator in internal/psp). Calls are bounded three ways: a
// per-attempt timeout, a circuit breaker over a rolling failure ratio, and
// retries with full jitter, safe because every command carries an
// idempotency key. Webhook signatures are verified here too.
package escrow

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/flaviotinococoutinho/intenza/internal/app"
)

// Client implements app.Escrow over HTTP.
type Client struct {
	base     string
	http     *http.Client
	timeout  time.Duration
	attempts int
	cb       *gobreaker.CircuitBreaker[[]byte]
	log      *slog.Logger
	OnCall   func(op, result string, d time.Duration) // metrics hook
	OnState  func(state int)                          // 0 closed, 1 half-open, 2 open
}

type statusError struct{ code int }

func (e statusError) Error() string { return "escrow: status " + strconv.Itoa(e.code) }

// New builds a client with a per-attempt timeout.
func New(base string, timeout time.Duration, log *slog.Logger) *Client {
	c := &Client{base: strings.TrimRight(base, "/"), http: &http.Client{}, timeout: timeout, attempts: 4, log: log}
	c.cb = gobreaker.NewCircuitBreaker[[]byte](gobreaker.Settings{
		Name:         "escrow",
		MaxRequests:  3,
		Interval:     30 * time.Second,
		BucketPeriod: 3 * time.Second,
		Timeout:      5 * time.Second,
		ReadyToTrip: func(n gobreaker.Counts) bool {
			return n.Requests >= 10 && float64(n.TotalFailures)/float64(n.Requests) >= 0.5
		},
		IsSuccessful: func(err error) bool {
			var se statusError
			return err == nil || (errors.As(err, &se) && se.code < 500)
		},
		OnStateChange: func(_ string, _, to gobreaker.State) {
			log.Warn("disjuntor do escrow mudou", "estado", to.String())
			if c.OnState != nil {
				c.OnState(int(to))
			}
		},
	})
	return c
}

func (c *Client) do(ctx context.Context, op, method, path, key string, in, out any) error {
	var payload []byte
	if in != nil {
		var err error
		if payload, err = json.Marshal(in); err != nil {
			return err
		}
	}
	var lastErr error
	for attempt := range c.attempts {
		start := time.Now()
		body, err := c.cb.Execute(func() ([]byte, error) { return c.once(ctx, method, path, key, payload) })
		result := "ok"
		switch {
		case err == nil:
		case errors.Is(err, gobreaker.ErrOpenState), errors.Is(err, gobreaker.ErrTooManyRequests):
			result = "breaker-open"
		default:
			result = "error"
		}
		if c.OnCall != nil {
			c.OnCall(op, result, time.Since(start))
		}
		if err == nil {
			if out != nil {
				return json.Unmarshal(body, out)
			}
			return nil
		}
		lastErr = err
		var se statusError
		if errors.As(err, &se) && se.code < 500 {
			return fmt.Errorf("escrow %s: %w", op, err) // client errors do not get better with retries
		}
		if attempt == c.attempts-1 {
			break
		}
		// full jitter: uniform in [0, base·2^attempt], capped
		limit := min(100*time.Millisecond<<attempt, 2*time.Second)
		select {
		case <-time.After(time.Duration(rand.Int64N(int64(limit)))):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("%w: escrow %s: %w", app.ErrUnavailable, op, lastErr)
}

func (c *Client) once(ctx context.Context, method, path, key string, payload []byte) ([]byte, error) {
	actx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, method, c.base+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, statusError{res.StatusCode}
	}
	return body, nil
}

type escrowDTO struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	PaymentCode string `json:"payment_code"`
}

// Open asks the agent to open an escrow.
func (c *Client) Open(ctx context.Context, r app.EscrowRequest) (app.EscrowRef, error) {
	var out escrowDTO
	err := c.do(ctx, "open", http.MethodPost, "/escrows", r.IdempotencyKey, map[string]any{
		"reference": r.Reference, "amount_minor": r.AmountMinor, "currency": r.Currency, "mcc": r.MCC,
		"payer": r.Payer, "payee": r.Payee,
	}, &out)
	return app.EscrowRef{ID: out.ID, PaymentCode: out.PaymentCode}, err
}

// Release commands the release to the seller.
func (c *Client) Release(ctx context.Context, id, key string) error {
	return c.do(ctx, "release", http.MethodPost, "/escrows/"+id+"/release", key, nil, nil)
}

// Refund commands the refund to the buyer.
func (c *Client) Refund(ctx context.Context, id, key string) error {
	return c.do(ctx, "refund", http.MethodPost, "/escrows/"+id+"/refund", key, nil, nil)
}

// Status reads the agent's view.
func (c *Client) Status(ctx context.Context, id string) (app.EscrowStatus, error) {
	var out escrowDTO
	err := c.do(ctx, "status", http.MethodGet, "/escrows/"+id, "", nil, &out)
	return app.EscrowStatus(out.Status), err
}

// Sign computes the webhook signature header for a body at instant t.
func Sign(secret []byte, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

var ErrSignature = errors.New("escrow: invalid webhook signature")

// Verify checks a webhook signature and its freshness (replay window).
func Verify(secret []byte, header string, body []byte, now time.Time, tolerance time.Duration) error {
	var ts, sig string
	for part := range strings.SplitSeq(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sig = v
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sig == "" {
		return ErrSignature
	}
	if d := now.Sub(time.Unix(sec, 0)); d > tolerance || d < -tolerance {
		return fmt.Errorf("%w: fora da janela de %s", ErrSignature, tolerance)
	}
	if !hmac.Equal([]byte(Sign(secret, time.Unix(sec, 0), body)), []byte("t="+ts+",v1="+sig)) {
		return ErrSignature
	}
	return nil
}
