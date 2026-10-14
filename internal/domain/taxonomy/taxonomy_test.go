package taxonomy

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultCatalogShape(t *testing.T) {
	niches, leaves := 0, 0
	for c := range Default.All() {
		if c.Depth() == 0 {
			niches++
		}
		if c.Leaf() {
			leaves++
			if !c.MCC().Valid() || len(c.Schema().Fields) == 0 || len(c.Schema().Evidentiary()) == 0 {
				t.Errorf("%s: leaf without MCC, fields or evidentiary attributes", c.ID())
			}
		}
	}
	if niches != 3 || leaves < 12 {
		t.Fatalf("niches %d leaves %d", niches, leaves)
	}
	tcg, _ := Default.Lookup("collectibles.tcg")
	if tcg.MCC().Code() != 5945 || tcg.Wave() != 1 || tcg.Density() != Low {
		t.Fatalf("tcg: %d %d %s", tcg.MCC().Code(), tcg.Wave(), tcg.Density())
	}
	panel, _ := Default.Lookup("auto-parts.body-panels")
	if panel.Wave() != 3 || panel.Density() != High || !panel.Eligible([]Credential{KYCBasic, Business, DismantlerRegistry}) ||
		panel.Eligible([]Credential{KYCBasic, Business}) {
		t.Fatal("auto parts: wave 3, high density, dismantler registry required")
	}
	if _, ok := panel.Schema().Field("traceability_code"); !ok {
		t.Fatal("auto parts inherit the dismantled-origin preset")
	}
}

func TestForbiddenCategoriesDoNotExist(t *testing.T) {
	for _, id := range []string{"collectibles.firearms", "pharmacy", "pets.puppies", "auto-parts.brakes", ""} {
		if _, err := Default.Lookup(id); !errors.Is(err, ErrUnknownCategory) {
			t.Errorf("%q: %v", id, err)
		}
	}
	var zero Category
	if zero.Valid() {
		t.Fatal("the zero Category is not a category")
	}
}

func TestLoaderRefusesForbiddenAnchorsAndTerms(t *testing.T) {
	base := func(node string) string {
		return "version: 1\npresets: {}\ntree:\n  - id: niche\n    label: Nicho\n    wave: 1\n    density: low\n    children:\n" + node
	}
	cases := map[string]string{
		"forbidden MCC":     "      - {id: farmacia, label: Farmácia, mcc: 5912, fields: [{key: a, label: A, kind: text}]}\n",
		"forbidden term":    "      - {id: tiro, label: Munição esportiva, mcc: 5945, fields: [{key: a, label: A, kind: text}]}\n",
		"term in an enum":   "      - {id: pecas, label: Peças, mcc: 5533, fields: [{key: part, label: Peça, kind: enum, values: [door, brake]}]}\n",
		"unknown MCC":       "      - {id: x, label: X, mcc: 1, fields: [{key: a, label: A, kind: text}]}\n",
		"leaf without MCC":  "      - {id: x, label: X, fields: [{key: a, label: A, kind: text}]}\n",
		"unknown preset":    "      - {id: x, label: X, mcc: 5945, presets: [ghost]}\n",
		"unknown field key": "      - {id: x, label: X, mcc: 5945, colour: red}\n",
	}
	for name, node := range cases {
		if _, err := Parse([]byte(base(node))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	ok := "      - {id: cartas, label: Cartas, mcc: 5945, fields: [{key: a, label: A, kind: text}]}\n"
	if _, err := Parse([]byte(base(ok))); err != nil {
		t.Fatalf("control case rejected: %v", err)
	}
}

func TestPublishableGatesLeafAndWave(t *testing.T) {
	if _, err := Default.Publishable("collectibles", 3); !errors.Is(err, ErrNotLeaf) {
		t.Fatalf("niche: %v", err)
	}
	if _, err := Default.Publishable("auto-parts.body-panels", 1); !errors.Is(err, ErrNotLaunched) {
		t.Fatalf("wave: %v", err)
	}
	if c, err := Default.Publishable("collectibles.vinyl", 1); err != nil || c.ID() != "collectibles.vinyl" {
		t.Fatalf("vinyl: %v", err)
	}
}

// Proof by exhaustion over every pair of nodes: the Euler interval agrees with
// walking up the parent chain.
func TestUnderAgreesWithParentChain(t *testing.T) {
	var all []Category
	for c := range Default.All() {
		all = append(all, c)
	}
	for _, a := range all {
		for _, b := range all {
			chain := false
			for cur, ok := a, true; ok; cur, ok = cur.Parent() {
				if cur.i == b.i {
					chain = true
				}
			}
			if a.Under(b) != chain {
				t.Fatalf("%s under %s: interval %v, chain %v", a.ID(), b.ID(), a.Under(b), chain)
			}
		}
	}
	tcg, _ := Default.Lookup("collectibles.tcg")
	if tcg.Niche().ID() != "collectibles" || !strings.HasPrefix(tcg.ID(), tcg.Niche().ID()) {
		t.Fatal("niche of tcg")
	}
}
