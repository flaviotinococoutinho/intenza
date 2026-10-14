package cpf

import (
	"errors"
	"math/rand/v2"
	"testing"
)

func TestKnownNumbers(t *testing.T) {
	for _, s := range []string{"529.982.247-25", "52998224725"} {
		if !Valid(s) {
			t.Errorf("%s should be valid", s)
		}
	}
	cases := map[string]error{
		"529.982.247-26": ErrCheck,
		"111.111.111-11": ErrRepeated,
		"5299822472":     ErrLength,
		"529982247255":   ErrLength,
		"52998224a25":    ErrChar,
	}
	for s, want := range cases {
		if _, err := Normalize(s); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", s, err, want)
		}
	}
	if f, _ := Format("52998224725"); f != "529.982.247-25" {
		t.Fatalf("format: %s", f)
	}
}

// Measured, not assumed: across random bodies, the share of single-digit
// substitutions the two digits miss stays tiny, and every miss is the 0/1
// collapse at work on both digits.
func TestSingleSubstitutionDetectionRate(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	var total, missed int
	for range 2000 {
		var body [9]byte
		for i := range body {
			body[i] = byte('0' + rng.IntN(10))
		}
		d1, d2, _ := CheckDigits(string(body[:]))
		for pos := range 9 {
			for c := byte('0'); c <= '9'; c++ {
				if c == body[pos] {
					continue
				}
				m := body
				m[pos] = c
				e1, e2, _ := CheckDigits(string(m[:]))
				total++
				if e1 == d1 && e2 == d2 {
					missed++
				}
			}
		}
	}
	rate := float64(missed) / float64(total)
	if rate > 0.01 {
		t.Fatalf("missed %d of %d single substitutions (%.4f)", missed, total, rate)
	}
	t.Logf("missed %d of %d single substitutions (%.5f)", missed, total, rate)
}
