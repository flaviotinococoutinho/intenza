//intenza:paradigm automata

// Package mpm reads and writes EMV QR Code payloads in Merchant-Presented Mode
// (EMVCo QRCPS-MPM): a flat sequence of ID-length-value records, two decimal
// digits each for ID and length, closed by a CRC-16/CCITT-FALSE in field 63.
//
// Parsing is a two-state automaton over the string (read header, read value)
// that slices the input: fields share its memory.
package mpm

import (
	"errors"
	"strconv"

	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

var (
	ErrSyntax = errors.New("mpm: malformed ID-length-value record")
	ErrCRC    = errors.New("mpm: CRC mismatch")
	ErrValue  = errors.New("mpm: value longer than 99 bytes")
)

// Field is one ID-length-value record.
type Field struct {
	ID    int
	Value string
}

// Payload is an ordered list of fields.
type Payload []Field

// crcTable is CRC-16/CCITT-FALSE (poly 0x1021, init 0xFFFF): one lookup per byte.
var crcTable = func() (t [256]uint16) {
	for i := range t {
		c := uint16(i) << 8
		for range 8 {
			if c&0x8000 != 0 {
				c = c<<1 ^ 0x1021
			} else {
				c <<= 1
			}
		}
		t[i] = c
	}
	return t
}()

// CRC16 computes CRC-16/CCITT-FALSE of s.
func CRC16(s string) uint16 {
	crc := uint16(0xFFFF)
	for i := range len(s) {
		crc = crc<<8 ^ crcTable[byte(crc>>8)^s[i]]
	}
	return crc
}

func digits2(s string) (int, bool) {
	if len(s) < 2 || s[0] < '0' || s[0] > '9' || s[1] < '0' || s[1] > '9' {
		return 0, false
	}
	return int(s[0]-'0')*10 + int(s[1]-'0'), true
}

// Split parses ID-length-value records without checking any CRC (use it for
// nested templates such as field 26 or 62).
func Split(s string) (Payload, error) {
	var p Payload
	for len(s) > 0 {
		id, ok1 := digits2(s)
		n, ok2 := digits2(s[min(2, len(s)):])
		if !ok1 || !ok2 || len(s) < 4+n {
			return nil, ErrSyntax
		}
		p = append(p, Field{ID: id, Value: s[4 : 4+n]})
		s = s[4+n:]
	}
	return p, nil
}

// Parse reads a complete payload: field 00 first, field 63 last with a CRC
// over everything up to and including "6304".
func Parse(s string) (Payload, error) {
	p, err := Split(s)
	if err != nil {
		return nil, err
	}
	if len(p) < 2 || p[0].ID != 0 || p[len(p)-1].ID != 63 || len(p[len(p)-1].Value) != 4 {
		return nil, ErrSyntax
	}
	want, err := strconv.ParseUint(p[len(p)-1].Value, 16, 16)
	if err != nil {
		return nil, ErrSyntax
	}
	if CRC16(s[:len(s)-4]) != uint16(want) {
		return nil, ErrCRC
	}
	return p[:len(p)-1], nil
}

// Get returns the value of the first field with the ID.
func (p Payload) Get(id int) (string, bool) {
	for _, f := range p {
		if f.ID == id {
			return f.Value, true
		}
	}
	return "", false
}

// Sub parses the nested template stored in field id.
func (p Payload) Sub(id int) (Payload, bool) {
	v, ok := p.Get(id)
	if !ok {
		return nil, false
	}
	sub, err := Split(v)
	return sub, err == nil
}

// Encode writes the records (no CRC).
func (p Payload) Encode() (string, error) {
	b := make([]byte, 0, 128)
	for _, f := range p {
		if f.ID < 0 || f.ID > 99 {
			return "", ErrSyntax
		}
		if len(f.Value) > 99 {
			return "", ErrValue
		}
		b = append(b, byte('0'+f.ID/10), byte('0'+f.ID%10), byte('0'+len(f.Value)/10), byte('0'+len(f.Value)%10))
		b = append(b, f.Value...)
	}
	return string(b), nil
}

// Seal encodes the payload and appends field 63 with its CRC.
func (p Payload) Seal() (string, error) {
	body, err := p.Encode()
	if err != nil {
		return "", err
	}
	body += "6304"
	const hexUpper = "0123456789ABCDEF"
	crc := CRC16(body)
	return body + string([]byte{hexUpper[crc>>12], hexUpper[crc>>8&0xF], hexUpper[crc>>4&0xF], hexUpper[crc&0xF]}), nil
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:           "emvco-qrcps-mpm",
		Code:         "EMVCo QRCPS-MPM",
		Title:        "EMV QR Code Specification for Payment Systems — Merchant-Presented Mode",
		Authority:    meta.EMVCo,
		Kind:         meta.Grammar,
		Edition:      "QRCPS-MPM v1.1",
		Package:      "emvco/mpm",
		Structure:    "autômato de dois estados que fatia a entrada; CRC-16/CCITT-FALSE por tabela de 256 entradas",
		Verification: "valor de checagem 0x29B1 para \"123456789\"; payload de exemplo do BCB com CRC 1D3D; ida e volta por fuzz",
		Sources: []meta.Source{
			{
				Name: "EMVCo — QR Code Specification for Payment Systems (MPM)", URL: "https://www.emvco.com/specifications/",
				Retrieved: "2026-10-14", License: "especificação pública com registro", Grounding: meta.Algorithmic,
			},
		},
	}
}
