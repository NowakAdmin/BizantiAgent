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
	EchoTest  bool        `json:"echo_test,omitempty"` // diagnostic only: read back each register right after writing it
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
	Composition string `json:"composition,omitempty"`  // ingredients/composition; sent as both L4 (Article Text Lines — confirmed on hardware to be what factory formats print) and X4 (G Text, in case a format uses that instead)
	EAN         string `json:"ean,omitempty"`          // fixed EAN-13 to print (AS register); printing it may also require the label format's barcode field to be configured for "fixed EAN" rather than the scale's auto-computed weight/price barcode — a DLD/scale-side setting, not something this register alone controls
	LabelNum    string `json:"label_num,omitempty"`    // on-scale label format number (L3 FormatoEtiquetaSerieL); default "01". Only takes effect once GLOBALNY FORMAT ETYKIETY (Global Label Format) is set to 0 on the scale (MENU > Printing Parameters) — otherwise the scale always prints its global format regardless of this field.

	// ShelfLifeDays triggers the L3 register (shelf-life + other article
	// attributes) when non-nil. Left nil, L3 is never sent — see
	// BuildL3Register for why L3 carries real risk beyond shelf-life.
	ShelfLifeDays *int `json:"shelf_life_days,omitempty"`
}

// BuildArticleRegisters renders every register needed to fully program an
// article: the L2 core record plus the X4 free-text pages. X4 is always
// included (even with empty text) so a shorter/removed composition clears any
// stale pages left by a previous, longer push — matching the original DFS
// behavior of syncing this field on every article update.
func BuildArticleRegisters(plu Dibal500PLU) ([][]byte, error) {
	l2, err := BuildL2Register(plu)
	if err != nil {
		return nil, err
	}

	registers := [][]byte{l2}

	if strings.ToUpper(strings.TrimSpace(plu.Mode)) != "B" {
		x4, err := BuildX4Registers(plu.LogicalAddr, plu.Group, plu.Code, plu.Composition)
		if err != nil {
			return nil, err
		}
		registers = append(registers, x4...)

		l4, err := BuildL4Registers(plu.LogicalAddr, plu.Group, plu.Code, plu.Composition)
		if err != nil {
			return nil, err
		}
		registers = append(registers, l4...)

		as, err := BuildASRegister(plu.LogicalAddr, plu.Group, plu.Code, plu.EAN)
		if err != nil {
			return nil, err
		}
		registers = append(registers, as)

		// L3 also carries the barcode-format slot needed for a fixed EAN
		// (see BuildL3Register), so send it whenever either is set.
		if plu.ShelfLifeDays != nil || strings.TrimSpace(plu.EAN) != "" {
			l3, err := BuildL3Register(plu, plu.ShelfLifeDays)
			if err != nil {
				return nil, err
			}
			registers = append(registers, l3)
		}
	}

	return registers, nil
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

// x4ChunkSize is the text payload per X4 page; x4MaxTextBytes is the SERIE_L
// (500-series) cap on total composition/free-text length, both reverse-
// engineered from ComunicacionesBalPC.GenerarX4_EnBytes.
const (
	x4ChunkSize    = 116
	x4MaxTextBytes = 1200
)

// BuildX4Registers renders the "free text" (G Text) pages used for long text
// such as an ingredient/composition list. Reverse-engineered from
// GenerarX4_EnBytes (SERIE_L path — the 500-series, not the Chinese-market
// branch). Byte layout per 130-byte page:
//
//	[0:2]   DireccionLogica — same as the article's L2 register
//	[2:4]   register type "X4"
//	[4:6]   Grupo — same as the article's L2 register
//	[6:12]  article code — zero-padded 6 digits (same article as L2)
//	[12:14] page number — zero-padded 2 digits, 1-indexed
//	[14:130] up to 116 bytes of raw Windows-1250 text
//
// Text is chunked at 116 bytes per page with no word-wrapping (matching the
// original). The page holding the final (possibly empty) chunk is terminated
// with an ESC (0x1B) byte right after the text, then space-padded — so an
// exact multiple of 116 bytes still gets one trailing empty ESC-only page,
// and empty text yields a single ESC-only page. This always produces at
// least one page, which is what lets a shorter update clear stale trailing
// pages from a previous, longer composition.
func BuildX4Registers(logicalAddr, group, code, text string) ([][]byte, error) {
	codeBytes, err := numericField(code, 6)
	if err != nil {
		return nil, fmt.Errorf("kod artykułu: %w", err)
	}

	encoded := cp1250Encode(strings.TrimSpace(text))
	if len(encoded) > x4MaxTextBytes {
		encoded = encoded[:x4MaxTextBytes]
	}

	logicalBytes := pad2Digits(logicalAddr)
	groupBytes := pad2Digits(group)

	var registers [][]byte
	page := 1
	pos := 0

	for {
		if page > 99 {
			return nil, fmt.Errorf("tekst składu zbyt długi (przekroczono 99 stron)")
		}

		reg := make([]byte, Dibal500RegisterLen)
		copy(reg[0:2], logicalBytes)
		reg[2] = 'X'
		reg[3] = '4'
		copy(reg[4:6], groupBytes)
		copy(reg[6:12], codeBytes)
		copy(reg[12:14], []byte(fmt.Sprintf("%02d", page)))

		remaining := len(encoded) - pos
		if remaining >= x4ChunkSize {
			copy(reg[14:14+x4ChunkSize], encoded[pos:pos+x4ChunkSize])
			registers = append(registers, reg)
			pos += x4ChunkSize
			page++
			continue
		}

		copy(reg[14:14+remaining], encoded[pos:pos+remaining])
		reg[14+remaining] = 0x1B // ESC marks end of text
		fillSpaces(reg[14+remaining+1:])
		registers = append(registers, reg)
		break
	}

	return registers, nil
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

func fillZeros(b []byte) {
	for i := range b {
		b[i] = '0'
	}
}

// l4LineWidth is the wire slot width reserved per Article Text Line;
// l4LineChars is the scale keypad's documented per-line character limit
// (49-MH000PL04, step 18: "up to 10 lines of 24 characters"); l4MaxLines is
// the total line count, packed 2 per 130-byte register (5 registers).
const (
	l4LineWidth = 48
	l4LineChars = 24
	l4MaxLines  = 10
)

// BuildL4Registers renders the "Article Text Lines" (Texto1..Texto10) —
// reverse engineered from ComunicacionesBalPC.GenerarL4_EnBytes (SERIE_L
// "modify" path) and confirmed against the scale's own manual (step 18: this
// field explicitly supports "tekst, SKŁADNIKI" — free text or ingredients).
// This is what the factory label formats' composition section actually
// prints from — X4 (G Text) is a different, single free-flowing field that a
// format may or may not use instead.
//
// Always renders all 5 registers (10 slots, blank if unused) so a shorter
// composition clears stale trailing lines left by a previous, longer one —
// same reasoning as X4. Text is split into <=24-character lines (hard cut,
// no word-wrap) into up to 10 slots; anything beyond 240 characters total is
// dropped.
//
// Register layout (2 lines per 130-byte register):
//
//	[0:2]   DireccionLogica; [2:4] "L4"; [4:6] Grupo
//	[6:12]  article code; [12] line-A marker ('0','2','4','6','8')
//	[13:61] line-A text, 48 bytes, space-padded
//	[61:67] article code (repeated); [67] line-B marker ('1','3','5','7','9')
//	[68:116] line-B text, 48 bytes, space-padded
//	[116:130] zero-filled
func BuildL4Registers(logicalAddr, group, code, text string) ([][]byte, error) {
	codeBytes, err := numericField(code, 6)
	if err != nil {
		return nil, fmt.Errorf("kod artykułu: %w", err)
	}

	logicalBytes := pad2Digits(logicalAddr)
	groupBytes := pad2Digits(group)

	runes := []rune(strings.TrimSpace(text))
	lines := make([]string, l4MaxLines)
	for i := 0; i < l4MaxLines && len(runes) > 0; i++ {
		end := l4LineChars
		if end > len(runes) {
			end = len(runes)
		}
		lines[i] = string(runes[:end])
		runes = runes[end:]
	}

	registers := make([][]byte, 0, l4MaxLines/2)
	for reg := 0; reg < l4MaxLines/2; reg++ {
		buf := make([]byte, Dibal500RegisterLen)
		fillZeros(buf)

		copy(buf[0:2], logicalBytes)
		buf[2] = 'L'
		buf[3] = '4'
		copy(buf[4:6], groupBytes)

		copy(buf[6:12], codeBytes)
		buf[12] = byte('0' + 2*reg)
		copy(buf[13:61], textField(lines[2*reg], l4LineWidth))

		copy(buf[61:67], codeBytes)
		buf[67] = byte('0' + 2*reg + 1)
		copy(buf[68:116], textField(lines[2*reg+1], l4LineWidth))

		registers = append(registers, buf)
	}

	return registers, nil
}

// asEANWidth is the fixed EAN field width in the AS register.
const asEANWidth = 13

// BuildASRegister renders the "AS" register — a fixed EAN-13 assigned to an
// article, reverse engineered from ComunicacionesBalPC.GenerarAS_EnBytes
// (single-article path). Simple, self-contained layout:
//
//	[0:2]   DireccionLogica; [2:4] "AS"; [4:6] Grupo
//	[6:12]  article code — zero-padded 6 digits
//	[12:25] EAN value, 13 bytes, space-padded/truncated
//	[25:130] zero-filled
//
// Always sent (even with an empty EAN) so removing a product's EAN clears
// any stale value from a previous push — same reasoning as X4/L4.
//
// Printing the fixed EAN may also require the active label format's barcode
// field to be configured for "article EAN" rather than the scale's built-in
// auto-computed weight/price barcode (KONF. EANC01..EANC10 on the scale, or
// the barcode element's binding in DLD) — this register alone provides the
// value, it doesn't select which barcode mode a format uses.
func BuildASRegister(logicalAddr, group, code, ean string) ([]byte, error) {
	codeBytes, err := numericField(code, 6)
	if err != nil {
		return nil, fmt.Errorf("kod artykułu: %w", err)
	}

	buf := make([]byte, Dibal500RegisterLen)
	fillZeros(buf)

	copy(buf[0:2], pad2Digits(logicalAddr))
	buf[2] = 'A'
	buf[3] = 'S'
	copy(buf[4:6], pad2Digits(group))
	copy(buf[6:12], codeBytes)
	copy(buf[12:12+asEANWidth], textField(ean, asEANWidth))

	return buf, nil
}

// BuildL3Register renders the "extra attributes" L3 register — reverse
// engineered from ComunicacionesBalPC.GenerarL3_EnBytes (SERIE_L path). L3 is
// a full-record register like L2: besides shelf-life days it also carries
// sale mode, fixed/percentage tare, VAT slot, section, and the SERIE_L label
// format selector — sending it replaces ALL of these at once. We only
// maintain the fields Bizanti actually tracks (shelf-life, label format) and
// default everything else to a neutral zero, confirmed safe for tare only
// because this deployment never sets tare on the scale's own keypad.
//
// ponytail: sale mode (byte [12], hardcoded "0" = best-guess weight-based
// default) has no source of truth in Bizanti — Dibal's IdTipo catalog is
// internal to DFS. Verify on a throwaway test article before trusting this
// for real weighed products: confirm the printed price still multiplies by
// weight (not a fixed unit price) after this register is sent. If wrong,
// this needs a real mapping, not a wider default guess.
func BuildL3Register(plu Dibal500PLU, shelfLifeDays *int) ([]byte, error) {
	buf := make([]byte, Dibal500RegisterLen)
	fillZeros(buf)

	copy(buf[0:2], pad2Digits(plu.LogicalAddr))
	buf[2] = 'L'
	buf[3] = '3'
	copy(buf[4:6], pad2Digits(plu.Group))

	code, err := numericField(plu.Code, 6)
	if err != nil {
		return nil, fmt.Errorf("kod artykułu: %w", err)
	}
	copy(buf[6:12], code)

	buf[12] = '0' // sale mode: weight-based default — see doc comment above

	// No shelf life set -> "000000", matching the original's own "not set"
	// fallback (both the date and hours branches write all-zero when empty).
	days := 0
	if shelfLifeDays != nil {
		days = *shelfLifeDays
	}
	expiry, err := intField(days, 6)
	if err != nil {
		return nil, fmt.Errorf("termin ważności: %w", err)
	}
	copy(buf[13:19], expiry)

	// [19:31] extra date + packaging date: unused, left zero.
	// [31:38] fixed + percentage tare: zero (confirmed safe — no manual tare in use).

	labelNum, err := numericField(plu.LabelNum, 2)
	if err != nil {
		return nil, fmt.Errorf("numer formatu etykiety: %w", err)
	}
	copy(buf[38:40], labelNum)

	// Barcode format slot (1-10): "01" enables the article's fixed EAN (AS
	// register) on formats bound to it; "00" leaves barcode printing on
	// whatever the format/scale defaults to (typically an auto-computed
	// weight/price code). Untested which of the 10 slots is "correct" beyond
	// slot 1 — verify on hardware; the scale's own KONF. EANC01..EANC10 menu
	// may need slot 1 configured for "fixed article EAN" mode too.
	if strings.TrimSpace(plu.EAN) != "" {
		copy(buf[40:42], []byte("01"))
	}

	// [42:44] fixed literal: unused, left zero.
	copy(buf[44:48], []byte("0001")) // section: default 1
	// [48:130] VAT slot, smiley, class, associated element, recipe, logo,
	// reserved, price-override flag: all unused, left zero.

	return buf, nil
}
