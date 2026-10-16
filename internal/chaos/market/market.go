//intenza:paradigm probabilistic

// Package market is the stochastic model behind the load generator, kept
// apart from the HTTP driver so it stays pure: every draw comes from an
// injected seeded source and nothing reads the clock.
//
//   - arrivals of intents form a Poisson process (exponential gaps);
//   - categories follow a Zipf law (a few hot, a long tail);
//   - each seller answers with a fixed probability;
//   - sellers snipe: their proposals arrive an exponential time before the
//     deadline, so arrivals pile up at the end of the window;
//   - prices are lognormal around a base.
package market

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
)

// Model draws market decisions.
type Model struct {
	r        *rand.Rand
	leaves   []taxonomy.Category
	zipf     *rand.Zipf
	rate     float64
	proposeP float64
	snipe    time.Duration
}

// New builds a model over the launched leaves of a wave.
func New(r *rand.Rand, tree *taxonomy.Tree, wave int, rate, proposeP float64, snipe time.Duration) *Model {
	m := &Model{r: r, rate: rate, proposeP: proposeP, snipe: snipe}
	for c := range tree.All() {
		if c.Leaf() && c.Wave() <= wave {
			m.leaves = append(m.leaves, c)
		}
	}
	m.zipf = rand.NewZipf(r, 1.3, 1, uint64(len(m.leaves)-1))
	return m
}

// exp draws an exponential variate with the given mean.
func (m *Model) exp(mean time.Duration) time.Duration {
	return time.Duration(-math.Log(1-m.r.Float64()) * float64(mean))
}

// NextArrival is the gap until the next intent.
func (m *Model) NextArrival() time.Duration {
	return m.exp(time.Duration(float64(time.Second) / m.rate))
}

// Category picks a leaf by popularity.
func (m *Model) Category() taxonomy.Category { return m.leaves[m.zipf.Uint64()] }

// Answers decides whether a seller proposes.
func (m *Model) Answers() bool { return m.r.Float64() < m.proposeP }

// SnipeLead is how long before the deadline a proposal arrives.
func (m *Model) SnipeLead() time.Duration { return m.exp(m.snipe) }

// PriceCents draws a lognormal price around R$ 500.
func (m *Model) PriceCents() int64 { return int64(math.Exp(math.Log(50000) + 0.5*m.r.NormFloat64())) }

// LeadDays draws a lead time between 1 and 10 days.
func (m *Model) LeadDays() int { return 1 + m.r.IntN(10) }

// Pick chooses one of n.
func (m *Model) Pick(n int) int { return m.r.IntN(n) }

// Keep decides with probability p (used to copy requested attributes).
func (m *Model) Keep(p float64) bool { return m.r.Float64() < p }

// Fork returns an independent stream for one lifecycle.
func (m *Model) Fork() *rand.Rand { return rand.New(rand.NewPCG(m.r.Uint64(), m.r.Uint64())) }
