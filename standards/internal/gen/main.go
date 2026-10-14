//intenza:paradigm generative

// Command gen turns committed extracts of normative datasets into Go tables.
//
//	go run ./internal/gen <dataset>                       # from a package dir, via go:generate
//	go run ./internal/gen extract <dataset> <upstream-dir> # rebuild data/*.csv from upstream files
//
// Generation is pure and deterministic: the same extract always yields the same
// file, which is what make generate-check relies on.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

var generators = map[string]func() error{
	"iso4217":  genISO4217,
	"iso3166":  genISO3166,
	"m49":      genM49,
	"iso639":   genISO639,
	"iso18245": genISO18245,
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: gen <dataset> | gen extract <dataset> <dir>")
	}
	if args[0] != "extract" {
		g, ok := generators[args[0]]
		if !ok {
			return fmt.Errorf("dataset desconhecido %q", args[0])
		}
		return g()
	}
	if len(args) != 3 {
		return fmt.Errorf("uso: gen extract <dataset> <dir>")
	}
	u := upstream{dir: args[2]}
	root := "." // standards module root
	switch args[1] {
	case "iso4217":
		return extractISO4217(u, filepath.Join(root, "iso/iso4217/data/iso4217.csv"))
	case "iso3166":
		return extractISO3166(u, filepath.Join(root, "iso/iso3166/data/iso3166.csv"), filepath.Join(root, "un/m49/data/m49.csv"))
	case "iso639":
		return extractISO639(u, filepath.Join(root, "iso/iso639/data/iso639.csv"))
	case "iso18245":
		return extractISO18245(u, filepath.Join(root, "iso/iso18245/data/mcc.csv"))
	}
	return fmt.Errorf("extração desconhecida %q", args[1])
}
