package succinct

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// Property: for any key set, Index returns the position of the key in sorted
// order and rejects every non-member of the universe.
func TestRankIndexIsPositionInSortedKeys(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := range 200 {
		universe := uint32(1 + rng.IntN(20000))
		seen := map[uint32]bool{}
		var keys []uint32
		for range rng.IntN(int(min(universe, 2000)) + 1) {
			k := uint32(rng.IntN(int(universe)))
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		r := Build(universe, keys)
		if r.Len() != len(keys) {
			t.Fatalf("trial %d: Len %d, want %d", trial, r.Len(), len(keys))
		}
		for k := range universe {
			i, ok := r.Index(k)
			pos, found := slices.BinarySearch(keys, k)
			if ok != found || (ok && i != pos) {
				t.Fatalf("trial %d: Index(%d) = %d,%v; want %d,%v", trial, k, i, ok, pos, found)
			}
		}
		if _, ok := r.Index(universe + 64); ok {
			t.Fatalf("trial %d: key beyond the universe accepted", trial)
		}
	}
}

func TestAlphaPackingIsCaseInsensitiveAndStrict(t *testing.T) {
	if a, _ := Alpha2("br"); a != 1*26+17 {
		t.Fatalf("Alpha2(br) = %d", a)
	}
	a, _ := Alpha3("BRL")
	b, _ := Alpha3("brl")
	if a != b {
		t.Fatal("case must not matter")
	}
	for _, bad := range []string{"", "B", "B1", "BRLX", "B@", "[]", "ÁB"} {
		if _, ok := Alpha2(bad); ok && len(bad) == 2 {
			t.Errorf("Alpha2(%q) accepted", bad)
		}
		if _, ok := Alpha3(bad); ok {
			t.Errorf("Alpha3(%q) accepted", bad)
		}
	}
}

func TestDigits(t *testing.T) {
	cases := map[string]uint32{"0": 0, "076": 76, "9999": 9999}
	for in, want := range cases {
		if got, ok := Digits(in); !ok || got != want {
			t.Errorf("Digits(%q) = %d,%v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "12345", "1a", "-1", " 1"} {
		if _, ok := Digits(bad); ok {
			t.Errorf("Digits(%q) accepted", bad)
		}
	}
}

func TestTokensMatchIsAndOfPrefixes(t *testing.T) {
	// records: 0 "estados unidos", 1 "emirados arabes unidos", 2 "estonia"
	tk := Tokens{
		Blob: "arabesemiradosestadosestoniaunidosunidos",
		Off:  []uint32{0, 6, 14, 21, 28, 34, 40},
		Post: []uint16{1, 1, 0, 2, 0, 1},
	}
	set, _ := tk.Match([]byte("unidos"))
	if !set.Has(0) || !set.Has(1) || set.Has(2) {
		t.Fatalf("unidos: %v", set[0])
	}
	set, _ = tk.Match([]byte("est uni"))
	if !set.Has(0) || set.Has(1) || set.Has(2) {
		t.Fatalf("est uni: %v", set[0])
	}
	if _, ok := tk.Match(nil); ok {
		t.Fatal("empty query matches nothing")
	}
}

func BenchmarkRankIndex(b *testing.B) {
	keys := make([]uint32, 0, 1000)
	for k := uint32(0); k < 10000; k += 10 {
		keys = append(keys, k)
	}
	r := Build(10000, keys)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		r.Index(uint32(i % 10000))
	}
}
