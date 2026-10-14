package rfc9562

import (
	"bytes"
	"math/rand/v2"
	"testing"
	"time"
)

type seeded struct{ r *rand.Rand }

func (s seeded) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(s.r.Uint32())
	}
	return len(p), nil
}

func gen(clock *time.Time) *Generator {
	return NewGenerator(func() time.Time { return *clock }, seeded{rand.New(rand.NewPCG(1, 2))})
}

func TestVersionVariantAndTime(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_123)
	u, err := gen(&now).New()
	if err != nil {
		t.Fatal(err)
	}
	if u.Version() != 7 || u[8]>>6 != 0b10 || !u.Time().Equal(now) {
		t.Fatalf("version %d variant %b time %v", u.Version(), u[8]>>6, u.Time())
	}
	back, err := Parse(u.String())
	if err != nil || back != u {
		t.Fatalf("round trip: %v %v", back, err)
	}
}

// Appendix A.6 example: the timestamp of 017F22E2-79B0-7CC3-98C4-DC0C0C07398F
// is 2022-02-22 19:22:22 UTC.
func TestAppendixExampleTimestamp(t *testing.T) {
	u, err := Parse("017F22E2-79B0-7CC3-98C4-DC0C0C07398F")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2022, 2, 22, 19, 22, 22, 0, time.UTC)
	if !u.Time().Equal(want) || u.Version() != 7 {
		t.Fatalf("time %v version %d", u.Time().UTC(), u.Version())
	}
}

// Strictly increasing with the clock frozen, stepping back, and past the
// 4 096 IDs a single millisecond can hold.
func TestMonotonicUnderAdversarialClocks(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	g := gen(&now)
	prev, _ := g.New()
	for i := range 10_000 {
		switch {
		case i == 5000:
			now = now.Add(-2 * time.Second) // clock steps back
		case i%3000 == 0:
			now = now.Add(time.Millisecond)
		}
		u, err := g.New()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Compare(u[:], prev[:]) <= 0 {
			t.Fatalf("not increasing at %d: %s after %s", i, u, prev)
		}
		prev = u
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	for _, s := range []string{"", "017F22E2-79B0-7CC3-98C4-DC0C0C07398", "017F22E2_79B0-7CC3-98C4-DC0C0C07398F", "017F22E2-79B0-7CC3-98C4-DC0C0C07398G"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}

func BenchmarkNew(b *testing.B) {
	now := time.Now()
	g := gen(&now)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = g.New()
	}
}

func TestFromHashIsDeterministicVersion8(t *testing.T) {
	sum := bytes.Repeat([]byte{0xAB}, 32)
	a, err := FromHash(sum)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := FromHash(sum)
	if a != b || a.Version() != 8 || a[8]>>6 != 0b10 {
		t.Fatalf("%s v%d", a, a.Version())
	}
	if _, err := FromHash(sum[:8]); err == nil {
		t.Fatal("short hash")
	}
}
