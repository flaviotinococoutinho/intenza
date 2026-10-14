//intenza:paradigm imperative-shell

// Command intenza-kb checks the knowledge base against the code and renders it.
//
//	intenza-kb check  [-root .]
//	intenza-kb render [-root .] [-o docs/knowledge.md]
package main

//go:generate go run . render -root ../.. -o ../../docs/knowledge.md

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flaviotinococoutinho/intenza/internal/kb"
	"github.com/flaviotinococoutinho/intenza/internal/kb/source"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	root := fs.String("root", ".", "raiz do repositório")
	out := fs.String("o", "docs/knowledge.md", "arquivo de saída (render)")
	_ = fs.Parse(os.Args[2:])

	base, facts, report, err := load(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "intenza-kb:", err)
		os.Exit(2)
	}
	switch cmd {
	case "check":
		for _, f := range report.Findings {
			fmt.Println(f)
		}
		fmt.Printf("%d conceitos, %d alegações, %d riscos, %d pacotes: %d erros, %d avisos\n",
			len(base.Concepts), len(base.Claims), len(base.Risks), len(facts.Packages),
			report.Errors(), len(report.Findings)-report.Errors())
	case "render":
		dst := filepath.Clean(*out)
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil { //nolint:gosec // idem
			fmt.Fprintln(os.Stderr, "intenza-kb:", err)
			os.Exit(2)
		}
		// O destino vem de quem roda a ferramenta: escrever ali é o propósito.
		if err := os.WriteFile(dst, kb.Render(base, facts, report), 0o600); err != nil { //nolint:gosec // destino escolhido pelo operador
			fmt.Fprintln(os.Stderr, "intenza-kb:", err)
			os.Exit(2)
		}
		for _, f := range report.Findings {
			if f.Severity == kb.SevError {
				fmt.Fprintln(os.Stderr, f)
			}
		}
	default:
		usage()
	}
	if report.Errors() > 0 {
		os.Exit(1)
	}
}

func load(root string) (kb.Base, kb.Facts, kb.Report, error) {
	base, err := source.LoadBase(root)
	if err != nil {
		return kb.Base{}, kb.Facts{}, kb.Report{}, err
	}
	facts, err := source.Gather(root, standardIDs())
	if err != nil {
		return kb.Base{}, kb.Facts{}, kb.Report{}, err
	}
	return base, facts, kb.Validate(base, facts), nil
}

// standardIDs lists the normative references the ontology may be grounded in.
func standardIDs() map[string]bool { return map[string]bool{} }

func usage() {
	fmt.Fprintln(os.Stderr, "uso: intenza-kb check|render [-root .] [-o arquivo]")
	os.Exit(2)
}
