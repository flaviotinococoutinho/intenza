package kb

import (
	"fmt"
	"slices"
	"strings"
)

// Render writes the knowledge base as Markdown. Output is a pure function of
// its inputs, so a stale document shows up as a diff in CI.
func Render(b Base, f Facts, r Report) []byte {
	var w strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&w, format, args...) }

	p("<!-- Gerado por `go run ./cmd/intenza-kb render`. Não edite à mão: edite knowledge/*.yaml. -->\n\n")
	p("# Base de conhecimento\n\n")
	p("Três perguntas organizam o Intenza: **o que existe** (ontologia), **como a gente sabe** (epistemologia) e **o que pode dar errado** (riscos). ")
	p("Cada resposta é dado versionado em `knowledge/` e um verificador confere contra o código, os testes e os experimentos de caos.\n\n")

	renderEpistemics(p, b, r)
	renderRisks(p, b, r)
	renderClaims(p, b)
	renderOntology(p, b)
	renderStandards(p, b, f)
	renderParadigms(p, f)
	renderFindings(p, r)
	return []byte(w.String())
}

type printer func(format string, args ...any)

var (
	modeLabels     = map[Mode]string{APriori: "a priori", APosteriori: "a posteriori", Testimonial: "testemunhal", Unjustified: "sem justificativa"}
	statusLabels   = map[RiskStatus]string{Mitigated: "mitigado", Exposed: "exposto"}
	solutionLabels = map[Solution]string{Deterministic: "determinística", Stochastic: "estocástica", Hybrid: "híbrida"}
)

func cell(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	return strings.ReplaceAll(s, "|", `\|`)
}

func renderEpistemics(p printer, b Base, r Report) {
	p("## Estado epistêmico\n\n")
	p("A priori vale para toda entrada no escopo (conhecimento determinístico). A posteriori sobreviveu a uma tentativa de refutação no sistema rodando (conhecimento estocástico). Testemunho é emprestado de uma autoridade. Conjectura é dívida.\n\n")
	p("| base | modo | força | alegações |\n|---|---|---:|---:|\n")
	order := []Basis{ByConstruction, ProofExhaustive, PropertyTested, ExampleTested, Measured, Testimony, Conjecture}
	for _, basis := range order {
		meta := bases[basis]
		p("| %s | %s | %d | %d |\n", meta.label, modeLabels[meta.mode], meta.strength, r.ByBasis[basis])
	}
	total := len(b.Claims)
	p("\nDívida epistêmica: **%d de %d** alegações ainda são conjecturas.\n\n", r.ByBasis[Conjecture], total)
}

func renderRisks(p printer, b Base, r Report) {
	p("## Riscos\n\n")
	p("Cada risco é uma cadeia causa → mecanismo → efeito. A solução é determinística (remove o modo de falha e se justifica a priori), estocástica (reduz probabilidade ou impacto e só se justifica medindo) ou híbrida (piso determinístico e teto medido).\n\n")
	p("### Matriz incerteza × solução\n\n")
	p("| | determinística | estocástica | híbrida |\n|---|---|---|---|\n")
	for _, u := range []Uncertainty{Aleatoric, Epistemic} {
		label := map[Uncertainty]string{Aleatoric: "aleatória", Epistemic: "epistêmica"}[u]
		p("| **%s** |", label)
		for _, s := range []Solution{Deterministic, Stochastic, Hybrid} {
			var ids []string
			for _, rk := range b.Risks {
				if rk.Uncertainty == u && rk.Solution == s {
					ids = append(ids, rk.ID)
				}
			}
			slices.Sort(ids)
			p(" %s |", strings.Join(ids, ", "))
		}
		p("\n")
	}
	mitigated := 0
	for _, st := range r.Risks {
		if st == Mitigated {
			mitigated++
		}
	}
	p("\nMitigados com a evidência certa para a natureza da solução: **%d de %d**.\n\n", mitigated, len(b.Risks))
	p("| risco | S×P | causa → mecanismo → efeito | solução | status | mitigado por | experimentos |\n|---|---:|---|---|---|---|---|\n")
	risks := slices.Clone(b.Risks)
	slices.SortStableFunc(risks, func(a, c Risk) int { return c.Severity*c.Likelihood - a.Severity*a.Likelihood })
	for _, rk := range risks {
		chain := cell(rk.Cause) + " → " + cell(rk.Mechanism) + " → " + cell(rk.Effect)
		p("| **%s**<br>%s | %d | %s | %s | %s | %s | %s |\n",
			rk.ID, cell(rk.Title), rk.Severity*rk.Likelihood, chain, solutionLabels[rk.Solution], statusLabels[r.Risks[rk.ID]],
			strings.Join(rk.MitigatedBy, ", "), strings.Join(rk.Experiments, ", "))
	}
	p("\n")
}

func renderClaims(p printer, b Base) {
	p("## Alegações\n\n")
	p("| alegação | enunciado | tipo | base | evidência |\n|---|---|---|---|---|\n")
	claims := slices.Clone(b.Claims)
	slices.SortStableFunc(claims, func(a, c Claim) int {
		if d := c.Basis.Strength() - a.Basis.Strength(); d != 0 {
			return d
		}
		return strings.Compare(a.ID, c.ID)
	})
	for _, cl := range claims {
		var ev []string
		for _, e := range cl.Evidence {
			switch {
			case e.Test != "":
				ev = append(ev, "teste `"+e.Test+"`")
			case e.Bench != "":
				ev = append(ev, "bench `"+e.Bench+"`")
			case e.Symbol != "":
				ev = append(ev, "símbolo `"+e.Symbol+"`")
			case e.Experiment != "":
				ev = append(ev, "experimento `"+e.Experiment+"`")
			case e.Source != "":
				ev = append(ev, cell(e.Source))
			}
		}
		p("| **%s** | %s | %s | %s | %s |\n", cl.ID, cell(cl.Statement), cl.Kind, bases[cl.Basis].label, strings.Join(ev, "<br>"))
	}
	p("\n")
}

var diagramCategories = []Metacategory{Kind, Subkind, Role, Relator, Commitment, SocialObject, HighOrderType, Collective, Quality, Information}

func mermaidID(id string) string { return strings.ReplaceAll(id, "-", "_") }

func renderOntology(p printer, b Base) {
	p("## Ontologia\n\n")
	p("Categorias inspiradas na UFO: o *kind* dá identidade, *role* e *phase* são anti-rígidos, o *relator* (contrato, escrow) existe entre partes, a categoria é um *tipo de alta ordem* que classifica itens e define as qualidades que eles carregam.\n\n")
	p("```mermaid\nflowchart LR\n")
	inDiagram := map[string]bool{}
	for _, k := range b.Concepts {
		if slices.Contains(diagramCategories, k.Category) {
			inDiagram[k.ID] = true
			p("  %s[\"%s<br/><i>%s</i>\"]:::%s\n", mermaidID(k.ID), k.Label, k.Category, strings.ReplaceAll(string(k.Category), "-", ""))
		}
	}
	edge := func(from, label, to string) {
		if inDiagram[from] && inDiagram[to] {
			p("  %s -->|%s| %s\n", mermaidID(from), label, mermaidID(to))
		}
	}
	for _, k := range b.Concepts {
		if k.Specializes != "" {
			edge(k.ID, "especializa", k.Specializes)
		}
		for _, m := range k.Mediates {
			edge(k.ID, "media", m)
		}
		if k.InheresIn != "" {
			edge(k.ID, "inere em", k.InheresIn)
		}
		if k.Classifies != "" {
			edge(k.ID, "classifica", k.Classifies)
		}
		if k.PartOf != "" {
			edge(k.ID, "parte de", k.PartOf)
		}
	}
	p("  classDef kind fill:#1f6f5c,color:#fff\n  classDef subkind fill:#2e8b74,color:#fff\n  classDef role fill:#d98e04,color:#fff\n")
	p("  classDef relator fill:#7a3e9d,color:#fff\n  classDef commitment fill:#b5446e,color:#fff\n  classDef socialobject fill:#c25b3f,color:#fff\n")
	p("  classDef type fill:#2b5d9b,color:#fff\n  classDef collective fill:#4f7cac,color:#fff\n  classDef quality fill:#6b8f23,color:#fff\n  classDef information fill:#5c5c5c,color:#fff\n")
	p("```\n\n")

	order := []Metacategory{Kind, Subkind, Role, Relator, Commitment, SocialObject, HighOrderType, Collective, Quality, Information, Datatype, Phase, Event}
	for _, cat := range order {
		var rows []Concept
		for _, k := range b.Concepts {
			if k.Category == cat {
				rows = append(rows, k)
			}
		}
		if len(rows) == 0 {
			continue
		}
		p("### %s — %s\n\n", cat, metacategories[cat])
		p("| conceito | definição | relações | normas | realizado por |\n|---|---|---|---|---|\n")
		for _, k := range rows {
			p("| **%s**<br>`%s` | %s | %s | %s | %s |\n", k.Label, k.ID, cell(k.Definition), relations(k),
				strings.Join(k.GroundedIn, ", "), codeList(k.RealizedBy))
		}
		p("\n")
	}
}

func relations(k Concept) string {
	var rel []string
	add := func(name string, vals ...string) {
		var nonEmpty []string
		for _, v := range vals {
			if v != "" {
				nonEmpty = append(nonEmpty, v)
			}
		}
		if len(nonEmpty) > 0 {
			rel = append(rel, name+" "+strings.Join(nonEmpty, ", "))
		}
	}
	add("especializa", k.Specializes)
	add("fase de", k.PhaseOf)
	add("media", k.Mediates...)
	add("fundado por", k.FoundedBy)
	add("inere em", k.InheresIn)
	add("classifica", k.Classifies)
	add("participantes", k.Participants...)
	add("cria", k.Creates...)
	add("parte de", k.PartOf)
	return strings.Join(rel, "<br>")
}

func codeList(syms []string) string {
	out := make([]string, len(syms))
	for i, s := range syms {
		out[i] = "`" + s + "`"
	}
	return strings.Join(out, "<br>")
}

func renderStandards(p printer, b Base, f Facts) {
	if len(f.Standards) == 0 {
		return
	}
	p("## Normas\n\n")
	p("Cadeia taxonômica: autoridade → norma → edição → tipo de artefato → pacote → conceitos que se apoiam nela. ")
	p("A fundamentação mostrada é a do elo mais fraco entre as fontes: primária, secundária, reconstruída ou algorítmica.\n\n")
	grounds := map[string][]string{}
	for _, k := range b.Concepts {
		for _, id := range k.GroundedIn {
			grounds[id] = append(grounds[id], k.ID)
		}
	}
	p("| autoridade | norma | tipo | edição | fundamentação | estrutura | pacote | ancora |\n|---|---|---|---|---|---|---|---|\n")
	stds := slices.Clone(f.Standards)
	slices.SortFunc(stds, func(a, c Standard) int {
		if d := strings.Compare(a.Authority, c.Authority); d != 0 {
			return d
		}
		return strings.Compare(a.Code, c.Code)
	})
	for _, s := range stds {
		p("| %s | **%s**<br>%s | %s | %s | %s | %s | `standards/%s` | %s |\n", s.Authority, s.Code, cell(s.Title), s.Kind,
			cell(s.Edition), s.Grounding, cell(s.Structure), s.Package, strings.Join(grounds[s.ID], ", "))
	}
	p("\n")
}

func renderParadigms(p printer, f Facts) {
	p("## Paradigmas\n\n")
	p("Cada pacote declara o paradigma que o governa (`//intenza:paradigm`). Os puros (núcleo funcional, autômatos, orientado a dados, probabilístico) não fazem I/O, não leem o relógio e não sorteiam sem uma fonte injetada; o verificador confere imports e referências.\n\n")
	p("| pacote | paradigma | puro |\n|---|---|---|\n")
	pkgs := slices.Clone(f.Packages)
	slices.SortFunc(pkgs, func(a, c Package) int { return strings.Compare(a.Dir, c.Dir) })
	for _, pk := range pkgs {
		pure := "não"
		if pk.Paradigm.Pure() {
			pure = "sim"
		}
		label := paradigms[pk.Paradigm].label
		if label == "" {
			label = string(pk.Paradigm)
		}
		p("| `%s` | %s | %s |\n", pk.Dir, label, pure)
	}
	p("\n")
}

func renderFindings(p printer, r Report) {
	if len(r.Findings) == 0 {
		return
	}
	p("## Achados do verificador\n\n")
	for _, f := range r.Findings {
		p("- `%s` %s: %s\n", f.Severity, f.Subject, cell(f.Message))
	}
	p("\n")
}
