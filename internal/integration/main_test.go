//go:build integration

// Package integration runs the use cases against real MongoDB (replica set)
// and Redis in containers. go test -tags=integration ./internal/integration
package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/flaviotinococoutinho/intenza/internal/adapter/httpapi"
	"github.com/flaviotinococoutinho/intenza/internal/adapter/mongo"
	"github.com/flaviotinococoutinho/intenza/internal/adapter/redis"
	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/domain/agreement"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/proposal"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
	"github.com/flaviotinococoutinho/intenza/internal/platform/clock"
	"github.com/flaviotinococoutinho/intenza/internal/platform/ids"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso8601"
	"github.com/flaviotinococoutinho/intenza/standards/rfb/cnpj"
	"github.com/flaviotinococoutinho/intenza/standards/rfb/cpf"
)

var (
	mongoURI  string
	redisAddr string
	dbSeq     atomic.Int64
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	mc, err := tcmongo.Run(ctx, "mongo:8.0", tcmongo.WithReplicaSet("rs0"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "mongo container:", err)
		os.Exit(1)
	}
	rc, err := tcredis.Run(ctx, "redis:8.2-alpine")
	if err != nil {
		fmt.Fprintln(os.Stderr, "redis container:", err)
		os.Exit(1)
	}
	if mongoURI, err = mc.ConnectionString(ctx); err != nil {
		os.Exit(1)
	}
	if !strings.Contains(mongoURI, "directConnection") {
		sep := "?"
		if strings.Contains(mongoURI, "?") {
			sep = "&"
		}
		mongoURI += sep + "directConnection=true"
	}
	raw, _ := rc.ConnectionString(ctx)
	redisAddr = strings.TrimPrefix(raw, "redis://")
	code := m.Run()
	_ = testcontainers.TerminateContainer(mc)
	_ = testcontainers.TerminateContainer(rc)
	os.Exit(code)
}

type env struct {
	t      *testing.T
	store  *mongo.Store
	market *app.Market
	srv    *httptest.Server
	clock  *clock.System
}

func newEnv(t *testing.T, withRedis bool) *env {
	t.Helper()
	ctx := context.Background()
	store, err := mongo.Connect(ctx, mongoURI, fmt.Sprintf("it_%d_%d", time.Now().UnixNano(), dbSeq.Add(1)), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Database().Drop(ctx); _ = store.Close(ctx) })
	sealer, _ := proposal.NewSealer(map[int][]byte{1: bytes.Repeat([]byte{1}, 32)}, 1, rand.Reader)
	clk := &clock.System{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := &app.Market{
		Store: store, IDs: ids.New(), Clock: clk, Sealer: sealer, Tree: taxonomy.Default,
		Intents: intent.Policy{
			MinWindow: iso8601.MustParse("PT1S"), MaxWindow: iso8601.MustParse("P1D"),
			SelectionWindow: iso8601.MustParse("PT1H"), ActiveWave: 3,
		},
		Settlement: agreement.DefaultPolicy, RatingDays: 14, Log: log,
	}
	deps := httpapi.Deps{Market: m, JWTSecret: []byte("test-secret"), DevAuth: true, MaxInFlight: 256, Log: log}
	if withRedis {
		rdb, err := redis.Connect(redisAddr, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(rdb.Close)
		m.Scheduler, deps.Limiter = rdb, rdb
	}
	srv := httptest.NewServer(httpapi.New(deps))
	t.Cleanup(srv.Close)
	return &env{t: t, store: store, market: m, srv: srv, clock: clk}
}

// call does a JSON request and decodes the response into out (if not nil).
func (e *env) call(method, path, token string, in any, out any, headers ...string) int {
	e.t.Helper()
	var rd io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, e.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		e.t.Logf("%s %s → %d: %s", method, path, res.StatusCode, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			e.t.Fatalf("%s %s: %v\n%s", method, path, err, raw)
		}
	}
	return res.StatusCode
}

type party struct {
	ID, Token string
}

// docFor builds a valid CPF (or alphanumeric CNPJ for companies) per index.
func docFor(i int, company bool) string {
	if company {
		base := fmt.Sprintf("%02dABC%07d", i%100, 1000+i)
		d1, d2, _ := cnpj.CheckDigits(base)
		return base + string([]byte{d1, d2})
	}
	base := fmt.Sprintf("%09d", 100000001+i*7919)
	d1, d2, _ := cpf.CheckDigits(base)
	return base + string([]byte{d1, d2})
}

func (e *env) register(i int, credentials ...string) party {
	e.t.Helper()
	var out struct {
		Party struct {
			ID string `json:"id"`
		} `json:"party"`
		Token string `json:"token"`
	}
	company := false
	for _, c := range credentials {
		company = company || c == "business"
	}
	in := map[string]any{"name": fmt.Sprintf("parte %d", i), "document": docFor(i, company), "credentials": credentials}
	if code := e.call(http.MethodPost, "/v1/dev/parties", "", in, &out); code != http.StatusCreated {
		e.t.Fatalf("register %d: %d", i, code)
	}
	return party{out.Party.ID, out.Token}
}
