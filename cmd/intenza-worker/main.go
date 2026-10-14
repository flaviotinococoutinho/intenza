//intenza:paradigm imperative-shell

// Command intenza-worker runs the asynchronous side of the market: deadlines,
// the settlement saga, reconciliation with the escrow agent and the fan-out
// of new intents. Roles are chosen by INTENZA_WORKER_ROLES.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/adapter/escrow"
	"github.com/flaviotinococoutinho/intenza/internal/adapter/kafka"
	"github.com/flaviotinococoutinho/intenza/internal/adapter/mongo"
	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/platform/admin"
	"github.com/flaviotinococoutinho/intenza/internal/platform/config"
	"github.com/flaviotinococoutinho/intenza/internal/platform/logx"
	"github.com/flaviotinococoutinho/intenza/internal/wiring"
)

func main() {
	log := logx.New("intenza-worker")
	if err := run(log); err != nil {
		log.Error("encerrando com erro", "err", err)
		os.Exit(1)
	}
}

func every(ctx context.Context, d time.Duration, fn func(context.Context) error, log *slog.Logger, name string) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := fn(ctx); err != nil && ctx.Err() == nil {
				log.Warn("tarefa periódica falhou", "task", name, "err", err)
			}
		}
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
	market, rdb, err := wiring.Market(cfg, store, log)
	if err != nil {
		return err
	}
	checks := []admin.Check{{Name: "mongo", Fn: store.Ping}}
	if rdb != nil {
		defer rdb.Close()
		checks = append(checks, admin.Check{Name: "redis", Fn: rdb.Ping})
	}
	adm := admin.New(checks...)
	go func() { _ = admin.Serve(ctx, cfg.Admin, adm.Mux) }()

	has := func(role string) bool { return slices.Contains(cfg.WorkerRoles, role) }
	if has("deadlines") {
		d := &app.Deadlines{Market: market, Log: log}
		if rdb != nil {
			d.Claim = rdb.ClaimDue
		}
		go every(ctx, 500*time.Millisecond, d.Tick, log, "deadlines-index")
		go every(ctx, 5*time.Second, d.Sweep, log, "deadlines-sweep")
	}
	esc := escrow.New(cfg.EscrowURL, cfg.EscrowTimeout, log)
	settlement := &app.Settlement{Market: market, Escrow: esc, Back: store, Log: log}
	if has("settlement") {
		c := &kafka.Consumer{
			Group: "settlement", Topics: []string{kafka.TopicAgreements}, Brokers: cfg.KafkaBrokers, Log: log,
			Handle: func(ctx context.Context, e kafka.Event) error { return settlement.Handle(ctx, e.Type, e.Subject) },
		}
		go c.Loop(ctx)
	}
	if has("reconcile") {
		go every(ctx, 10*time.Second, func(ctx context.Context) error {
			n, err := settlement.Reconcile(ctx, 10*time.Second)
			if n > 0 {
				log.Info("reconciliação corrigiu acordos", "n", n)
			}
			return err
		}, log, "reconcile")
	}
	if has("fanout") {
		f := app.NewFanout(market, store, store, cfg.FanoutK, cfg.FanoutSeed, log)
		c := &kafka.Consumer{
			Group: "fanout", Topics: []string{kafka.TopicIntents}, Brokers: cfg.KafkaBrokers, Log: log,
			Handle: func(ctx context.Context, e kafka.Event) error {
				if e.Type != intent.EvPublished {
					return nil
				}
				return f.Handle(ctx, e.ID, e.Subject)
			},
		}
		go c.Loop(ctx)
	}
	log.Info("worker no ar", "roles", cfg.WorkerRoles)
	<-ctx.Done()
	return nil
}
