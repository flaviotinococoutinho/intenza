//intenza:paradigm imperative-shell

// Command intenza-relay pumps the outbox into Kafka.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/flaviotinococoutinho/intenza/internal/adapter/kafka"
	"github.com/flaviotinococoutinho/intenza/internal/adapter/mongo"
	"github.com/flaviotinococoutinho/intenza/internal/platform/admin"
	"github.com/flaviotinococoutinho/intenza/internal/platform/config"
	"github.com/flaviotinococoutinho/intenza/internal/platform/logx"
	"github.com/flaviotinococoutinho/intenza/internal/platform/telemetry"
	"github.com/flaviotinococoutinho/intenza/internal/relay"
)

func main() {
	log := logx.New("intenza-relay")
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
	tel, err := telemetry.Setup(ctx, "intenza-relay", cfg.OTLPEndpoint)
	if err != nil {
		return err
	}
	defer func() { _ = tel.Shutdown(context.WithoutCancel(ctx)) }()
	// no client-side operation timeout: the change stream is a long-lived cursor
	store, err := mongo.Connect(ctx, cfg.MongoURI, cfg.MongoDB, 0, tel.MongoMonitor())
	if err != nil {
		return err
	}
	defer store.Close(context.WithoutCancel(ctx))
	producer, err := kafka.NewProducer(cfg.KafkaBrokers)
	if err != nil {
		return err
	}
	defer producer.Close()
	for attempt := 0; ; attempt++ {
		tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = kafka.EnsureTopics(tctx, producer, 3)
		cancel()
		if err == nil || attempt == 30 || ctx.Err() != nil {
			break
		}
		log.Warn("broker ainda não respondeu, tentando de novo", "err", err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return err
	}
	r := &relay.Relay{
		DB: store.Database(), Producer: producer, Log: log, Grace: 2 * time.Second, Every: 2 * time.Second, Exit: os.Exit,
		OnPublished: func(n int, _ time.Duration) { tel.RelayPublished.Add(ctx, int64(n)) },
	}
	if err := observeBacklog(tel, store); err != nil {
		return err
	}
	adm := admin.New(admin.Check{Name: "mongo", Fn: store.Ping}, admin.Check{Name: "kafka", Fn: producer.Ping})
	adm.Mux.Handle("GET /metrics", tel.MetricsHandler())
	if cfg.Faults {
		adm.EnableFaults()
		adm.Register("crash_after_publish", admin.Fault{
			Describe: "sai do processo depois de publicar e antes de marcar o outbox (provoca duplicatas)",
			Get:      func() string { return strconv.FormatBool(r.CrashAfterPublish.Load()) },
			Set: func(v string) error {
				b, err := strconv.ParseBool(v)
				r.CrashAfterPublish.Store(b)
				return err
			},
		})
	}
	go func() { _ = admin.Serve(ctx, cfg.Admin, adm.Mux) }()
	log.Info("relay no ar", "brokers", cfg.KafkaBrokers)
	return ignoreCanceled(r.Run(ctx))
}

// observeBacklog exposes the outbox lag: how many events wait and how old
// the oldest is. It is the first signal of a broker outage.
func observeBacklog(tel *telemetry.Telemetry, store *mongo.Store) error {
	count, err := tel.Meter.Int64ObservableGauge("intenza_outbox_backlog", metric.WithDescription("Eventos esperando publicação"))
	if err != nil {
		return err
	}
	age, err := tel.Meter.Float64ObservableGauge("intenza_outbox_oldest_age", metric.WithUnit("s"), metric.WithDescription("Idade do evento mais antigo não publicado"))
	if err != nil {
		return err
	}
	_, err = tel.Meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		qctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		n, oldest, err := relay.Backlog(qctx, store.Database())
		if err != nil {
			return err
		}
		o.ObserveInt64(count, n)
		o.ObserveFloat64(age, oldest.Seconds())
		return nil
	}, count, age)
	return err
}

func ignoreCanceled(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
