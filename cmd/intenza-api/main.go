//intenza:paradigm imperative-shell

// Command intenza-api serves the public HTTP API.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/adapter/httpapi"
	"github.com/flaviotinococoutinho/intenza/internal/adapter/mongo"
	"github.com/flaviotinococoutinho/intenza/internal/adapter/redis"
	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/domain/agreement"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/proposal"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
	"github.com/flaviotinococoutinho/intenza/internal/platform/admin"
	"github.com/flaviotinococoutinho/intenza/internal/platform/clock"
	"github.com/flaviotinococoutinho/intenza/internal/platform/config"
	"github.com/flaviotinococoutinho/intenza/internal/platform/ids"
	"github.com/flaviotinococoutinho/intenza/internal/platform/logx"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso8601"
)

func main() {
	log := logx.New("intenza-api")
	if err := run(log); err != nil {
		log.Error("encerrando com erro", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := mongo.Connect(ctx, cfg.MongoURI, cfg.MongoDB, cfg.MongoTimeout)
	if err != nil {
		return err
	}
	defer store.Close(context.WithoutCancel(ctx))

	market, rdb, err := build(cfg, store, log)
	if err != nil {
		return err
	}
	deps := httpapi.Deps{Market: market, JWTSecret: []byte(cfg.JWTSecret), DevAuth: cfg.DevAuth, MaxInFlight: 512, Log: log}
	checks := []admin.Check{{Name: "mongo", Fn: store.Ping}}
	if rdb != nil {
		deps.Limiter = rdb
		checks = append(checks, admin.Check{Name: "redis", Fn: rdb.Ping})
		defer rdb.Close()
	}
	adm := admin.New(checks...)
	if cfg.Faults {
		adm.EnableFaults()
		clk := market.Clock.(*clock.System)
		adm.Register("clock_skew", admin.Fault{
			Describe: "adianta ou atrasa o relógio local (ISO 8601, ex.: PT1H, -PT30M, PT0S)",
			Get:      func() string { return clk.Skew().String() },
			Set: func(v string) error {
				d, err := iso8601.Parse(v)
				if err != nil {
					return err
				}
				exact, err := d.Exact()
				if err != nil {
					return err
				}
				clk.SetSkew(exact)
				return nil
			},
		})
	}
	return serve(ctx, log, cfg, httpapi.New(deps), adm.Mux)
}

func build(cfg config.Config, store *mongo.Store, log *slog.Logger) (*app.Market, *redis.Client, error) {
	keys, err := cfg.Keys()
	if err != nil {
		return nil, nil, err
	}
	sealer, err := proposal.NewSealer(keys, cfg.SealKeyCurrent, rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	minW, maxW, sel, fund, insp, err := cfg.Durations()
	if err != nil {
		return nil, nil, err
	}
	m := &app.Market{
		Store: store, IDs: ids.New(), Clock: &clock.System{}, Sealer: sealer, Tree: taxonomy.Default,
		Intents:    intent.Policy{MinWindow: minW, MaxWindow: maxW, SelectionWindow: sel, ActiveWave: cfg.ActiveWave},
		Settlement: agreement.Policy{FundingWindow: fund, InspectionWindow: insp},
		RatingDays: cfg.RatingDays, Log: log,
	}
	rdb, err := redis.Connect(cfg.RedisAddr, cfg.RedisTimeout)
	if err != nil {
		log.Warn("redis indisponível na partida: limites locais e prazos pela varredura", "err", err)
		return m, nil, nil
	}
	m.Scheduler = rdb
	return m, rdb, nil
}

func serve(ctx context.Context, log *slog.Logger, cfg config.Config, public, adm http.Handler) error {
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: public, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second}
	admSrv := &http.Server{Addr: cfg.Admin, Handler: adm, ReadHeaderTimeout: 5 * time.Second}
	errs := make(chan error, 2)
	go func() { errs <- admSrv.ListenAndServe() }()
	go func() { errs <- srv.ListenAndServe() }()
	log.Info("no ar", "http", cfg.HTTPAddr, "admin", cfg.Admin)
	select {
	case <-ctx.Done():
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = admSrv.Shutdown(shutdown)
	return srv.Shutdown(shutdown)
}
