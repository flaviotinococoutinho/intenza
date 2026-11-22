package market

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
)

func TestModelIsReproducibleAndShaped(t *testing.T) {
	a := New(rand.New(rand.NewPCG(1, 2)), taxonomy.Default, 1, 2, 0.6, 3*time.Second)
	b := New(rand.New(rand.NewPCG(1, 2)), taxonomy.Default, 1, 2, 0.6, 3*time.Second)
	for range 100 {
		if a.NextArrival() != b.NextArrival() || a.Category().ID() != b.Category().ID() {
			t.Fatal("the same seed must yield the same market")
		}
	}
	const n = 20000
	var gaps, leads time.Duration
	answers, hot := 0, map[string]int{}
	for range n {
		gaps += a.NextArrival()
		leads += a.SnipeLead()
		if a.Answers() {
			answers++
		}
		hot[a.Category().ID()]++
	}
	if mean := gaps / n; mean < 450*time.Millisecond || mean > 550*time.Millisecond {
		t.Fatalf("mean gap at 2/s should be ~500ms, got %v", mean)
	}
	if mean := leads / n; mean < 2800*time.Millisecond || mean > 3200*time.Millisecond {
		t.Fatalf("mean snipe lead should be ~3s, got %v", mean)
	}
	if p := float64(answers) / n; math.Abs(p-0.6) > 0.02 {
		t.Fatalf("answer rate %v", p)
	}
	top := 0
	for _, c := range hot {
		top = max(top, c)
	}
	if float64(top)/n < 0.3 {
		t.Fatalf("Zipf should concentrate demand, the hottest leaf has %v", float64(top)/n)
	}
}
