//intenza:paradigm imperative-shell

// Package httpapi is the HTTP surface: Huma v2 on the standard library mux,
// OpenAPI 3.1 at /openapi.json, errors as RFC 9457 problem details. Each
// handler translates a request into one use case and the use case's errors
// into status codes; nothing here decides business rules.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"golang.org/x/time/rate"

	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/domain/money"
)

// Limiter is the distributed rate limiter (Redis GCRA).
type Limiter interface {
	Allow(ctx context.Context, key string, rate float64, burst int) (bool, time.Duration, error)
}

// Deps is what the server needs.
type Deps struct {
	Market      *app.Market
	Limiter     Limiter // optional: nil means local limits only
	JWTSecret   []byte
	DevAuth     bool
	MaxInFlight int
	Log         *slog.Logger
	OnLimit     func(scope string, degraded bool) // metrics hook
	Wrap        func(http.Handler) http.Handler   // telemetry hook
}

// New builds the handler.
func New(d Deps) http.Handler {
	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("Intenza", "1.0.0")
	cfg.Info.Description = "Marketplace reverso C2B: intenções, propostas seladas, contrato & liquidação."
	cfg.CreateHooks = nil // no $schema links in responses
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer": {Type: "http", Scheme: "bearer", BearerFormat: "JWT"},
	}
	api := humago.New(mux, cfg)
	// Domain values with their own JSON shape get the schema of that shape.
	api.OpenAPI().Components.Schemas.RegisterTypeAlias(reflect.TypeFor[money.Money](), reflect.TypeFor[moneyWire]())
	s := &server{d: d, local: map[string]*rate.Limiter{}}
	api.UseMiddleware(s.authenticate)
	s.routes(api)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	var h http.Handler = mux
	h = s.shed(h)
	h = recoverer(d.Log, h)
	if d.Wrap != nil {
		h = d.Wrap(h)
	}
	return h
}

// moneyWire is the JSON shape of money.Money.
type moneyWire struct {
	Amount   string `json:"amount" example:"1234.56" pattern:"^-?[0-9]+(\\.[0-9]+)?$" doc:"Valor decimal na moeda"`
	Currency string `json:"currency" example:"BRL" doc:"Código ISO 4217"`
}

type server struct {
	d     Deps
	mu    sync.Mutex
	local map[string]*rate.Limiter
}

// shed refuses work beyond a fixed concurrency instead of queueing it: under
// overload a fast 503 keeps latency bounded for the requests that do run.
func (s *server) shed(next http.Handler) http.Handler {
	n := s.d.MaxInFlight
	if n <= 0 {
		n = 512
	}
	sem := make(chan struct{}, n)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
			next.ServeHTTP(w, r)
		default:
			w.Header().Set("Retry-After", "1")
			http.Error(w, `{"title":"Service Unavailable","status":503,"detail":"sobrecarga: tente de novo"}`, http.StatusServiceUnavailable)
		}
	})
}

func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.ErrorContext(r.Context(), "panic", "value", v, "path", r.URL.Path)
				http.Error(w, `{"title":"Internal Server Error","status":500}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// limit applies the distributed limiter and falls back to a per-node token
// bucket when Redis does not answer: limits get looser (per node instead of
// global) but never disappear.
func (s *server) limit(ctx context.Context, scope, who string, perSecond float64, burst int) error {
	key := scope + ":" + who
	if s.d.Limiter != nil {
		ok, wait, err := s.d.Limiter.Allow(ctx, key, perSecond, burst)
		if err == nil {
			if s.d.OnLimit != nil && !ok {
				s.d.OnLimit(scope, false)
			}
			if !ok {
				return tooMany(wait)
			}
			return nil
		}
		s.d.Log.WarnContext(ctx, "limitador distribuído indisponível, usando o local", "err", err)
	}
	s.mu.Lock()
	l, ok := s.local[key]
	if !ok {
		l = rate.NewLimiter(rate.Limit(perSecond), burst)
		s.local[key] = l
	}
	s.mu.Unlock()
	if !l.Allow() {
		if s.d.OnLimit != nil {
			s.d.OnLimit(scope, true)
		}
		return tooMany(time.Second)
	}
	return nil
}

func tooMany(wait time.Duration) error {
	secs := int(wait.Round(time.Second) / time.Second)
	if secs < 1 {
		secs = 1
	}
	return &retryError{huma.Error429TooManyRequests("limite de requisições atingido"), strconv.Itoa(secs)}
}

// retryError carries a Retry-After header with a 429.
type retryError struct {
	huma.StatusError
	after string
}

func (e *retryError) GetHeaders() http.Header { return http.Header{"Retry-After": {e.after}} }
