package m49

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

func TestGeneratedTablesMatchExtract(t *testing.T) {
	raw, err := os.ReadFile("data/m49.csv")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != sourceSHA256 {
		t.Fatalf("extract changed without go generate: %s", got)
	}
}

// Proof by exhaustion over every pair: the Euler-interval test agrees with
// walking the parent chain.
func TestContainsAgreesWithParentChain(t *testing.T) {
	for a := Area(1); int(a) <= count; a++ {
		for b := Area(1); int(b) <= count; b++ {
			viaChain := false
			for anc := range b.Ancestors() {
				if anc == a {
					viaChain = true
				}
			}
			if got := a.Contains(b); got != viaChain {
				t.Fatalf("%s contains %s: interval says %v, chain says %v", a.Name(), b.Name(), got, viaChain)
			}
		}
	}
}

func TestHierarchy(t *testing.T) {
	sa, ok := ByCode(5)
	if !ok || sa.NamePT() != "América do Sul" || sa.Depth() != 3 {
		t.Fatalf("south america: %v %s %d", ok, sa.NamePT(), sa.Depth())
	}
	var chain []int
	for a := range sa.Ancestors() {
		chain = append(chain, a.Code())
	}
	want := []int{5, 419, 19, 1}
	if len(chain) != len(want) {
		t.Fatalf("chain = %v", chain)
	}
	for i := range want {
		if chain[i] != want[i] {
			t.Fatalf("chain = %v, want %v", chain, want)
		}
	}
	if World.Depth() != 0 {
		t.Fatal("world is the root")
	}
	if _, ok := World.Parent(); ok {
		t.Fatal("world has no parent")
	}
}
