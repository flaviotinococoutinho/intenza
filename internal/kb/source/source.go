//intenza:paradigm imperative-shell

// Package source reads the world for the knowledge base: the YAML under
// knowledge/, the Go code of every module and the chaos experiments with
// their recorded results.
package source

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/flaviotinococoutinho/intenza/internal/kb"
)

const paradigmDirective = "//intenza:paradigm "

// LoadBase decodes knowledge/{ontology,claims,risks}.yaml rejecting unknown keys.
func LoadBase(root string) (kb.Base, error) {
	var base kb.Base
	var onto struct {
		Concepts []kb.Concept `yaml:"concepts"`
	}
	var claims struct {
		Claims []kb.Claim `yaml:"claims"`
	}
	var risks struct {
		Risks []kb.Risk `yaml:"risks"`
	}
	for name, dst := range map[string]any{"ontology.yaml": &onto, "claims.yaml": &claims, "risks.yaml": &risks} {
		if err := decodeStrict(filepath.Join(root, "knowledge", name), dst); err != nil {
			return base, err
		}
	}
	base.Concepts, base.Claims, base.Risks = onto.Concepts, claims.Claims, risks.Risks
	return base, nil
}

func decodeStrict(file string, dst any) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	return nil
}

// ScanCode indexes top-level symbols, tests and per-package paradigm facts of
// every Go file below root.
func ScanCode(root string) (symbols, tests map[string]bool, pkgs []kb.Package, err error) {
	symbols, tests = map[string]bool{}, map[string]bool{}
	byDir := map[string]*kb.Package{}
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, filepath.Dir(p))
		if rerr != nil {
			return rerr
		}
		dir := filepath.ToSlash(rel)
		file, perr := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.SkipObjectResolution)
		if perr != nil {
			return perr
		}
		isTest := strings.HasSuffix(name, "_test.go")
		indexDecls(dir, file, isTest, symbols, tests)
		if isTest {
			return nil
		}
		pkg := byDir[dir]
		if pkg == nil {
			pkg = &kb.Package{Dir: dir, Name: file.Name.Name}
			byDir[dir] = pkg
		}
		return collectPackageFacts(fset, root, pkg, file)
	})
	if err != nil {
		return nil, nil, nil, err
	}
	for _, pkg := range byDir {
		slices.Sort(pkg.Imports)
		pkg.Imports = slices.Compact(pkg.Imports)
		pkgs = append(pkgs, *pkg)
	}
	slices.SortFunc(pkgs, func(a, b kb.Package) int { return strings.Compare(a.Dir, b.Dir) })
	return symbols, tests, pkgs, nil
}

func indexDecls(dir string, file *ast.File, isTest bool, symbols, tests map[string]bool) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil {
				if t := recvType(d.Recv); t != "" {
					symbols[dir+"."+t+"."+name] = true
				}
				continue
			}
			if isTest {
				for _, pre := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
					if strings.HasPrefix(name, pre) {
						tests[dir+"."+name] = true
					}
				}
				continue
			}
			symbols[dir+"."+name] = true
		case *ast.GenDecl:
			if isTest {
				continue
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					symbols[dir+"."+s.Name.Name] = true
				case *ast.ValueSpec:
					for _, n := range s.Names {
						symbols[dir+"."+n.Name] = true
					}
				}
			}
		}
	}
}

func recvType(fl *ast.FieldList) string {
	if len(fl.List) == 0 {
		return ""
	}
	expr := fl.List[0].Type
	for {
		switch t := expr.(type) {
		case *ast.StarExpr:
			expr = t.X
		case *ast.IndexExpr:
			expr = t.X
		case *ast.IndexListExpr:
			expr = t.X
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

func collectPackageFacts(fset *token.FileSet, root string, pkg *kb.Package, file *ast.File) error {
	for _, cg := range file.Comments {
		if cg.Pos() > file.Package {
			break
		}
		for _, c := range cg.List {
			if v, ok := strings.CutPrefix(c.Text, paradigmDirective); ok {
				p := kb.Paradigm(strings.TrimSpace(v))
				if pkg.Paradigm != "" && pkg.Paradigm != p {
					return fmt.Errorf("%s: paradigmas conflitantes %q e %q", pkg.Dir, pkg.Paradigm, p)
				}
				pkg.Paradigm = p
			}
		}
	}
	local := map[string]string{}
	for _, imp := range file.Imports {
		ipath, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return err
		}
		pkg.Imports = append(pkg.Imports, ipath)
		name := path.Base(ipath)
		if strings.HasPrefix(name, "v") && strings.Contains(ipath, "/") {
			if _, err := strconv.Atoi(name[1:]); err == nil {
				name = path.Base(path.Dir(ipath))
			}
		}
		if imp.Name != nil {
			name = imp.Name.Name
		}
		local[name] = ipath
	}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if ipath, ok := local[id.Name]; ok {
			pos := fset.Position(sel.Pos())
			rel, _ := filepath.Rel(root, pos.Filename)
			pkg.Selectors = append(pkg.Selectors, kb.Selector{
				Path: ipath, Name: sel.Sel.Name, Pos: fmt.Sprintf("%s:%d", filepath.ToSlash(rel), pos.Line),
			})
		}
		return true
	})
	return nil
}

// LoadExperiments lists experiment ids under chaos/experiments and the last
// recorded result of each under chaos/results.
func LoadExperiments(root string) (map[string]bool, map[string]kb.Result, error) {
	exps, results := map[string]bool{}, map[string]kb.Result{}
	files, err := filepath.Glob(filepath.Join(root, "chaos", "experiments", "*.yaml"))
	if err != nil {
		return nil, nil, err
	}
	for _, f := range files {
		var head struct {
			ID string `yaml:"id"`
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, nil, err
		}
		if err := yaml.Unmarshal(raw, &head); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", f, err)
		}
		if head.ID == "" {
			return nil, nil, fmt.Errorf("%s: experimento sem id", f)
		}
		exps[head.ID] = true
		raw, err = os.ReadFile(filepath.Join(root, "chaos", "results", head.ID+".json"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		var res kb.Result
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, nil, fmt.Errorf("resultado de %s: %w", head.ID, err)
		}
		results[head.ID] = res
	}
	return exps, results, nil
}

// Gather builds the complete Facts for a repository root.
func Gather(root string, standards map[string]bool) (kb.Facts, error) {
	symbols, tests, pkgs, err := ScanCode(root)
	if err != nil {
		return kb.Facts{}, err
	}
	exps, results, err := LoadExperiments(root)
	if err != nil {
		return kb.Facts{}, err
	}
	return kb.Facts{
		Symbols: symbols, Tests: tests, Packages: pkgs,
		Experiments: exps, Results: results, Standards: standards,
	}, nil
}
