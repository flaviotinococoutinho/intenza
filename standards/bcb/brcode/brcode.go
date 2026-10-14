//intenza:paradigm functional-core

// Package brcode builds and reads the Pix BR Code, the "copia e cola" payload
// of the Banco Central do Brasil. It is an EMV MPM profile, and the imports
// spell out the chain of norms it rests on: the merchant category is an
// ISO 18245 MCC, the currency is ISO 4217 numeric 986, the country is ISO
// 3166-1 alpha-2 "BR", and the framing and CRC come from EMVCo.
package brcode

import (
	"errors"
	"strconv"

	"github.com/flaviotinococoutinho/intenza/standards/emvco/mpm"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso18245"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso3166"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso4217"
	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

const gui = "br.gov.bcb.pix"

var (
	ErrNotPix   = errors.New("brcode: not a Pix payload")
	ErrField    = errors.New("brcode: field out of bounds")
	ErrCurrency = errors.New("brcode: Pix is settled in BRL")
)

// Pix holds what a payer's app needs to pay.
type Pix struct {
	Key          string       // Pix key (static code) — exclusive with Location
	Location     string       // payload URL (dynamic code, field 26-25)
	Description  string       // free text shown to the payer (26-02)
	MCC          iso18245.MCC // merchant category (52); 0 when not informed
	AmountCents  int64        // 0 lets the payer type the amount
	MerchantName string       // up to 25 characters (59)
	MerchantCity string       // up to 15 characters (60)
	TxID         string       // up to 25 characters (62-05); "***" when absent
	Dynamic      bool         // point of initiation 12 instead of 11
}

func amount(cents int64) string {
	s := strconv.FormatInt(cents, 10)
	for len(s) < 3 {
		s = "0" + s
	}
	return s[:len(s)-2] + "." + s[len(s)-2:]
}

// Encode produces the payload with its CRC.
func Encode(p Pix) (string, error) {
	if (p.Key == "") == (p.Location == "") || p.MerchantName == "" || len(p.MerchantName) > 25 ||
		p.MerchantCity == "" || len(p.MerchantCity) > 15 || len(p.TxID) > 25 || p.AmountCents < 0 {
		return "", ErrField
	}
	acct := mpm.Payload{{ID: 0, Value: gui}}
	if p.Key != "" {
		acct = append(acct, mpm.Field{ID: 1, Value: p.Key})
	} else {
		acct = append(acct, mpm.Field{ID: 25, Value: p.Location})
	}
	if p.Description != "" {
		acct = append(acct, mpm.Field{ID: 2, Value: p.Description})
	}
	acctValue, err := acct.Encode()
	if err != nil {
		return "", err
	}
	txid := p.TxID
	if txid == "" {
		txid = "***"
	}
	extra, _ := mpm.Payload{{ID: 5, Value: txid}}.Encode()
	initiation := "11"
	if p.Dynamic {
		initiation = "12"
	}
	mcc := "0000"
	if p.MCC != 0 {
		mcc = p.MCC.String()
	}
	fields := mpm.Payload{
		{ID: 0, Value: "01"},
		{ID: 1, Value: initiation},
		{ID: 26, Value: acctValue},
		{ID: 52, Value: mcc},
		{ID: 53, Value: iso4217.BRL.NumericString()},
	}
	if p.AmountCents > 0 {
		fields = append(fields, mpm.Field{ID: 54, Value: amount(p.AmountCents)})
	}
	fields = append(fields,
		mpm.Field{ID: 58, Value: iso3166.BR.Alpha2()},
		mpm.Field{ID: 59, Value: p.MerchantName},
		mpm.Field{ID: 60, Value: p.MerchantCity},
		mpm.Field{ID: 62, Value: extra})
	return fields.Seal()
}

// Decode reads a payload, checking the CRC, the Pix GUI and the currency.
func Decode(s string) (Pix, error) {
	p, err := mpm.Parse(s)
	if err != nil {
		return Pix{}, err
	}
	acct, ok := p.Sub(26)
	if g, _ := acct.Get(0); !ok || g != gui {
		return Pix{}, ErrNotPix
	}
	var out Pix
	out.Key, _ = acct.Get(1)
	out.Location, _ = acct.Get(25)
	out.Description, _ = acct.Get(2)
	if cur, _ := p.Get(53); cur != iso4217.BRL.NumericString() {
		return Pix{}, ErrCurrency
	}
	if m, _ := p.Get(52); m != "0000" {
		out.MCC, _ = iso18245.Parse(m)
	}
	if v, ok := p.Get(54); ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return Pix{}, ErrField
		}
		out.AmountCents = int64(f*100 + 0.5)
	}
	ini, _ := p.Get(1)
	out.Dynamic = ini == "12"
	out.MerchantName, _ = p.Get(59)
	out.MerchantCity, _ = p.Get(60)
	if extra, ok := p.Sub(62); ok {
		out.TxID, _ = extra.Get(5)
	}
	return out, nil
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:           "bcb-brcode",
		Code:         "BCB BR Code (Pix)",
		Title:        "Manual de Padrões para Iniciação do Pix — BR Code",
		Authority:    meta.BCB,
		Kind:         meta.Grammar,
		Edition:      "Manual de Padrões para Iniciação do Pix (versão vigente)",
		Package:      "bcb/brcode",
		Structure:    "perfil sobre emvco/mpm; MCC, moeda e país vêm dos SDKs ISO, sem constantes mágicas",
		Verification: "o payload de exemplo do manual decodifica e reencoda byte a byte; valor, txid e chave fazem ida e volta",
		Sources: []meta.Source{
			{
				Name: "Banco Central do Brasil — Manual de Padrões para Iniciação do Pix", URL: "https://www.bcb.gov.br/estabilidadefinanceira/pix",
				Retrieved: "2026-10-14", License: "dados públicos", Grounding: meta.Algorithmic,
			},
		},
	}
}
