//intenza:paradigm imperative-shell

// Command intenza-psp runs the escrow agent simulator.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/flaviotinococoutinho/intenza/internal/platform/admin"
	"github.com/flaviotinococoutinho/intenza/internal/platform/config"
	"github.com/flaviotinococoutinho/intenza/internal/platform/logx"
	"github.com/flaviotinococoutinho/intenza/internal/platform/telemetry"
	"github.com/flaviotinococoutinho/intenza/internal/psp"
)

func main() {
	log := logx.New("intenza-psp")
	cfg, err := config.Load()
	if err != nil {
		log.Error("configuração", "err", err)
		os.Exit(1)
	}
	tel, err := telemetry.Setup(context.Background(), "intenza-psp", cfg.OTLPEndpoint)
	if err != nil {
		log.Error("telemetria", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer func() { _ = tel.Shutdown(context.WithoutCancel(ctx)) }()
	sim := psp.New(cfg.WebhookURL, []byte(cfg.WebhookSecret), cfg.PSPSeed, log)
	adm := admin.New()
	adm.Mux.Handle("GET /metrics", tel.MetricsHandler())
	adm.EnableFaults() // the simulator exists to be broken
	for _, name := range []string{"latency_median", "latency_sigma", "error_rate", "webhook_drop", "webhook_dup", "webhook_delay", "auto_pay_after"} {
		adm.Register(name, admin.Fault{
			Describe: "knob do simulador",
			Get: func() string {
				f := sim.GetFaults()
				switch name {
				case "latency_median":
					return f.LatencyMedian.String()
				case "latency_sigma":
					return formatFloat(f.LatencySigma)
				case "error_rate":
					return formatFloat(f.ErrorRate)
				case "webhook_drop":
					return formatFloat(f.WebhookDrop)
				case "webhook_dup":
					return formatFloat(f.WebhookDup)
				case "webhook_delay":
					return f.WebhookDelay.String()
				}
				return f.AutoPayAfter.String()
			},
			Set: func(v string) error {
				f, err := psp.ParseFaults(sim.GetFaults(), name, v)
				if err == nil {
					sim.SetFaults(f)
				}
				return err
			},
		})
	}
	go sim.Run(ctx)
	go func() { _ = admin.Serve(ctx, cfg.Admin, adm.Mux) }()
	log.Info("psp simulado no ar", "http", cfg.HTTPAddr, "webhook", cfg.WebhookURL)
	if err := admin.Serve(ctx, cfg.HTTPAddr, tel.HTTP(sim.Handler(), "intenza-psp")); err != nil {
		log.Error("encerrando", "err", err)
	}
}
