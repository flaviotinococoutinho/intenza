//intenza:paradigm imperative-shell

// Package wiring assembles the use cases from configuration; every binary
// shares it so the API and the workers run exactly the same market.
package wiring

import (
	"crypto/rand"
	"log/slog"

	"github.com/flaviotinococoutinho/intenza/internal/adapter/mongo"
	"github.com/flaviotinococoutinho/intenza/internal/adapter/redis"
	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/domain/agreement"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/proposal"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
	"github.com/flaviotinococoutinho/intenza/internal/platform/clock"
	"github.com/flaviotinococoutinho/intenza/internal/platform/config"
	"github.com/flaviotinococoutinho/intenza/internal/platform/ids"
)

// Market builds the use cases. Redis is optional: without it the market runs
// with local rate limits and deadlines found by the sweeper.
func Market(cfg config.Config, store *mongo.Store, log *slog.Logger) (*app.Market, *redis.Client, error) {
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
