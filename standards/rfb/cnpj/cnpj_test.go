package cnpj

import (
	"errors"
	"testing"
)

func TestKnownNumbers(t *testing.T) {
	valid := []string{"12.ABC.345/01DE-35", "12abc34501de35", "11.222.333/0001-81", "11222333000181"}
	for _, s := range valid {
		if !Valid(s) {
			t.Errorf("%s should be valid", s)
		}
	}
	cases := map[string]error{
		"12.ABC.345/01DE-36": ErrCheck,
		"11.222.333/0001-82": ErrCheck,
		"00.000.000/0000-00": ErrRepeated,
		"12.ABC.345/01DE-3A": ErrChar,
		"12.ABC.345/01DE":    ErrLength,
		"12.ABC.345/01DE-3$": ErrChar,
	}
	for s, want := range cases {
		if _, err := Normalize(s); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", s, err, want)
		}
	}
	if !Alphanumeric("12.ABC.345/01DE-35") || Alphanumeric("11.222.333/0001-81") {
		t.Fatal("alphanumeric detection")
	}
	if f, _ := Format("12abc34501de35"); f != "12.ABC.345/01DE-35" {
		t.Fatalf("format: %s", f)
	}
}

// The alphanumeric rule is a superset: for numeric bases it computes exactly
// the legacy digits, checked here against an independent legacy implementation.
func TestAlphanumericRuleAgreesWithLegacyOnDigits(t *testing.T) {
	legacy := func(base string) (byte, byte) {
		calc := func(s string, weights []int) byte {
			sum := 0
			for i := range s {
				sum += int(s[i]-'0') * weights[i]
			}
			if r := sum % 11; r >= 2 {
				return byte('0' + 11 - r)
			}
			return '0'
		}
		d1 := calc(base, []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2})
		d2 := calc(base+string(d1), []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2})
		return d1, d2
	}
	n := uint64(112223330001)
	for range 5000 {
		n = n*6364136223846793005 + 1442695040888963407
		var b [12]byte
		x := n
		for i := range b {
			b[i] = byte('0' + x%10)
			x /= 10
		}
		a1, a2, _ := CheckDigits(string(b[:]))
		l1, l2 := legacy(string(b[:]))
		if a1 != l1 || a2 != l2 {
			t.Fatalf("%s: %c%c vs legacy %c%c", b, a1, a2, l1, l2)
		}
	}
}
