package iso639

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"slices"
	"testing"
)

func TestGeneratedTablesMatchExtract(t *testing.T) {
	raw, err := os.ReadFile("data/iso639.csv")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != sourceSHA256 {
		t.Fatalf("extract changed without go generate: %s", got)
	}
}

func TestPortugueseAndTheBTPairs(t *testing.T) {
	if l, ok := ByAlpha2("PT"); !ok || l != PT || l.Alpha3B() != "por" || l.NamePT() != "português" {
		t.Fatalf("pt: %v %s %s", ok, l.Alpha3B(), l.NamePT())
	}
	ger, _ := ByAlpha3("ger")
	deu, _ := ByAlpha3("deu")
	if ger != deu || ger != DE || DE.Alpha3B() != "ger" || DE.Alpha3T() != "deu" {
		t.Fatalf("german B/T: %v %v", ger, deu)
	}
	if DE.String() != "de" {
		t.Fatalf("String prefers alpha-2: %s", DE)
	}
	ain, ok := ByAlpha3("ain")
	if !ok {
		t.Fatal("ainu exists in 639-2")
	}
	if _, ok := ain.Alpha2(); ok {
		t.Fatal("ainu has no alpha-2")
	}
}

// Proof by exhaustion: every alpha-3 (both forms) and alpha-2 resolves back.
func TestEveryLanguageRoundTrips(t *testing.T) {
	withAlpha2 := 0
	for l := range All() {
		if got, ok := ByAlpha3(l.Alpha3B()); !ok || got != l {
			t.Errorf("B %s", l.Alpha3B())
		}
		if got, ok := ByAlpha3(l.Alpha3T()); !ok || got != l {
			t.Errorf("T %s", l.Alpha3T())
		}
		if a2, ok := l.Alpha2(); ok {
			withAlpha2++
			if got, ok := ByAlpha2(a2); !ok || got != l {
				t.Errorf("alpha-2 %s", a2)
			}
		}
	}
	if withAlpha2 < 180 || withAlpha2 > 190 {
		t.Fatalf("ISO 639-1 has about 184 codes, got %d", withAlpha2)
	}
}

func TestSearch(t *testing.T) {
	if got := Search(nil, "portug"); !slices.Contains(got, PT) {
		t.Fatalf("portug: %v", got)
	}
	if got := Search(nil, "japones"); !slices.Contains(got, JA) {
		t.Fatalf("japones without accent: %v", got)
	}
}
