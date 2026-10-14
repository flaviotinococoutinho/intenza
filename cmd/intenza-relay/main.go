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

	"github.com/flaviotinococoutinho/intenza/internal/adapter/kafka"
	"github.com/flaviotinococoutinho/intenza/internal/adapter/mongo"
	"github.com/flaviotinococoutinho/intenza/internal/platform/admin"
	"github.com/flaviotinococoutinho/intenza/internal/platform/config"
	"github.com/flaviotinococoutinho/intenza/internal/platform/logx"
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
	store, err := mongo.Connect(ctx, cfg.MongoURI, cfg.MongoDB, cfg.MongoTimeout)
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
	r := &relay.Relay{DB: store.Database(), Producer: producer, Log: log, Grace: 2 * time.Second, Every: 2 * time.Second, Exit: os.Exit}
	adm := admin.New(admin.Check{Name: "mongo", Fn: store.Ping}, admin.Check{Name: "kafka", Fn: producer.Ping})
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

func ignoreCanceled(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
