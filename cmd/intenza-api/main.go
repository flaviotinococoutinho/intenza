//intenza:paradigm imperative-shell

// Command intenza-api serves the public HTTP API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/adapter/httpapi"
	"github.com/flaviotinococoutinho/intenza/internal/adapter/mongo"
	"github.com/flaviotinococoutinho/intenza/internal/platform/admin"
	"github.com/flaviotinococoutinho/intenza/internal/platform/clock"
	"github.com/flaviotinococoutinho/intenza/internal/platform/config"
	"github.com/flaviotinococoutinho/intenza/internal/platform/logx"
	"github.com/flaviotinococoutinho/intenza/internal/platform/telemetry"
	"github.com/flaviotinococoutinho/intenza/internal/wiring"
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

	tel, err := telemetry.Setup(ctx, "intenza-api", cfg.OTLPEndpoint)
	if err != nil {
		return err
	}
	defer func() { _ = tel.Shutdown(context.WithoutCancel(ctx)) }()
	store, err := mongo.Connect(ctx, cfg.MongoURI, cfg.MongoDB, cfg.MongoTimeout, tel.MongoMonitor())
	if err != nil {
		return err
	}
	defer store.Close(context.WithoutCancel(ctx))

	market, rdb, err := wiring.Market(cfg, store, log)
	if err != nil {
		return err
	}
	market.Obs = tel
	deps := httpapi.Deps{
		Market: market, JWTSecret: []byte(cfg.JWTSecret), DevAuth: cfg.DevAuth, MaxInFlight: 512, Log: log,
		Back: store, WebhookKey: []byte(cfg.WebhookSecret),
		Wrap: func(h http.Handler) http.Handler { return tel.HTTP(h, "intenza-api") },
		OnLimit: func(scope string, degraded bool) {
			tel.HTTPRateLimited.Add(ctx, 1, telemetry.Attrs("scope", scope, "degraded", strconv.FormatBool(degraded)))
		},
	}
	checks := []admin.Check{{Name: "mongo", Fn: store.Ping}}
	if rdb != nil {
		deps.Limiter = rdb
		checks = append(checks, admin.Check{Name: "redis", Fn: rdb.Ping})
		defer rdb.Close()
	}
	adm := admin.New(checks...)
	adm.Mux.Handle("GET /metrics", tel.MetricsHandler())
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
