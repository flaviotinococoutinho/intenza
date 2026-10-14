package standards

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

func TestEveryRecordIsCompleteAndPointsAtItsPackage(t *testing.T) {
	seen := map[string]bool{}
	kinds := map[meta.Kind]bool{meta.CodeList: true, meta.Classification: true, meta.Identifier: true, meta.Grammar: true, meta.IntervalMap: true}
	for _, s := range All() {
		if seen[s.ID] {
			t.Errorf("duplicate id %s", s.ID)
		}
		seen[s.ID] = true
		if s.Code == "" || s.Title == "" || s.Authority == "" || s.Edition == "" || s.Structure == "" || s.Verification == "" {
			t.Errorf("%s: incomplete record", s.ID)
		}
		if !kinds[s.Kind] {
			t.Errorf("%s: kind %q", s.ID, s.Kind)
		}
		if len(s.Sources) == 0 {
			t.Errorf("%s: no source", s.ID)
		}
		if fi, err := os.Stat(filepath.FromSlash(s.Package)); err != nil || !fi.IsDir() {
			t.Errorf("%s: package %s not found", s.ID, s.Package)
		}
		if got, ok := ByID(s.ID); !ok || got.Code != s.Code {
			t.Errorf("ByID(%s)", s.ID)
		}
	}
}

func TestWeakestLinkGovernsTheChain(t *testing.T) {
	s := meta.Standard{Sources: []meta.Source{{Grounding: meta.Primary}, {Grounding: meta.Reconstructed}}}
	if s.Weakest() != meta.Reconstructed {
		t.Fatal("a chain is as strong as its weakest link")
	}
	if (meta.Standard{}).Weakest() != meta.Reconstructed {
		t.Fatal("no source means no grounding")
	}
}
