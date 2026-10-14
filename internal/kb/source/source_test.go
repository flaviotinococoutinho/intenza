package source

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/flaviotinococoutinho/intenza/internal/kb"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScanCodeReadsParadigmImportsSelectorsAndTests(t *testing.T) {
	root := t.TempDir()
	write(t, root, "core/core.go", `//intenza:paradigm functional-core

// Package core is a fixture.
package core

import (
	"time"
	r "math/rand/v2"
)

type Clock struct{}

func (Clock) Tick() {}

func Stamp() time.Time { _ = r.IntN(3); return time.Now() }
`)
	write(t, root, "core/core_test.go", `package core

import "testing"

func TestStamp(t *testing.T) {}
func BenchmarkStamp(b *testing.B) {}
`)
	symbols, tests, pkgs, err := ScanCode(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"core.Clock", "core.Stamp", "core.Clock.Tick"} {
		if !symbols[s] {
			t.Errorf("symbol %s not indexed", s)
		}
	}
	if !tests["core.TestStamp"] || !tests["core.BenchmarkStamp"] {
		t.Errorf("tests not indexed: %v", tests)
	}
	if len(pkgs) != 1 || pkgs[0].Paradigm != kb.FunctionalCore {
		t.Fatalf("paradigm not read: %+v", pkgs)
	}
	var sawNow, sawRand bool
	for _, s := range pkgs[0].Selectors {
		sawNow = sawNow || (s.Path == "time" && s.Name == "Now")
		sawRand = sawRand || (s.Path == "math/rand/v2" && s.Name == "IntN")
	}
	if !sawNow || !sawRand {
		t.Fatalf("selectors not resolved through aliases: %+v", pkgs[0].Selectors)
	}
	r := kb.Validate(kb.Base{}, kb.Facts{Packages: pkgs})
	if r.Errors() != 2 {
		t.Fatalf("want 2 purity errors (time.Now, rand.IntN), got %v", r.Findings)
	}
}

func TestLoadExperimentsPairsSpecsWithResults(t *testing.T) {
	root := t.TempDir()
	write(t, root, "chaos/experiments/a.yaml", "id: kafka-outage\ntitle: x\n")
	write(t, root, "chaos/experiments/b.yaml", "id: redis-outage\ntitle: y\n")
	write(t, root, "chaos/results/kafka-outage.json", `{"verdict":"held","finished_at":"2026-10-14T00:00:00Z"}`)
	exps, results, err := LoadExperiments(root)
	if err != nil {
		t.Fatal(err)
	}
	if !exps["kafka-outage"] || !exps["redis-outage"] {
		t.Fatalf("experiments: %v", exps)
	}
	if results["kafka-outage"].Verdict != "held" {
		t.Fatalf("results: %v", results)
	}
	if _, ok := results["redis-outage"]; ok {
		t.Fatal("experiment without result must not have one")
	}
}

func TestLoadBaseRejectsUnknownKeys(t *testing.T) {
	root := t.TempDir()
	write(t, root, "knowledge/ontology.yaml", "concepts:\n  - id: a\n    lable: typo\n")
	write(t, root, "knowledge/claims.yaml", "claims: []\n")
	write(t, root, "knowledge/risks.yaml", "risks: []\n")
	if _, err := LoadBase(root); err == nil {
		t.Fatal("a typo in a key must not pass silently")
	}
}

// The repository's own knowledge base must be consistent with its code.
func TestRepositoryKnowledgeBaseIsConsistent(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	base, err := LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := Gather(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := kb.Validate(base, facts)
	for _, f := range r.Findings {
		if f.Severity == kb.SevError {
			t.Error(f)
		}
	}
}
