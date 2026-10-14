package iso3166

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"slices"
	"testing"

	"github.com/flaviotinococoutinho/intenza/standards/un/m49"
)

func TestGeneratedTablesMatchExtract(t *testing.T) {
	raw, err := os.ReadFile("data/iso3166.csv")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != sourceSHA256 {
		t.Fatalf("extract changed without go generate: %s", got)
	}
}

func TestBrazil(t *testing.T) {
	c, ok := ByAlpha2("br")
	if !ok || c != BR || c.Alpha3() != "BRA" || c.Numeric() != 76 || c.NumericString() != "076" || c.NamePT() != "Brasil" {
		t.Fatalf("BR: %v %s %d %s %s", ok, c.Alpha3(), c.Numeric(), c.NumericString(), c.NamePT())
	}
	latam, _ := m49.ByCode(419)
	europe, _ := m49.ByCode(150)
	if !BR.In(latam) || BR.In(europe) || !BR.In(m49.World) {
		t.Fatal("Brazil lies in Latin America, not in Europe")
	}
	if BR.Area().Code() != 5 || BR.Area().NamePT() != "América do Sul" {
		t.Fatalf("area: %d %s", BR.Area().Code(), BR.Area().NamePT())
	}
	if AQ.Area() != m49.World {
		t.Fatal("Antarctica has no region below the world")
	}
}

// Proof by exhaustion: the three keys agree for every record and every
// referenced M49 area exists.
func TestEveryCountryRoundTripsAndHasAnArea(t *testing.T) {
	n := 0
	for c := range All() {
		n++
		for _, k := range []string{c.Alpha2(), c.Alpha3(), c.NumericString()} {
			if got, err := Parse(k); err != nil || got != c {
				t.Errorf("%s: Parse(%s) = %v, %v", c, k, got, err)
			}
		}
		if !c.Area().Valid() {
			t.Errorf("%s: area %d does not exist", c, area[c.i()])
		}
		if c.Name() == "" || c.NamePT() == "" {
			t.Errorf("%s without name", c)
		}
	}
	if n != 249 {
		t.Fatalf("count = %d", n)
	}
}

func TestSearch(t *testing.T) {
	if got := Search(nil, "estados unid"); !slices.Contains(got, US) {
		t.Fatalf("estados unid: %v", got)
	}
	if got := Search(nil, "sao tome"); len(got) != 1 || got[0] != ST {
		t.Fatalf("sao tome: %v", got)
	}
}

func TestLookupsDoNotAllocate(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() {
		c, _ := ByAlpha3("BRA")
		_ = c.NamePT()
		_, _ = ByNumeric(76)
		_ = c.In(m49.World)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v", allocs)
	}
}

func BenchmarkByAlpha3(b *testing.B) {
	codes := []string{"BRA", "USA", "PRT", "JPN", "ZZZ"}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		ByAlpha3(codes[i%len(codes)])
	}
}

func BenchmarkIn(b *testing.B) {
	latam, _ := m49.ByCode(419)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		Country(1 + i%count).In(latam)
	}
}
