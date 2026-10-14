package iso2108

import (
	"errors"
	"testing"

	"github.com/flaviotinococoutinho/intenza/standards/gs1/gtin"
)

func TestKnownISBNs(t *testing.T) {
	for _, s := range []string{"0-306-40615-2", "978-0-306-40615-7", "0-8044-2957-X", "0-8044-2957-x", "979-10-90636-07-1"} {
		if !Valid(s) {
			t.Errorf("%s should be valid", s)
		}
	}
	for _, s := range []string{"0-306-40615-3", "977-0-306-40615-7", "X-306-40615-2", "12345"} {
		if Valid(s) {
			t.Errorf("%s should be invalid", s)
		}
	}
	if n, _ := To13("0-306-40615-2"); n != "9780306406157" {
		t.Fatalf("To13 = %s", n)
	}
	if n, _ := To10("978-0-306-40615-7"); n != "0306406152" {
		t.Fatalf("To10 = %s", n)
	}
	if _, err := To10("979-10-90636-07-1"); !errors.Is(err, ErrNo10) {
		t.Fatalf("979 has no 10-digit form: %v", err)
	}
	d85, _ := gtin.CheckDigit("978853590277")
	d65, _ := gtin.CheckDigit("978650001234")
	if !Brazilian("978853590277"+string(d85)) || !Brazilian("978650001234"+string(d65)) || Brazilian("0-306-40615-2") {
		t.Fatal("Brazilian registration group")
	}
}

// Proof by exhaustion on ISBN-10: every substitution and every adjacent
// transposition of distinct characters changes the check.
func TestISBN10DetectsEverySubstitutionAndTransposition(t *testing.T) {
	base := []byte("0306406152")
	alphabet := "0123456789"
	for pos := range 10 {
		for i := range len(alphabet) {
			c := alphabet[i]
			if c == base[pos] {
				continue
			}
			m := append([]byte(nil), base...)
			m[pos] = c
			if Valid(string(m)) {
				t.Fatalf("substitution at %d not detected", pos)
			}
		}
	}
	for pos := range 9 {
		for a := byte('0'); a <= '9'; a++ {
			for b := byte('0'); b <= '9'; b++ {
				if a == b {
					continue
				}
				x := append([]byte(nil), base...)
				x[pos], x[pos+1] = a, b
				d, _ := CheckDigit10(string(x[:9]))
				x[9] = d
				if pos == 8 {
					continue // swapping into the check position changes the alphabet
				}
				y := append([]byte(nil), x...)
				y[pos], y[pos+1] = b, a
				if Valid(string(y)) {
					t.Fatalf("transposition %c%c at %d not detected", a, b, pos)
				}
			}
		}
	}
}

func FuzzConversionsRoundTrip(f *testing.F) {
	f.Add("030640615")
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) != 9 {
			return
		}
		d, err := CheckDigit10(body)
		if err != nil {
			return
		}
		isbn10 := body + string(d)
		isbn13, err := To13(isbn10)
		if err != nil {
			t.Fatalf("To13(%s): %v", isbn10, err)
		}
		back, err := To10(isbn13)
		if err != nil || back != isbn10 {
			t.Fatalf("%s → %s → %s (%v)", isbn10, isbn13, back, err)
		}
	})
}
