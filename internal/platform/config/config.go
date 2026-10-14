//intenza:paradigm imperative-shell

// Package config reads the environment once, at start-up, into typed values.
package config

import (
	"encoding/base64"
	"errors"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/flaviotinococoutinho/intenza/standards/iso/iso8601"
)

// Config is the environment of every binary; each one uses what it needs.
type Config struct {
	Service  string `env:"INTENZA_SERVICE" envDefault:"intenza"`
	HTTPAddr string `env:"INTENZA_HTTP_ADDR" envDefault:":8080"`
	Admin    string `env:"INTENZA_ADMIN_ADDR" envDefault:":9090"`

	MongoURI      string        `env:"INTENZA_MONGO_URI" envDefault:"mongodb://localhost:27017/?replicaSet=rs0"`
	MongoDB       string        `env:"INTENZA_MONGO_DB" envDefault:"intenza"`
	MongoTimeout  time.Duration `env:"INTENZA_MONGO_TIMEOUT" envDefault:"2s"`
	RedisAddr     string        `env:"INTENZA_REDIS_ADDR" envDefault:"localhost:6379"`
	RedisTimeout  time.Duration `env:"INTENZA_REDIS_TIMEOUT" envDefault:"300ms"`
	KafkaBrokers  []string      `env:"INTENZA_KAFKA_BROKERS" envDefault:"localhost:9092" envSeparator:","`
	EscrowURL     string        `env:"INTENZA_ESCROW_URL" envDefault:"http://localhost:8090"`
	WebhookSecret string        `env:"INTENZA_WEBHOOK_SECRET" envDefault:"dev-webhook-secret"`

	JWTSecret string `env:"INTENZA_JWT_SECRET" envDefault:"dev-only-change-me"`
	DevAuth   bool   `env:"INTENZA_DEV_AUTH" envDefault:"true"`
	Faults    bool   `env:"INTENZA_FAULTS" envDefault:"false"`

	SealKeys       map[int]string `env:"INTENZA_SEAL_KEYS" envKeyValSeparator:"="`
	SealKeyCurrent int            `env:"INTENZA_SEAL_KEY_CURRENT" envDefault:"1"`

	MinWindow       string `env:"INTENZA_MIN_WINDOW" envDefault:"PT1H"`
	MaxWindow       string `env:"INTENZA_MAX_WINDOW" envDefault:"P14D"`
	SelectionWindow string `env:"INTENZA_SELECTION_WINDOW" envDefault:"P3D"`
	FundingWindow   string `env:"INTENZA_FUNDING_WINDOW" envDefault:"P2D"`
	InspectWindow   string `env:"INTENZA_INSPECTION_WINDOW" envDefault:"P7D"`
	ActiveWave      int    `env:"INTENZA_ACTIVE_WAVE" envDefault:"1"`
	RatingDays      int    `env:"INTENZA_RATING_DAYS" envDefault:"14"`

	WorkerRoles   []string      `env:"INTENZA_WORKER_ROLES" envDefault:"deadlines,settlement,reconcile,fanout" envSeparator:","`
	FanoutK       int           `env:"INTENZA_FANOUT_K" envDefault:"5"`
	FanoutSeed    uint64        `env:"INTENZA_FANOUT_SEED" envDefault:"1"`
	EscrowTimeout time.Duration `env:"INTENZA_ESCROW_TIMEOUT" envDefault:"1500ms"`
	PSPSeed       uint64        `env:"INTENZA_PSP_SEED" envDefault:"7"`
	WebhookURL    string        `env:"INTENZA_WEBHOOK_URL" envDefault:"http://localhost:8080/v1/webhooks/escrow"`

	OTLPEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
}

// Load parses the environment.
func Load() (Config, error) { return env.ParseAs[Config]() }

// Durations parses the ISO 8601 policy windows.
func (c Config) Durations() (minW, maxW, sel, fund, insp iso8601.Duration, err error) {
	parse := func(s string) iso8601.Duration {
		d, e := iso8601.Parse(s)
		if e != nil && err == nil {
			err = e
		}
		return d
	}
	return parse(c.MinWindow), parse(c.MaxWindow), parse(c.SelectionWindow), parse(c.FundingWindow), parse(c.InspectWindow), err
}

// DevSealKey is used when no key is configured: only for local runs.
var DevSealKey = []byte("intenza-dev-seal-key-32-bytes!!!")

// Keys decodes the base64 seal keys, falling back to the dev key.
func (c Config) Keys() (map[int][]byte, error) {
	if len(c.SealKeys) == 0 {
		return map[int][]byte{1: DevSealKey}, nil
	}
	out := map[int][]byte{}
	for v, k := range c.SealKeys {
		b, err := base64.StdEncoding.DecodeString(k)
		if err != nil {
			return nil, err
		}
		if len(b) != 32 {
			return nil, errors.New("config: seal keys must have 32 bytes")
		}
		out[v] = b
	}
	return out, nil
}
