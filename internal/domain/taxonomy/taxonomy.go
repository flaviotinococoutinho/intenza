//intenza:paradigm functional-core

// Package taxonomy is the curated category tree: every node anchored in an
// ISO 18245 merchant category, inheriting launch wave, regulatory density,
// seller requirements and attribute presets from its ancestors.
//
// The denylist is structural. Forbidden categories (weapons, medicines, live
// animals, tobacco, alcohol, gambling, safety-critical car parts) do not exist
// in the tree: the loader refuses forbidden MCCs and terms, and a Category can
// only be obtained by looking a node up, so an intent in a forbidden category
// is unrepresentable rather than merely rejected.
//
// The tree is stored in DFS preorder with Euler intervals: every subtree is a
// contiguous slice and "is X under Y" costs two comparisons.
package taxonomy

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/flaviotinococoutinho/intenza/internal/domain/schema"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso18245"
)

// Density is how much regulation weighs on a niche; niches launch in order.
type Density string

const (
	Low    Density = "low"
	Medium Density = "medium"
	High   Density = "high"
)

// Credential is something a seller must hold to answer intents in a category.
type Credential string

const (
	KYCBasic           Credential = "kyc-basic"           // verified CPF or CNPJ
	Business           Credential = "business"            // acts as a company (CNPJ)
	DismantlerRegistry Credential = "dismantler-registry" // dismantler registered with the state transit authority
)

var credentials = map[Credential]bool{KYCBasic: true, Business: true, DismantlerRegistry: true}

// ErrUnknownCategory is returned for ids outside the tree, forbidden ones included.
var ErrUnknownCategory = errors.New("taxonomy: category does not exist")

// ErrNotLeaf is returned when an intent targets an inner node.
var ErrNotLeaf = errors.New("taxonomy: intents target leaf categories")

// ErrNotLaunched is returned for categories of a wave not open yet.
var ErrNotLaunched = errors.New("taxonomy: category belongs to a wave not launched")

// forbiddenMCC lists merchant categories that cannot anchor any node.
var forbiddenMCC = []int{
	4829, // money orders, wire transfers
	5122, // drugs (wholesale)
	5912, // drug stores and pharmacies
	5921, // package stores: beer, wine, liquor
	5933, // pawn shops
	5967, // inbound teleservices (adult content)
	5993, // cigar stores
	5995, // pet shops (live animals)
	6051, // quasi-cash, foreign currency
	6211, // securities brokers
	7273, // dating and escort services
	7297, // massage parlors
	7995, // betting and gambling
}

// forbiddenTerms is a tripwire over folded ids, labels and enum values: a
// curator mistake that slips a forbidden item in fails the build.
var forbiddenTerms = []string{
	"arma", "municao", "pistola", "revolver", "fuzil", "espingarda", "weapon", "firearm", "ammo",
	"medicamento", "remedio", "farmaco", "drug", "pharma",
	"animal vivo", "filhote", "live animal", "pet",
	"tabaco", "cigarro", "charuto", "tobacco", "cigar", "vape",
	"bebida alcoolica", "cerveja", "vinho", "whisky", "alcohol", "liquor",
	"aposta", "betting", "gambling",
	"airbag", "freio", "brake", "cinto de seguranca", "seatbelt", "seat-belt", "direcao", "steering", "suspensao", "suspension",
}

type rawNode struct {
	ID       string         `yaml:"id"`
	Label    string         `yaml:"label"`
	MCC      int            `yaml:"mcc,omitempty"`
	Wave     int            `yaml:"wave,omitempty"`
	Density  Density        `yaml:"density,omitempty"`
	Requires []Credential   `yaml:"requires,omitempty"`
	Presets  []string       `yaml:"presets,omitempty"`
	Fields   []schema.Field `yaml:"fields,omitempty"`
	Children []rawNode      `yaml:"children,omitempty"`
}

type rawCatalog struct {
	Version int                       `yaml:"version"`
	Presets map[string][]schema.Field `yaml:"presets"`
	Tree    []rawNode                 `yaml:"tree"`
}

type node struct {
	id, label string
	mcc       iso18245.MCC
	wave      int
	density   Density
	requires  []Credential
	parent    int // -1 for roots
	depth     int
	post      int // end of the subtree in preorder (exclusive)
	leaf      bool
	schema    schema.Schema
}

// Tree is the immutable category tree.
type Tree struct {
	version int
	nodes   []node
	byID    map[string]int
}

//go:embed catalog.yaml
var catalogYAML []byte

// Default is the curated catalog embedded in the binary.
var Default = func() *Tree {
	t, err := Parse(catalogYAML)
	if err != nil {
		panic(err)
	}
	return t
}()

// Parse builds a tree from catalog YAML, enforcing the denylist and the
// inheritance rules. It is exported so tests can try broken catalogs.
func Parse(src []byte) (*Tree, error) {
	var raw rawCatalog
	dec := yaml.NewDecoder(bytes.NewReader(src))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("taxonomy: %w", err)
	}
	t := &Tree{version: raw.Version, byID: map[string]int{}}
	for _, r := range raw.Tree {
		if err := t.add(raw, r, -1, "", nil, nil); err != nil {
			return nil, err
		}
	}
	return t, nil
}

func (t *Tree) add(cat rawCatalog, r rawNode, parent int, prefix string, inherited []schema.Field, presetsSeen []string) error {
	id := r.ID
	if prefix != "" {
		id = prefix + "." + r.ID
	}
	if r.ID == "" || strings.ContainsAny(r.ID, ". /") || r.Label == "" {
		return fmt.Errorf("taxonomy: nó %q sem id válido ou rótulo", id)
	}
	if _, dup := t.byID[id]; dup {
		return fmt.Errorf("taxonomy: nó %q duplicado", id)
	}
	n := node{id: id, label: r.Label, parent: parent, density: r.Density, wave: r.Wave}
	if parent >= 0 {
		p := t.nodes[parent]
		n.depth = p.depth + 1
		n.mcc, n.requires = p.mcc, slices.Clone(p.requires)
		if n.wave == 0 {
			n.wave = p.wave
		}
		if n.density == "" {
			n.density = p.density
		}
	}
	if r.MCC != 0 {
		m, ok := iso18245.Lookup(r.MCC)
		if !ok {
			return fmt.Errorf("taxonomy: %s ancora em MCC inexistente %d", id, r.MCC)
		}
		if slices.Contains(forbiddenMCC, r.MCC) {
			return fmt.Errorf("taxonomy: %s ancora em MCC bloqueado %d (%s)", id, r.MCC, m.Description())
		}
		n.mcc = m
	}
	for _, c := range r.Requires {
		if !credentials[c] {
			return fmt.Errorf("taxonomy: %s exige credencial desconhecida %q", id, c)
		}
		if !slices.Contains(n.requires, c) {
			n.requires = append(n.requires, c)
		}
	}
	if n.wave < 1 || n.wave > 3 {
		return fmt.Errorf("taxonomy: %s sem onda de lançamento válida", id)
	}
	switch n.density {
	case Low, Medium, High:
	default:
		return fmt.Errorf("taxonomy: %s sem densidade regulatória", id)
	}
	fields := slices.Clone(inherited)
	for _, p := range r.Presets {
		preset, ok := cat.Presets[p]
		if !ok {
			return fmt.Errorf("taxonomy: %s usa preset inexistente %q", id, p)
		}
		if slices.Contains(presetsSeen, p) {
			return fmt.Errorf("taxonomy: %s repete o preset %q", id, p)
		}
		presetsSeen = append(slices.Clone(presetsSeen), p)
		fields = append(fields, preset...)
	}
	fields = append(fields, r.Fields...)
	if err := tripwire(id, r.Label, fields); err != nil {
		return err
	}
	idx := len(t.nodes)
	t.nodes = append(t.nodes, n)
	t.byID[id] = idx
	if len(r.Children) == 0 {
		if n.mcc == 0 {
			return fmt.Errorf("taxonomy: folha %s sem MCC", id)
		}
		s, err := schema.New(id, t.version, fields)
		if err != nil {
			return err
		}
		t.nodes[idx].leaf, t.nodes[idx].schema = true, s
	}
	for _, c := range r.Children {
		if err := t.add(cat, c, idx, id, fields, presetsSeen); err != nil {
			return err
		}
	}
	t.nodes[idx].post = len(t.nodes)
	return nil
}

func tripwire(id, label string, fields []schema.Field) error {
	texts := []string{id, label}
	for _, f := range fields {
		texts = append(texts, f.Key, f.Label)
		texts = append(texts, f.Values...)
	}
	for _, s := range texts {
		folded := " " + foldASCII(s) + " "
		for _, term := range forbiddenTerms {
			if strings.Contains(folded, " "+term+" ") {
				return fmt.Errorf("taxonomy: %s contém termo bloqueado %q em %q", id, term, s)
			}
		}
	}
	return nil
}

// foldASCII lowercases and strips the Portuguese diacritics, turning
// separators into spaces, so "Munição" and "municao" meet.
func foldASCII(s string) string {
	r := strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "é", "e", "ê", "e", "í", "i", "ó", "o", "ô", "o", "õ", "o",
		"ú", "u", "ü", "u", "ç", "c", "_", " ", ".", " ", "-", " ", "/", " ", "(", " ", ")", " ")
	return r.Replace(strings.ToLower(s))
}

// Category is a node of the tree. It can only be obtained from a Tree.
type Category struct {
	t *Tree
	i int
}

// Lookup returns the category with the dotted id ("collectibles.tcg").
func (t *Tree) Lookup(id string) (Category, error) {
	i, ok := t.byID[id]
	if !ok {
		return Category{}, fmt.Errorf("%w: %q", ErrUnknownCategory, id)
	}
	return Category{t, i}, nil
}

// Publishable returns a leaf category open in the active wave: the only kind
// of category an intent may target.
func (t *Tree) Publishable(id string, activeWave int) (Category, error) {
	c, err := t.Lookup(id)
	if err != nil {
		return Category{}, err
	}
	if !c.Leaf() {
		return Category{}, fmt.Errorf("%w: %q", ErrNotLeaf, id)
	}
	if c.Wave() > activeWave {
		return Category{}, fmt.Errorf("%w: %q é da onda %d", ErrNotLaunched, id, c.Wave())
	}
	return c, nil
}

// Version is the catalog version stamped on every schema.
func (t *Tree) Version() int { return t.version }

// All yields every node in preorder.
func (t *Tree) All() iter.Seq[Category] {
	return func(yield func(Category) bool) {
		for i := range t.nodes {
			if !yield(Category{t, i}) {
				return
			}
		}
	}
}

func (c Category) n() *node { return &c.t.nodes[c.i] }

// Valid reports whether c came from a tree.
func (c Category) Valid() bool { return c.t != nil }

// ID is the dotted path.
func (c Category) ID() string { return c.n().id }

// Label is the Portuguese name.
func (c Category) Label() string { return c.n().label }

// MCC is the merchant category the node is anchored in.
func (c Category) MCC() iso18245.MCC { return c.n().mcc }

// Wave is the launch wave.
func (c Category) Wave() int { return c.n().wave }

// Density is the regulatory density.
func (c Category) Density() Density { return c.n().density }

// Leaf reports whether the node has no children.
func (c Category) Leaf() bool { return c.n().leaf }

// Depth is 0 for niches.
func (c Category) Depth() int { return c.n().depth }

// Requires lists the credentials a seller needs here.
func (c Category) Requires() []Credential { return slices.Clone(c.n().requires) }

// Schema is the flattened attribute schema of a leaf.
func (c Category) Schema() schema.Schema { return c.n().schema }

// Parent is the enclosing node; niches have none.
func (c Category) Parent() (Category, bool) {
	if p := c.n().parent; p >= 0 {
		return Category{c.t, p}, true
	}
	return Category{}, false
}

// Under reports whether c lies in the subtree of other (a node is under itself).
func (c Category) Under(other Category) bool {
	return c.t == other.t && other.i <= c.i && c.i < c.t.nodes[other.i].post
}

// Niche is the root ancestor.
func (c Category) Niche() Category {
	for {
		p, ok := c.Parent()
		if !ok {
			return c
		}
		c = p
	}
}

// Eligible reports whether a seller holding the credentials may answer here.
func (c Category) Eligible(held []Credential) bool {
	for _, need := range c.n().requires {
		if !slices.Contains(held, need) {
			return false
		}
	}
	return true
}
