package iso4217

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// The compiled tables must come from the committed extract: the first link of
// the chain source → extract → generated code.
func TestGeneratedTablesMatchExtract(t *testing.T) {
	raw, err := os.ReadFile("data/iso4217.csv")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != sourceSHA256 {
		t.Fatalf("extract changed without go generate: %s != %s", got, sourceSHA256)
	}
}

func TestKnownCurrencies(t *testing.T) {
	brl, ok := ByAlpha("brl")
	if !ok || brl != BRL {
		t.Fatalf("ByAlpha(brl) = %v, %v", brl, ok)
	}
	if BRL.Numeric() != 986 || BRL.NumericString() != "986" || BRL.NamePT() != "Real brasileiro" {
		t.Fatalf("BRL: %d %q %q", BRL.Numeric(), BRL.NumericString(), BRL.NamePT())
	}
	if d, ok := BRL.MinorUnits(); !ok || d != 2 {
		t.Fatalf("BRL minor units = %d, %v", d, ok)
	}
	if _, ok := XAU.MinorUnits(); ok {
		t.Fatal("gold has no minor units (N.A.)")
	}
	if !CLF.Fund() || BRL.Fund() {
		t.Fatal("fund flags")
	}
	if d, _ := JPY.MinorUnits(); d != 0 {
		t.Fatal("yen has no decimals")
	}
	if d, _ := KWD.MinorUnits(); d != 3 {
		t.Fatal("kuwaiti dinar has three decimals")
	}
	if c, err := Parse("008"); err != nil || c != ALL {
		t.Fatalf("Parse(008) = %v, %v", c, err)
	}
	if EUR.Entities() < 20 {
		t.Fatalf("euro entities = %d", EUR.Entities())
	}
}

// Proof by exhaustion over the finite list: every key round-trips.
func TestEveryCurrencyRoundTrips(t *testing.T) {
	n := 0
	for c := range All() {
		n++
		if got, ok := ByAlpha(c.Alpha()); !ok || got != c {
			t.Errorf("alpha %s", c.Alpha())
		}
		if got, ok := ByNumeric(c.Numeric()); !ok || got != c {
			t.Errorf("numeric %d", c.Numeric())
		}
		if got, err := Parse(c.NumericString()); err != nil || got != c {
			t.Errorf("numeric string %s", c.NumericString())
		}
		if c.Name() == "" || c.NamePT() == "" {
			t.Errorf("%s without name", c)
		}
	}
	if n != count || n < 150 {
		t.Fatalf("count = %d", n)
	}
}

func TestRejectsWhatIsNotInTheList(t *testing.T) {
	for _, s := range []string{"", "BR", "BRLL", "XYZ", "000", "1000", "br1"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) accepted", s)
		}
	}
	var zero Currency
	if zero.Valid() || zero.Alpha() != "" || zero.Numeric() != 0 {
		t.Fatal("zero value must be inert")
	}
}

func TestTextMarshalling(t *testing.T) {
	var v struct{ C Currency }
	if err := json.Unmarshal([]byte(`{"C":"986"}`), &v); err != nil || v.C != BRL {
		t.Fatalf("unmarshal: %v %v", v.C, err)
	}
	out, _ := json.Marshal(v)
	if string(out) != `{"C":"BRL"}` {
		t.Fatalf("marshal: %s", out)
	}
}

func TestSearchIsAccentAndCaseInsensitive(t *testing.T) {
	got := Search(nil, "REAL bras")
	if !slices.Contains(got, BRL) {
		t.Fatalf("search: %v", got)
	}
	if got := Search(nil, "dolar americano"); !slices.Contains(got, USD) {
		t.Fatalf("search pt without accent: %v", got)
	}
}

func TestLookupsDoNotAllocate(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() {
		c, _ := ByAlpha("BRL")
		_ = c.Name()
		_ = c.NumericString()
		_, _ = ByNumeric(986)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v", allocs)
	}
}

func BenchmarkByAlpha(b *testing.B) {
	codes := []string{"BRL", "USD", "EUR", "JPY", "XAU", "zzz"}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		ByAlpha(codes[i%len(codes)])
	}
}

// The baseline a map would give, for comparison with the rank dictionary.
func BenchmarkMapBaseline(b *testing.B) {
	m := map[string]Currency{}
	for c := range All() {
		m[c.Alpha()] = c
	}
	codes := []string{"BRL", "USD", "EUR", "JPY", "XAU", "zzz"}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		_ = m[codes[i%len(codes)]]
	}
}

func BenchmarkSearch(b *testing.B) {
	dst := make([]Currency, 0, 8)
	b.ReportAllocs()
	for b.Loop() {
		dst = Search(dst[:0], "real")
	}
}
