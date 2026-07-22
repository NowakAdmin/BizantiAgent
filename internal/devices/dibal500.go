package devices

// Dibal 500-series (e.g. W-025S) article programming.
//
// Unlike the K-235 register protocol (dibal.go), the 500-series is spoken by
// Dibal's own native commL.dll. The agent reuses that DLL for transport (via a
// 32-bit bridge process) and only builds the article record here.
//
// The article record is the fixed 130-byte "L2" register, reverse-engineered
// from ComunicacionesBalPC.GenerarL2_EnBytes. Byte layout (0-indexed):
//
//	[0:2]    DireccionLogica (scale logical address) — ASCII digits, default "00"
//	[2:4]    register type "L2"
//	[4:6]    Grupo (group/department) — default "00"
//	[6]      operation: 'A' add, 'B' delete, 'M' modify (upsert)
//	[7:13]   article code — zero-padded 6 digits
//	[13:16]  TeclaDirecta (direct key) — "-01" if none, else "NNN"
//	[16:40]  name line 1 — space-padded 24
//	[40:64]  name line 2 — space-padded 24
//	[64:88]  name line 3 — space-padded 24
//	[88:96]  gross price (PrecioConIVA) — zero-padded 8 digits, in minor units
//	[96:104] offer price — zero-padded 8 digits (0 if none)
//	[104:112] standard/cost price — zero-padded 8 digits
//	[112:115] spaces
//	[115:124] reference — zero-padded 9 digits (0 if none)
//	[124:130] spaces
//
// For a delete ('B') the whole [13:130] region is spaces.

import (
	"fmt"
	"strconv"
	"strings"
)

// Dibal500RegisterLen is the fixed L2 register size the scale expects.
const Dibal500RegisterLen = 130

// Dibal500ProgramPayload is the payload for the "program_dibal_plu_500" command.
type Dibal500ProgramPayload struct {
	ScaleIP   string      `json:"scale_ip"`
	ScalePort int         `json:"scale_port,omitempty"`
	PCIP      string      `json:"pc_ip,omitempty"`
	TimeoutMs int         `json:"timeout_ms,omitempty"`
	Transform bool        `json:"transform,omitempty"` // commL.dll byte transform; false = raw CP1250 (correct for our path)
	PLU       Dibal500PLU `json:"plu"`
}

// Dibal500PLU holds the fields needed to build an L2 article register.
type Dibal500PLU struct {
	Mode        string `json:"mode,omitempty"`         // A / B / M (default M)
	Code        string `json:"code"`                   // up to 6 digits
	DirectKey   string `json:"direct_key,omitempty"`   // up to 3 digits; empty = none
	Name        string `json:"name"`                   // name line 1
	Name2       string `json:"name2,omitempty"`        // name line 2
	Name3       string `json:"name3,omitempty"`        // name line 3
	PriceGrosze int    `json:"price_grosze"`           // gross price, minor units
	OfferGrosze int    `json:"offer_grosze,omitempty"` // offer price, minor units
	StdGrosze   int    `json:"std_grosze,omitempty"`   // standard/cost price
	Reference   int    `json:"reference,omitempty"`    // reference number (9 digits)
	LogicalAddr string `json:"logical_addr,omitempty"` // scale logical address (default "00")
	Group       string `json:"group,omitempty"`        // group/department (default "00")
}

// BuildL2Register renders the 130-byte L2 article register for the scale.
func BuildL2Register(plu Dibal500PLU) ([]byte, error) {
	buf := make([]byte, Dibal500RegisterLen)

	copy(buf[0:2], pad2Digits(plu.LogicalAddr))
	buf[2] = 'L'
	buf[3] = '2'
	copy(buf[4:6], pad2Digits(plu.Group))

	mode := strings.ToUpper(strings.TrimSpace(plu.Mode))
	switch mode {
	case "A":
		buf[6] = 'A'
	case "B":
		buf[6] = 'B'
	default:
		buf[6] = 'M'
	}

	code, err := numericField(plu.Code, 6)
	if err != nil {
		return nil, fmt.Errorf("kod artykułu: %w", err)
	}
	copy(buf[7:13], code)

	if buf[6] == 'B' {
		fillSpaces(buf[13:130])
		return buf, nil
	}

	copy(buf[13:16], directKeyField(plu.DirectKey))
	copy(buf[16:40], textField(plu.Name, 24))
	copy(buf[40:64], textField(plu.Name2, 24))
	copy(buf[64:88], textField(plu.Name3, 24))

	price, err := intField(plu.PriceGrosze, 8)
	if err != nil {
		return nil, fmt.Errorf("cena: %w", err)
	}
	copy(buf[88:96], price)

	offer, err := intField(plu.OfferGrosze, 8)
	if err != nil {
		return nil, fmt.Errorf("cena promocyjna: %w", err)
	}
	copy(buf[96:104], offer)

	std, err := intField(plu.StdGrosze, 8)
	if err != nil {
		return nil, fmt.Errorf("cena standardowa: %w", err)
	}
	copy(buf[104:112], std)

	fillSpaces(buf[112:115])

	ref, err := intField(plu.Reference, 9)
	if err != nil {
		return nil, fmt.Errorf("referencja: %w", err)
	}
	copy(buf[115:124], ref)

	fillSpaces(buf[124:130])

	return buf, nil
}

// pad2Digits normalises a 2-digit ASCII field (logical address / group).
func pad2Digits(s string) []byte {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "00"
	}
	if len(s) > 2 {
		s = s[len(s)-2:]
	}
	for len(s) < 2 {
		s = "0" + s
	}
	return []byte(s)
}

// directKeyField renders the 3-byte TeclaDirecta field. Empty/invalid/out-of-range
// maps to "-01" (no key assigned), matching GenerarL2_EnBytes.
func directKeyField(key string) []byte {
	key = strings.TrimSpace(key)
	n, err := strconv.Atoi(key)
	if key == "" || err != nil || n < 0 || n > 699 {
		return []byte("-01")
	}
	return []byte(fmt.Sprintf("%03d", n%1000))
}

// textField encodes a name to fixed width, space-padded, in raw Windows-1250 —
// the scale's native code page (confirmed on hardware: Ć=0xC6, Č=0xC8 match
// what the scale stores from keyboard entry). Sent with transform=false so
// commL.dll passes the bytes through unchanged.
func textField(s string, width int) []byte {
	out := make([]byte, width)
	fillSpaces(out)
	b := cp1250Encode(strings.TrimSpace(s))
	if len(b) > width {
		b = b[:width]
	}
	copy(out, b)
	return out
}

// cp1250Extra maps the non-ASCII runes common in Polish (and nearby Central
// European) product names to their Windows-1250 byte. ASCII passes through
// unchanged; any other non-ASCII rune becomes '?'.
var cp1250Extra = map[rune]byte{
	'Ą': 0xA5, 'Ć': 0xC6, 'Ę': 0xCA, 'Ł': 0xA3, 'Ń': 0xD1, 'Ó': 0xD3, 'Ś': 0x8C, 'Ż': 0xAF, 'Ź': 0x8F,
	'ą': 0xB9, 'ć': 0xE6, 'ę': 0xEA, 'ł': 0xB3, 'ń': 0xF1, 'ó': 0xF3, 'ś': 0x9C, 'ż': 0xBF, 'ź': 0x9F,
	'Ä': 0xC4, 'Ö': 0xD6, 'Ü': 0xDC, 'ä': 0xE4, 'ö': 0xF6, 'ü': 0xFC, 'ß': 0xDF,
	'°': 0xB0, '§': 0xA7, '€': 0x80,
}

func cp1250Encode(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 0x20 && r < 0x7f:
			out = append(out, byte(r))
		default:
			if b, ok := cp1250Extra[r]; ok {
				out = append(out, b)
			} else {
				out = append(out, '?')
			}
		}
	}
	return out
}

// numericField zero-pads a numeric string to length, erroring if it overflows.
func numericField(s string, length int) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "0"
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil, fmt.Errorf("wartość nienumeryczna %q", s)
	}
	return intField(n, length)
}

// intField zero-pads an int to length, erroring if it does not fit.
func intField(n, length int) ([]byte, error) {
	s := strconv.Itoa(n)
	if len(s) > length {
		return nil, fmt.Errorf("wartość %d nie mieści się w %d cyfrach", n, length)
	}
	for len(s) < length {
		s = "0" + s
	}
	return []byte(s), nil
}

func fillSpaces(b []byte) {
	for i := range b {
		b[i] = ' '
	}
}
