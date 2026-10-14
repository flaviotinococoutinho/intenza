package gtin

import (
	"strconv"
	"testing"
)

func TestKnownNumbers(t *testing.T) {
	for _, s := range []string{"4006381333931", "614141234561", "96385074", "9780306406157", "10614141000415", "7 891234 567895"} {
		if !Valid(s) {
			t.Errorf("%s should be valid", s)
		}
	}
	for _, s := range []string{"4006381333932", "61414123456", "abc", "40063813339310000", ""} {
		if Valid(s) {
			t.Errorf("%s should be invalid", s)
		}
	}
	if d, _ := CheckDigit("400638133393"); d != '1' {
		t.Fatalf("check digit = %c", d)
	}
	if g, _ := To14("614141234561"); g != "00614141234561" {
		t.Fatalf("To14 = %s", g)
	}
}

func TestPrefixes(t *testing.T) {
	cases := map[string]string{
		"7891234567895": "GS1 Brasil",
		"4006381333931": "GS1 Germany",
		"614141234561":  "GS1 US",
		"9780306406157": "Bookland (ISBN)",
	}
	for n, want := range cases {
		p, ok := Prefix(n)
		if !ok || p.Org() != want {
			t.Errorf("%s: %q %v", n, p.Org(), ok)
		}
	}
	if p, _ := Prefix("1520000000002"); p.Kind() != Reserved {
		t.Fatalf("152 is unlisted, hence reserved: %v", p.Org())
	}
	if _, ok := Prefix("96385074"); ok {
		t.Fatal("GTIN-8 uses another allocation scheme")
	}
}

// Proof by exhaustion over every prefix: the table covers 000–999 and every
// row's range maps back to that row unless a later row overrides it.
func TestPrefixTableCoversEveryPrefix(t *testing.T) {
	for p := range 1000 {
		e := prefixTable[p]
		lo, hi := e.Range()
		if p < lo || p > hi {
			t.Fatalf("prefix %03d mapped to %q [%d,%d]", p, e.Org(), lo, hi)
		}
	}
	if prefixTable[789].Org() != "GS1 Brasil" || prefixTable[790].Org() != "GS1 Brasil" || prefixTable[791].Kind() != Reserved {
		t.Fatal("Brazil is 789–790")
	}
}

// What the check digit catches, proved over every position of a 13-digit
// number and every pair of digits: all single substitutions, and adjacent
// transpositions unless the swapped digits differ by 5.
func TestCheckDigitDetectionIsExactlyKnown(t *testing.T) {
	base := []byte("4006381333931")
	for pos := range 12 {
		for d := byte('0'); d <= '9'; d++ {
			if d == base[pos] {
				continue
			}
			mut := append([]byte(nil), base...)
			mut[pos] = d
			if Valid(string(mut)) {
				t.Fatalf("substitution at %d to %c not detected", pos, d)
			}
		}
	}
	for pos := range 11 {
		for a := byte('0'); a <= '9'; a++ {
			for b := byte('0'); b <= '9'; b++ {
				if a == b {
					continue
				}
				x := append([]byte(nil), base...)
				x[pos], x[pos+1] = a, b
				fixed, _ := CheckDigit(string(x[:12]))
				x[12] = fixed
				y := append([]byte(nil), x...)
				y[pos], y[pos+1] = b, a
				detected := !Valid(string(y))
				diff := int(a) - int(b)
				if diff < 0 {
					diff = -diff
				}
				if detected != (diff != 5) {
					t.Fatalf("swap %c%c at %d: detected=%v", a, b, pos, detected)
				}
			}
		}
	}
}

func TestNormalizeDoesNotCopyCompactInput(t *testing.T) {
	allocs := testing.AllocsPerRun(100, func() { _, _ = Normalize("4006381333931") })
	if allocs != 0 {
		t.Fatalf("allocs = %v", allocs)
	}
}

func FuzzCheckDigitCompletesAnyBody(f *testing.F) {
	f.Add(uint64(400638133393))
	f.Fuzz(func(t *testing.T, n uint64) {
		body := strconv.FormatUint(n%1_000_000_000_000, 10)
		for len(body) < 12 {
			body = "0" + body
		}
		d, err := CheckDigit(body)
		if err != nil || !Valid(body+string(d)) {
			t.Fatalf("%s + %c", body, d)
		}
	})
}
