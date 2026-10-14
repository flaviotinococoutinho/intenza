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

	"go.opentelemetry.io/otel/trace"

	"github.com/flaviotinococoutinho/intenza/internal/adapter/escrow"
	"github.com/flaviotinococoutinho/intenza/internal/adapter/kafka"
	"github.com/flaviotinococoutinho/intenza/internal/adapter/mongo"
	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/platform/admin"
	"github.com/flaviotinococoutinho/intenza/internal/platform/config"
	"github.com/flaviotinococoutinho/intenza/internal/platform/logx"
	"github.com/flaviotinococoutinho/intenza/internal/platform/telemetry"
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
	tel, err := telemetry.Setup(ctx, "intenza-worker", cfg.OTLPEndpoint)
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
	checks := []admin.Check{{Name: "mongo", Fn: store.Ping}}
	if rdb != nil {
		defer rdb.Close()
		checks = append(checks, admin.Check{Name: "redis", Fn: rdb.Ping})
	}
	market.Obs = tel
	adm := admin.New(checks...)
	adm.Mux.Handle("GET /metrics", tel.MetricsHandler())
	go func() { _ = admin.Serve(ctx, cfg.Admin, adm.Mux) }()
	traced := func(group string, h kafka.Handler) (kafka.Handler, func(topic, result string)) {
		return func(ctx context.Context, e kafka.Event) error {
				ctx, span := tel.Tracer.Start(telemetry.FromTraceParent(ctx, e.TraceParent), "consume "+e.Type,
					trace.WithSpanKind(trace.SpanKindConsumer))
				defer span.End()
				return h(ctx, e)
			}, func(topic, result string) {
				tel.ConsumerEvents.Add(ctx, 1, telemetry.Attrs("group", group, "topic", topic, "result", result))
			}
	}

	has := func(role string) bool { return slices.Contains(cfg.WorkerRoles, role) }
	if has("deadlines") {
		d := &app.Deadlines{Market: market, Log: log, OnFire: func(kind, source string, late time.Duration) {
			tel.DeadlineLateness.Record(ctx, late.Seconds(), telemetry.Attrs("kind", kind, "source", source))
		}}
		if rdb != nil {
			d.Claim = rdb.ClaimDue
		}
		go every(ctx, 500*time.Millisecond, d.Tick, log, "deadlines-index")
		go every(ctx, 5*time.Second, d.Sweep, log, "deadlines-sweep")
	}
	esc := escrow.New(cfg.EscrowURL, cfg.EscrowTimeout, log)
	esc.OnCall = func(op, result string, d time.Duration) {
		tel.EscrowCalls.Add(ctx, 1, telemetry.Attrs("op", op, "result", result))
		tel.EscrowDuration.Record(ctx, d.Seconds(), telemetry.Attrs("op", op))
	}
	esc.OnState = func(state int) { tel.BreakerState.Record(ctx, int64(state), telemetry.Attrs("name", "escrow")) }
	settlement := &app.Settlement{Market: market, Escrow: esc, Back: store, Log: log}
	if has("settlement") {
		h, onResult := traced("settlement", func(ctx context.Context, e kafka.Event) error { return settlement.Handle(ctx, e.Type, e.Subject) })
		c := &kafka.Consumer{
			Group: "settlement", Topics: []string{kafka.TopicAgreements}, Brokers: cfg.KafkaBrokers, Log: log,
			Handle: h, OnResult: onResult,
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
		h, onResult := traced("fanout", func(ctx context.Context, e kafka.Event) error {
			if e.Type != intent.EvPublished {
				return nil
			}
			return f.Handle(ctx, e.ID, e.Subject)
		})
		c := &kafka.Consumer{
			Group: "fanout", Topics: []string{kafka.TopicIntents}, Brokers: cfg.KafkaBrokers, Log: log,
			Handle: h, OnResult: onResult,
		}
		go c.Loop(ctx)
	}
	log.Info("worker no ar", "roles", cfg.WorkerRoles)
	<-ctx.Done()
	return nil
}
