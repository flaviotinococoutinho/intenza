package renavam

import (
	"errors"
	"testing"
)

// Vectors taken from the independent implementations cited in Spec, not
// produced by this package.
func TestVectorsFromIndependentImplementations(t *testing.T) {
	valid := []string{"75320797785", "91671999129", "60885589380", "639884962"}
	for _, s := range valid {
		if !Valid(s) {
			t.Errorf("%s should be valid", s)
		}
	}
	if n, _ := Normalize("639884962"); n != "00639884962" {
		t.Fatalf("padding: %s", n)
	}
	cases := map[string]error{"42752486198": ErrCheck, "12345678": ErrLength, "7532079778a": ErrChar}
	for s, want := range cases {
		if _, err := Normalize(s); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", s, err, want)
		}
	}
}

// 60885589380 lands on remainder 1, the case where 11 - r = 10 becomes 0.
func TestRemainderOneBecomesZero(t *testing.T) {
	if d, _ := CheckDigit("6088558938"); d != '0' {
		t.Fatalf("got %c", d)
	}
}
