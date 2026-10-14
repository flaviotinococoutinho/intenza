package iso18245

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"slices"
	"testing"
)

func TestGeneratedTablesMatchExtract(t *testing.T) {
	raw, err := os.ReadFile("data/mcc.csv")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != sourceSHA256 {
		t.Fatalf("extract changed without go generate: %s", got)
	}
}

func TestKnownCodes(t *testing.T) {
	m, err := Parse("5945")
	if err != nil {
		t.Fatal(err)
	}
	if m.Description() != "Hobby, Toy, and Game Shops" {
		t.Fatalf("description: %q", m.Description())
	}
	if pt, ok := m.DescriptionPT(); !ok || pt != "Lojas de hobbies, brinquedos e jogos" {
		t.Fatalf("pt: %q %v", pt, ok)
	}
	if m.Group() != MiscellaneousStores {
		t.Fatalf("group: %v", m.Group().Name())
	}
	if MCC(5661).Group() != ClothingStores || MCC(5651).Group() != ClothingStores || MCC(3001).Group() != Airlines {
		t.Fatal("groups by range")
	}
	if MCC(742).String() != "0742" {
		t.Fatal("four-digit form")
	}
	for _, bad := range []string{"", "594", "05945", "abcd", "0000"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
}

// Proof by exhaustion: the ranges partition 0–9999 with no gap and no overlap.
func TestGroupsPartitionTheCodeSpace(t *testing.T) {
	prevHi := -1
	for g := AgriculturalServices; g <= GovernmentServices; g++ {
		lo, hi := g.Range()
		if lo != prevHi+1 || hi < lo {
			t.Fatalf("%s: [%d,%d] after %d", g.Name(), lo, hi, prevHi)
		}
		prevHi = hi
	}
	if prevHi != 9999 {
		t.Fatalf("last range ends at %d", prevHi)
	}
	for code := range 10000 {
		g := MCC(code).Group()
		lo, hi := g.Range()
		if code < lo || code > hi {
			t.Fatalf("code %d mapped to %s [%d,%d]", code, g.Name(), lo, hi)
		}
	}
}

func TestEveryAssignedCodeIsFoundAndDescribed(t *testing.T) {
	n := 0
	for m := range All() {
		n++
		if got, ok := Lookup(m.Code()); !ok || got != m || m.Description() == "" {
			t.Errorf("%s", m)
		}
	}
	if n != count || n < 900 {
		t.Fatalf("count = %d", n)
	}
}

func TestSearch(t *testing.T) {
	if got := Search(nil, "hobby toy"); !slices.Contains(got, MCC(5945)) {
		t.Fatalf("hobby toy: %v", got)
	}
	if got := Search(nil, "farmacias"); !slices.Contains(got, MCC(5912)) {
		t.Fatalf("farmacias via portuguese: %v", got)
	}
}

func BenchmarkDescription(b *testing.B) {
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		_ = MCC(5900 + i%100).Description()
	}
}
