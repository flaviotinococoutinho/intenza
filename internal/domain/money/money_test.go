package money

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/flaviotinococoutinho/intenza/standards/iso/iso4217"
)

func TestParseAndDecimalRoundTrip(t *testing.T) {
	for _, s := range []string{"0.00", "0.05", "1234.56", "-7.10"} {
		m, err := Parse(s, iso4217.BRL)
		if err != nil || m.Decimal() != s {
			t.Errorf("%s → %v %v", s, m.Decimal(), err)
		}
	}
	if m, _ := Parse("12.5", iso4217.BRL); m.Minor != 1250 {
		t.Fatalf("12.5 = %d centavos", m.Minor)
	}
	if m, _ := Parse("7", iso4217.JPY); m.Decimal() != "7" {
		t.Fatal("yen has no decimals")
	}
	for _, bad := range []string{"", "1.234", "abc", ".5"} {
		if _, err := Parse(bad, iso4217.BRL); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := New(1, iso4217.XAU); !errors.Is(err, ErrCurrency) {
		t.Fatal("gold has no minor units")
	}
}

func TestAddGuardsCurrencyAndOverflow(t *testing.T) {
	if _, err := BRL(1).Add(Money{1, iso4217.USD}); !errors.Is(err, ErrCurrency) {
		t.Fatal("mixed currencies")
	}
	if _, err := BRL(math.MaxInt64).Add(BRL(1)); !errors.Is(err, ErrOverflow) {
		t.Fatal("overflow")
	}
	if s, _ := BRL(150).Add(BRL(250)); s != BRL(400) {
		t.Fatalf("sum %v", s)
	}
}

func TestJSON(t *testing.T) {
	b, _ := json.Marshal(BRL(123456))
	if string(b) != `{"amount":"1234.56","currency":"BRL"}` {
		t.Fatalf("%s", b)
	}
	var m Money
	if err := json.Unmarshal([]byte(`{"amount":"9.90","currency":"986"}`), &m); err != nil || m != BRL(990) {
		t.Fatalf("%v %v", m, err)
	}
}
