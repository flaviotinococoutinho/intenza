//intenza:paradigm imperative-shell

// Command intenza-chaos runs chaos experiments against the compose stack and
// writes their journals, or just generates load.
//
//	intenza-chaos run chaos/experiments/*.yaml [-out chaos/results]
//	intenza-chaos load -rate 2 -duration 60s
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/chaos"
	"github.com/flaviotinococoutinho/intenza/internal/chaos/load"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "uso: intenza-chaos run <experimentos...> | load")
		os.Exit(2)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	e := chaos.Env{
		API:        env("CHAOS_API", "http://localhost:8080"),
		Prometheus: env("CHAOS_PROMETHEUS", "http://localhost:9090"),
		Toxiproxy:  env("CHAOS_TOXIPROXY", "http://localhost:18474"),
		MongoURI:   env("CHAOS_MONGO", "mongodb://localhost:27117/?directConnection=true"),
		DockerHost: env("DOCKER_HOST", "unix:///var/run/docker.sock"),
		Project:    env("CHAOS_PROJECT", "intenza"),
		Admin: map[string]string{
			"api":    env("CHAOS_ADMIN_API", "http://localhost:9091"),
			"relay":  env("CHAOS_ADMIN_RELAY", "http://localhost:9092"),
			"worker": env("CHAOS_ADMIN_WORKER", "http://localhost:9093"),
			"psp":    env("CHAOS_ADMIN_PSP", "http://localhost:9094"),
		},
		Log: log,
	}
	switch os.Args[1] {
	case "run":
		fs := flag.NewFlagSet("run", flag.ExitOnError)
		out := fs.String("out", "chaos/results", "diretório dos diários")
		_ = fs.Parse(os.Args[2:])
		code := run(ctx, e, *out, fs.Args(), log)
		stop()
		os.Exit(code) //nolint:gocritic // stop() já rodou

	case "load":
		fs := flag.NewFlagSet("load", flag.ExitOnError)
		rate := fs.Float64("rate", 1, "intenções por segundo")
		dur := fs.Duration("duration", 60*time.Second, "duração")
		settle := fs.Bool("settlement", false, "levar acordos até a liberação")
		seed := fs.Uint64("seed", 1, "semente")
		_ = fs.Parse(os.Args[2:])
		stats, err := load.Run(ctx, load.Config{API: e.API, Rate: *rate, ProposeP: 0.6, Duration: *dur, Settlement: *settle, Seed: *seed, Log: log})
		if stats != nil {
			out, _ := json.MarshalIndent(stats.Summaries(), "", "  ")
			fmt.Println(string(out))
		}
		if err != nil {
			log.Error("carga", "err", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "comando desconhecido:", os.Args[1])
		os.Exit(2)
	}
}

func run(ctx context.Context, e chaos.Env, out string, files []string, log *slog.Logger) int {
	r, err := chaos.NewRunner(ctx, e)
	if err != nil {
		log.Error("runner", "err", err)
		return 2
	}
	defer r.Close(context.WithoutCancel(ctx))
	failed := 0
	var lines []string
	for _, f := range files {
		exp, err := chaos.LoadExperiment(f)
		if err != nil {
			log.Error("experimento", "file", f, "err", err)
			failed++
			continue
		}
		j := r.Run(ctx, exp)
		if err := chaos.Write(out, j); err != nil {
			log.Error("diário", "err", err)
		}
		if j.Verdict != "held" {
			failed++
		}
		lines = append(lines, fmt.Sprintf("%-28s %s (%s)", exp.ID, j.Verdict, j.FinishedAt.Sub(j.StartedAt).Round(time.Second)))
		if ctx.Err() != nil {
			break
		}
	}
	fmt.Println(strings.Join(lines, "\n"))
	if failed > 0 {
		return 1
	}
	return 0
}
