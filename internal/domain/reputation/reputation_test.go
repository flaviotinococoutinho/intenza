package reputation

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"pgregory.net/rapid"
)

var now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func ratings(pos, neg int, at time.Time) []Rating {
	out := make([]Rating, 0, pos+neg)
	for range pos {
		out = append(out, Rating{Stars: 5, SubmittedAt: at})
	}
	for range neg {
		out = append(out, Rating{Stars: 2, SubmittedAt: at})
	}
	return out
}

// Closed forms: Beta(1,1) is uniform and Beta(2,2) is symmetric around 0.5.
func TestQuantilesAgainstClosedForms(t *testing.T) {
	for _, p := range []float64{0.025, 0.5, 0.975} {
		if q := BetaQuantile(p, 1, 1); math.Abs(q-p) > 1e-9 {
			t.Errorf("uniform quantile %v = %v", p, q)
		}
	}
	if q := BetaQuantile(0.5, 2, 2); math.Abs(q-0.5) > 1e-9 {
		t.Errorf("Beta(2,2) median = %v", q)
	}
	// Beta(2,1) has CDF x², so the p-quantile is sqrt(p).
	if q := BetaQuantile(0.81, 2, 1); math.Abs(q-0.9) > 1e-9 {
		t.Errorf("Beta(2,1) 0.81-quantile = %v", q)
	}
}

// Property: more evidence at the same proportion shrinks the interval, and
// the mean always lies inside it.
func TestIntervalShrinksWithEvidence(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		pos := rapid.IntRange(0, 50).Draw(rt, "pos")
		neg := rapid.IntRange(0, 50).Draw(rt, "neg")
		small := Estimate(ratings(pos, neg, now), DefaultPrior, DefaultHalfLife, now)
		big := Estimate(ratings(pos*4, neg*4, now), DefaultPrior, DefaultHalfLife, now)
		if small.Low > small.Mean || small.Mean > small.High {
			rt.Fatalf("mean outside its interval: %+v", small)
		}
		if pos+neg > 0 && big.High-big.Low > small.High-small.Low+1e-12 {
			rt.Fatalf("interval grew with evidence: %v → %v", small.High-small.Low, big.High-big.Low)
		}
	})
}

// A single 5 does not beat a long history averaging 4.8: shrinkage toward the
// prior protects buyers from lucky newcomers.
func TestShrinkageAgainstLuckyNewcomers(t *testing.T) {
	newcomer := Estimate(ratings(1, 0, now), DefaultPrior, DefaultHalfLife, now)
	veteran := Estimate(ratings(190, 10, now), DefaultPrior, DefaultHalfLife, now)
	if newcomer.Mean >= veteran.Mean {
		t.Fatalf("newcomer %.3f vs veteran %.3f", newcomer.Mean, veteran.Mean)
	}
	if newcomer.High-newcomer.Low < 2*(veteran.High-veteran.Low) {
		t.Fatal("the newcomer must carry much more uncertainty")
	}
}

func TestOldRatingsWeighLess(t *testing.T) {
	fresh := Estimate(ratings(0, 10, now), DefaultPrior, DefaultHalfLife, now)
	stale := Estimate(ratings(0, 10, now.AddDate(-3, 0, 0)), DefaultPrior, DefaultHalfLife, now)
	if stale.Mean <= fresh.Mean {
		t.Fatalf("three-year-old complaints should matter less: %.3f vs %.3f", stale.Mean, fresh.Mean)
	}
}

func TestDoubleBlindReveal(t *testing.T) {
	p := Pair{AgreementID: "a", BuyerID: "b", SellerID: "s", WindowEnds: now.Add(14 * 24 * time.Hour)}
	p, err := p.Submit("b", 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Revealed(now.Add(time.Hour)); ok {
		t.Fatal("one rating alone stays sealed")
	}
	if _, err := p.Submit("b", 5, now); !errors.Is(err, ErrTwice) {
		t.Fatal("no second rating")
	}
	if _, err := p.Submit("x", 5, now); !errors.Is(err, ErrNotPair) {
		t.Fatal("outsiders do not rate")
	}
	both, _ := p.Submit("s", 4, now.Add(2*time.Hour))
	if rs, ok := both.Revealed(now.Add(2 * time.Hour)); !ok || len(rs) != 2 {
		t.Fatal("both rated: show both")
	}
	if rs, ok := p.Revealed(p.WindowEnds); !ok || len(rs) != 1 || rs[0].Role != AsSeller {
		t.Fatal("window over: show what exists")
	}
	if _, err := p.Submit("s", 4, p.WindowEnds); !errors.Is(err, ErrWindow) {
		t.Fatal("no rating after the window")
	}
}

// Seeded Monte Carlo: against an established seller (mean ~0.87) a newcomer
// still wins the single notification slot a meaningful share of the time,
// while a seller with a poor record almost never does.
func TestThompsonSamplingExposesNewcomers(t *testing.T) {
	r := rand.New(rand.NewPCG(42, 1))
	veteran := Contender{"veteran", Estimate(ratings(120, 18, now), DefaultPrior, DefaultHalfLife, now)}
	newcomer := Contender{"newcomer", Estimate(nil, DefaultPrior, DefaultHalfLife, now)}
	poor := Contender{"poor", Estimate(ratings(30, 70, now), DefaultPrior, DefaultHalfLife, now)}
	wins := map[string]int{}
	const trials = 20000
	for range trials {
		wins[Explore(r, []Contender{veteran, newcomer, poor}, 1)[0]]++
	}
	share := func(id string) float64 { return float64(wins[id]) / trials }
	if share("newcomer") < 0.15 || share("veteran") < 0.5 || share("poor") > 0.01 {
		t.Fatalf("shares: veteran %.3f newcomer %.3f poor %.3f", share("veteran"), share("newcomer"), share("poor"))
	}
	t.Logf("veteran %.3f newcomer %.3f poor %.3f", share("veteran"), share("newcomer"), share("poor"))
}

func TestSamplingMatchesThePosteriorMean(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 9))
	s := Estimate(ratings(30, 10, now), DefaultPrior, DefaultHalfLife, now)
	var sum float64
	const n = 50000
	for range n {
		sum += s.Sample(r)
	}
	if math.Abs(sum/n-s.Mean) > 0.005 {
		t.Fatalf("sample mean %.4f vs posterior mean %.4f", sum/n, s.Mean)
	}
}
