package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/flaviotinococoutinho/intenza/standards/internal/succinct"
)

func mustAlpha2(s string) uint32 {
	k, ok := succinct.Alpha2(s)
	if !ok {
		panic("alpha-2 inválido: " + s)
	}
	return k
}

func mustAlpha3(s string) uint32 {
	k, ok := succinct.Alpha3(s)
	if !ok {
		panic("alpha-3 inválido: " + s)
	}
	return k
}

// secondaryIndex emits a Rank over keys of records plus the permutation from
// key order to record order.
func (e *emitter) secondaryIndex(name string, universe uint32, keyOf func(rec int) (uint32, bool), n int) {
	type kv struct {
		k   uint32
		rec int
	}
	var kvs []kv
	for rec := range n {
		if k, ok := keyOf(rec); ok {
			kvs = append(kvs, kv{k, rec})
		}
	}
	slices.SortFunc(kvs, func(a, b kv) int { return int(a.k) - int(b.k) })
	keys := make([]uint32, len(kvs))
	order := make([]int, len(kvs))
	for i, x := range kvs {
		if i > 0 && kvs[i-1].k == x.k {
			panic(fmt.Sprintf("%s: chave duplicada %d", name, x.k))
		}
		keys[i], order[i] = x.k, x.rec
	}
	e.rank(name+"Rank", universe, keys)
	e.ints(name+"Order", "uint16", order)
}

func genISO4217() error {
	rows, digest, err := readExtract("data/iso4217.csv", 7)
	if err != nil {
		return err
	}
	e := newEmitter("iso4217", "data/iso4217.csv", digest)
	n := len(rows)
	e.line("const count = %d", n)
	e.line("")
	e.line("// Currency handles, in alphabetic-code order.")
	e.line("const (")
	for i, r := range rows {
		e.line("\t%s Currency = %d // %s", r[0], i+1, r[5])
	}
	e.line(")")
	e.line("")
	alpha, num, names, namesPT := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	numeric, minor, entities, fund := make([]int, n), make([]int, n), make([]int, n), make([]int, n)
	keys := make([]uint32, n)
	search := make([][]string, n)
	for i, r := range rows {
		alpha[i], num[i] = r[0], r[1]
		numeric[i], minor[i], entities[i] = atoi(r[1]), atoi(r[2]), atoi(r[4])
		if r[3] == "true" {
			fund[i] = 1
		}
		names[i], namesPT[i] = r[5], r[6]
		keys[i] = mustAlpha3(r[0])
		search[i] = []string{r[0], r[5], r[6]}
		if i > 0 && rows[i-1][0] >= r[0] {
			return fmt.Errorf("iso4217: extract fora de ordem em %s", r[0])
		}
	}
	e.fixed("alphaCodes", alpha)
	e.fixed("numericCodes", num)
	e.ints("numeric", "uint16", numeric)
	e.ints("minorUnits", "int8", minor)
	e.ints("entities", "uint8", entities)
	e.ints("fund", "uint8", fund)
	e.blob("nameEN", names)
	e.blob("namePT", namesPT)
	e.rank("alphaRank", 26*26*26, keys)
	e.secondaryIndex("num", 1000, func(rec int) (uint32, bool) { return uint32(numeric[rec]), true }, n)
	e.tokens(search)
	return e.write()
}

func genISO3166() error {
	rows, digest, err := readExtract("data/iso3166.csv", 6)
	if err != nil {
		return err
	}
	e := newEmitter("iso3166", "data/iso3166.csv", digest)
	n := len(rows)
	e.line("const count = %d", n)
	e.line("")
	e.line("// Country handles, in alpha-2 order.")
	e.line("const (")
	for i, r := range rows {
		e.line("\t%s Country = %d // %s", r[0], i+1, r[4])
	}
	e.line(")")
	e.line("")
	a2, a3, num, en, pt := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	numeric, area := make([]int, n), make([]int, n)
	keys := make([]uint32, n)
	search := make([][]string, n)
	for i, r := range rows {
		a2[i], a3[i], num[i], en[i], pt[i] = r[0], r[1], r[2], r[3], r[4]
		numeric[i], area[i] = atoi(r[2]), atoi(r[5])
		keys[i] = mustAlpha2(r[0])
		search[i] = []string{r[0], r[1], r[3], r[4]}
	}
	e.fixed("alpha2Codes", a2)
	e.fixed("alpha3Codes", a3)
	e.fixed("numericCodes", num)
	e.ints("numeric", "uint16", numeric)
	e.ints("area", "uint16", area)
	e.blob("nameEN", en)
	e.blob("namePT", pt)
	e.rank("alpha2Rank", 26*26, keys)
	e.secondaryIndex("alpha3", 26*26*26, func(rec int) (uint32, bool) { return mustAlpha3(a3[rec]), true }, n)
	e.secondaryIndex("num", 1000, func(rec int) (uint32, bool) { return uint32(numeric[rec]), true }, n)
	e.tokens(search)
	return e.write()
}

func genM49() error {
	rows, digest, err := readExtract("data/m49.csv", 4)
	if err != nil {
		return err
	}
	e := newEmitter("m49", "data/m49.csv", digest)
	n := len(rows)
	idx := map[int]int{}
	codes := make([]int, n)
	for i, r := range rows {
		codes[i] = atoi(r[0])
		idx[codes[i]] = i
	}
	parent := make([]int, n)
	children := make([][]int, n)
	root := -1
	for i, r := range rows {
		if r[1] == "" {
			root = i
			continue
		}
		p, ok := idx[atoi(r[1])]
		if !ok {
			return fmt.Errorf("m49: pai %s inexistente", r[1])
		}
		parent[i] = p + 1
		children[p] = append(children[p], i)
	}
	if root < 0 {
		return fmt.Errorf("m49: sem raiz")
	}
	// Euler tour: pre/post numbering makes containment two comparisons.
	pre, post, depth := make([]int, n), make([]int, n), make([]int, n)
	clock := 0
	var walk func(i, d int)
	walk = func(i, d int) {
		pre[i], depth[i] = clock, d
		clock++
		for _, c := range children[i] {
			walk(c, d+1)
		}
		post[i] = clock
	}
	walk(root, 0)
	en, pt := make([]string, n), make([]string, n)
	keys := make([]uint32, n)
	for i, r := range rows {
		en[i], pt[i] = r[2], r[3]
		keys[i] = uint32(codes[i])
	}
	e.line("const count = %d", n)
	e.line("")
	e.line("// World is the root of the M49 hierarchy.")
	e.line("const World Area = %d", root+1)
	e.line("")
	e.ints("codes", "uint16", codes)
	e.ints("parent", "uint16", parent)
	e.ints("pre", "uint16", pre)
	e.ints("post", "uint16", post)
	e.ints("depth", "uint8", depth)
	e.blob("nameEN", en)
	e.blob("namePT", pt)
	e.rank("codeRank", 1000, keys)
	return e.write()
}

func genISO639() error {
	rows, digest, err := readExtract("data/iso639.csv", 5)
	if err != nil {
		return err
	}
	e := newEmitter("iso639", "data/iso639.csv", digest)
	n := len(rows)
	e.line("const count = %d", n)
	e.line("")
	e.line("// Language handles for codes that have an ISO 639-1 alpha-2 form.")
	e.line("const (")
	for i, r := range rows {
		if r[2] != "" {
			e.line("\t%s Language = %d // %s", strings.ToUpper(r[2]), i+1, r[4])
		}
	}
	e.line(")")
	e.line("")
	b3, t3, a2, en, pt := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	keys := make([]uint32, n)
	search := make([][]string, n)
	for i, r := range rows {
		b3[i], en[i], pt[i] = r[0], r[3], r[4]
		t3[i] = r[1]
		if t3[i] == "" {
			t3[i] = r[0]
		}
		a2[i] = r[2]
		if a2[i] == "" {
			a2[i] = "  "
		}
		keys[i] = mustAlpha3(r[0])
		search[i] = []string{r[0], r[1], r[2], r[3], r[4]}
	}
	e.fixed("alpha3B", b3)
	e.fixed("alpha3T", t3)
	e.fixed("alpha2Codes", a2)
	e.blob("nameEN", en)
	e.blob("namePT", pt)
	e.rank("b3Rank", 26*26*26, keys)
	e.secondaryIndex("t3", 26*26*26, func(rec int) (uint32, bool) {
		if rows[rec][1] == "" {
			return 0, false
		}
		return mustAlpha3(rows[rec][1]), true
	}, n)
	e.secondaryIndex("a2", 26*26, func(rec int) (uint32, bool) {
		if rows[rec][2] == "" {
			return 0, false
		}
		return mustAlpha2(rows[rec][2]), true
	}, n)
	e.tokens(search)
	return e.write()
}

func genISO18245() error {
	rows, digest, err := readExtract("data/mcc.csv", 2)
	if err != nil {
		return err
	}
	ptRows, _, err := readExtract("data/mcc-pt.csv", 2)
	if err != nil {
		return err
	}
	pt := map[string]string{}
	for _, r := range ptRows {
		pt[r[0]] = r[1]
	}
	e := newEmitter("iso18245", "data/mcc.csv + data/mcc-pt.csv", digest)
	n := len(rows)
	codes, en, ptNames := make([]int, n), make([]string, n), make([]string, n)
	keys := make([]uint32, n)
	search := make([][]string, n)
	for i, r := range rows {
		codes[i] = atoi(r[0])
		en[i] = r[1]
		ptNames[i] = pt[r[0]]
		delete(pt, r[0])
		keys[i] = uint32(codes[i])
		search[i] = []string{r[0], r[1], ptNames[i]}
	}
	if len(pt) > 0 {
		return fmt.Errorf("iso18245: tradução para código inexistente: %v", pt)
	}
	e.line("const count = %d", n)
	e.line("")
	e.ints("codes", "uint16", codes)
	e.blob("nameEN", en)
	e.blob("namePT", ptNames)
	e.rank("codeRank", 10000, keys)
	e.tokens(search)
	return e.write()
}
