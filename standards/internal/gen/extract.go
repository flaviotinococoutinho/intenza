package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Upstream sources. extract rebuilds the committed CSVs from these files,
// downloaded by the maintainer into one directory (see standards/README.md).
const (
	srcSIX       = "iso4217-list-one.xml" // https://www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml
	srcCLDR      = "cldr-pt.xml"          // https://raw.githubusercontent.com/unicode-org/cldr/main/common/main/pt.xml
	srcCountries = "country-codes.csv"    // https://raw.githubusercontent.com/datasets/country-codes/main/data/country-codes.csv
	srcLanguages = "language-codes-full.csv"
	srcMCC       = "mcc_codes.csv" // https://raw.githubusercontent.com/greggles/mcc-codes/main/mcc_codes.csv
)

type upstream struct {
	dir string
}

func (u upstream) read(name string) ([]byte, string, error) {
	raw, err := os.ReadFile(filepath.Join(u.dir, name))
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

func (u upstream) csv(name string) ([]map[string]string, string, error) {
	raw, digest, err := u.read(name)
	if err != nil {
		return nil, "", err
	}
	rows, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", name, err)
	}
	head := rows[0]
	out := make([]map[string]string, 0, len(rows)-1)
	for _, r := range rows[1:] {
		m := make(map[string]string, len(head))
		for i, h := range head {
			m[strings.TrimPrefix(h, "\ufeff")] = r[i]
		}
		out = append(out, m)
	}
	return out, digest, nil
}

// cldr holds the Portuguese display names from CLDR pt.xml.
type cldr struct {
	territories, languages, currencies map[string]string
	digest                             string
}

func (u upstream) cldr() (cldr, error) {
	raw, digest, err := u.read(srcCLDR)
	if err != nil {
		return cldr{}, err
	}
	var doc struct {
		Names struct {
			Languages []struct {
				Type string `xml:"type,attr"`
				Alt  string `xml:"alt,attr"`
				Text string `xml:",chardata"`
			} `xml:"languages>language"`
			Territories []struct {
				Type string `xml:"type,attr"`
				Alt  string `xml:"alt,attr"`
				Text string `xml:",chardata"`
			} `xml:"territories>territory"`
		} `xml:"localeDisplayNames"`
		Currencies []struct {
			Type  string `xml:"type,attr"`
			Names []struct {
				Count string `xml:"count,attr"`
				Text  string `xml:",chardata"`
			} `xml:"displayName"`
		} `xml:"numbers>currencies>currency"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return cldr{}, fmt.Errorf("cldr: %w", err)
	}
	c := cldr{territories: map[string]string{}, languages: map[string]string{}, currencies: map[string]string{}, digest: digest}
	for _, t := range doc.Names.Territories {
		if t.Alt == "" {
			c.territories[t.Type] = strings.TrimSpace(t.Text)
		}
	}
	for _, l := range doc.Names.Languages {
		if l.Alt == "" {
			c.languages[l.Type] = strings.TrimSpace(l.Text)
		}
	}
	for _, cur := range doc.Currencies {
		for _, n := range cur.Names {
			if n.Count == "" {
				c.currencies[cur.Type] = strings.TrimSpace(n.Text)
			}
		}
	}
	return c, nil
}

func writeExtract(path string, provenance []string, header []string, rows [][]string) error {
	var b bytes.Buffer
	for _, p := range provenance {
		b.WriteString("# " + p + "\n")
	}
	w := csv.NewWriter(&b)
	_ = w.Write(header)
	_ = w.WriteAll(rows)
	if err := w.Error(); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o600)
}

func today() string { return time.Now().UTC().Format("2006-01-02") }

func extractISO4217(u upstream, out string) error {
	raw, digest, err := u.read(srcSIX)
	if err != nil {
		return err
	}
	var doc struct {
		Published string `xml:"Pblshd,attr"`
		Entries   []struct {
			Country string `xml:"CtryNm"`
			Name    struct {
				IsFund string `xml:"IsFund,attr"`
				Text   string `xml:",chardata"`
			} `xml:"CcyNm"`
			Code   string `xml:"Ccy"`
			Number string `xml:"CcyNbr"`
			Minor  string `xml:"CcyMnrUnts"`
		} `xml:"CcyTbl>CcyNtry"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	pt, err := u.cldr()
	if err != nil {
		return err
	}
	type cur struct {
		number, minor, name string
		fund                bool
		entities            int
	}
	byCode := map[string]*cur{}
	for _, e := range doc.Entries {
		if e.Code == "" {
			continue // territories without a universal currency
		}
		c := byCode[e.Code]
		if c == nil {
			minor := e.Minor
			if minor == "N.A." {
				minor = "-1"
			}
			c = &cur{number: e.Number, minor: minor, name: strings.TrimSpace(e.Name.Text)}
			byCode[e.Code] = c
		}
		c.entities++
		c.fund = c.fund || e.Name.IsFund == "true"
	}
	codes := make([]string, 0, len(byCode))
	for k := range byCode {
		codes = append(codes, k)
	}
	slices.Sort(codes)
	rows := make([][]string, 0, len(codes))
	for _, k := range codes {
		c := byCode[k]
		namePT := pt.currencies[k]
		if namePT == "" {
			namePT = c.name
		}
		rows = append(rows, []string{k, c.number, c.minor, strconv.FormatBool(c.fund), strconv.Itoa(c.entities), c.name, namePT})
	}
	return writeExtract(out, []string{
		"ISO 4217 — lista publicada pela SIX Financial Information (agência de manutenção) em " + doc.Published,
		"upstream " + srcSIX + " sha256=" + digest,
		"nomes pt: Unicode CLDR common/main/pt.xml sha256=" + pt.digest + " (Unicode-3.0)",
		"extraído em " + today(),
	}, []string{"alpha", "numeric", "minor", "fund", "entities", "name_en", "name_pt"}, rows)
}

func pad3(s string) string {
	n := atoi(s)
	return fmt.Sprintf("%03d", n)
}

func extractISO3166(u upstream, outCountries, outAreas string) error {
	rows, digest, err := u.csv(srcCountries)
	if err != nil {
		return err
	}
	pt, err := u.cldr()
	if err != nil {
		return err
	}
	type area struct{ parent, name string }
	areas := map[string]area{"001": {"", "World"}}
	var countries [][]string
	for _, r := range rows {
		a2 := r["ISO3166-1-Alpha-2"]
		if a2 == "" {
			continue
		}
		region, sub, inter := r["Region Code"], r["Sub-region Code"], r["Intermediate Region Code"]
		leaf := "001"
		if region != "" {
			region = pad3(region)
			areas[region] = area{"001", r["Region Name"]}
			leaf = region
		}
		if sub != "" {
			sub = pad3(sub)
			areas[sub] = area{region, r["Sub-region Name"]}
			leaf = sub
		}
		if inter != "" {
			inter = pad3(inter)
			areas[inter] = area{sub, r["Intermediate Region Name"]}
			leaf = inter
		}
		namePT := pt.territories[a2]
		if namePT == "" {
			namePT = r["official_name_en"]
		}
		countries = append(countries, []string{a2, r["ISO3166-1-Alpha-3"], pad3(r["ISO3166-1-numeric"]), r["official_name_en"], namePT, leaf})
	}
	slices.SortFunc(countries, func(a, b []string) int { return strings.Compare(a[0], b[0]) })
	provenance := []string{
		"ISO 3166-1 e UN M49 — compilação datasets/country-codes (ODC-PDDL) a partir da ISO e da UNSD",
		"upstream " + srcCountries + " sha256=" + digest,
		"nomes pt: Unicode CLDR common/main/pt.xml sha256=" + pt.digest + " (Unicode-3.0)",
		"extraído em " + today(),
	}
	if err := writeExtract(outCountries, provenance, []string{"alpha2", "alpha3", "numeric", "name_en", "name_pt", "area"}, countries); err != nil {
		return err
	}
	codes := make([]string, 0, len(areas))
	for k := range areas {
		codes = append(codes, k)
	}
	slices.Sort(codes)
	var arows [][]string
	for _, k := range codes {
		a := areas[k]
		namePT := pt.territories[k]
		if namePT == "" {
			namePT = a.name
		}
		arows = append(arows, []string{k, a.parent, a.name, namePT})
	}
	return writeExtract(outAreas, provenance, []string{"code", "parent", "name_en", "name_pt"}, arows)
}

func extractISO639(u upstream, out string) error {
	rows, digest, err := u.csv(srcLanguages)
	if err != nil {
		return err
	}
	pt, err := u.cldr()
	if err != nil {
		return err
	}
	var langs [][]string
	for _, r := range rows {
		b := r["alpha3-b"]
		if len(b) != 3 {
			continue // reserved ranges such as qaa-qtz
		}
		a2, t := r["alpha2"], r["alpha3-t"]
		namePT := pt.languages[a2]
		if namePT == "" {
			namePT = pt.languages[b]
		}
		if namePT == "" {
			namePT = r["English"]
		}
		langs = append(langs, []string{b, t, a2, r["English"], namePT})
	}
	slices.SortFunc(langs, func(a, b []string) int { return strings.Compare(a[0], b[0]) })
	return writeExtract(out, []string{
		"ISO 639-1/639-2 — compilação datasets/language-codes (ODC-PDDL) a partir da Library of Congress (RA da ISO 639-2)",
		"upstream " + srcLanguages + " sha256=" + digest,
		"nomes pt: Unicode CLDR common/main/pt.xml sha256=" + pt.digest + " (Unicode-3.0)",
		"extraído em " + today(),
	}, []string{"alpha3b", "alpha3t", "alpha2", "name_en", "name_pt"}, langs)
}

func extractISO18245(u upstream, out string) error {
	rows, digest, err := u.csv(srcMCC)
	if err != nil {
		return err
	}
	var mccs [][]string
	for _, r := range rows {
		code := atoi(r["mcc"])
		mccs = append(mccs, []string{fmt.Sprintf("%04d", code), strings.TrimSpace(r["edited_description"])})
	}
	slices.SortFunc(mccs, func(a, b []string) int { return strings.Compare(a[0], b[0]) })
	mccs = slices.CompactFunc(mccs, func(a, b []string) bool { return a[0] == b[0] })
	return writeExtract(out, []string{
		"ISO 18245 (MCC) — compilação greggles/mcc-codes (Unlicense) a partir de publicações do IRS e do USDA",
		"a norma ISO 18245 é paga: os códigos e descrições vêm de fontes públicas secundárias",
		"upstream " + srcMCC + " sha256=" + digest,
		"extraído em " + today(),
	}, []string{"code", "name_en"}, mccs)
}
