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

// Dibal500FormatProgramPayload is the payload for the "program_dibal_format"
// command — programs a label FORMAT (physical layout), as opposed to
// program_dibal_plu_500 which programs article DATA.
type Dibal500FormatProgramPayload struct {
	ScaleIP     string                `json:"scale_ip"`
	ScalePort   int                   `json:"scale_port,omitempty"`
	PCIP        string                `json:"pc_ip,omitempty"`
	TimeoutMs   int                   `json:"timeout_ms,omitempty"`
	Transform   bool                  `json:"transform,omitempty"`
	LogicalAddr string                `json:"logical_addr,omitempty"`
	Group       string                `json:"group,omitempty"`
	FormatNum   string                `json:"format_num"`
	Width       int                   `json:"width"`
	Height      int                   `json:"height"`
	Fields      []Dibal500FormatField `json:"fields"`
}

// Dibal500ReadFormatPayload is the payload for the "read_dibal_format"
// command — asks the scale for a label FORMAT's registers back (the
// physical layout), the inverse of program_dibal_format. FormatNum is an
// int (not the zero-padded string used for writes) since 1-20 are valid to
// read (the scale's built-in factory formats) but never to write.
type Dibal500ReadFormatPayload struct {
	ScaleIP   string `json:"scale_ip"`
	ScalePort int    `json:"scale_port,omitempty"`
	PCIP      string `json:"pc_ip,omitempty"`
	PCPort    int    `json:"pc_port,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
	FormatNum int    `json:"format_num"`
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
	BarcodeSlot string `json:"barcode_slot,omitempty"` // L3 IdCodBarras override (1-10, KONF. EANC01..EANC10); defaults to "01" when EAN is set — untested which slot means "use article's fixed EAN" vs the scale's built-in auto-computed one
	LabelNum    string `json:"label_num,omitempty"`    // on-scale label format number (L3 FormatoEtiquetaSerieL); default "01". Only takes effect once GLOBALNY FORMAT ETYKIETY (Global Label Format) is set to 0 on the scale (MENU > Printing Parameters) — otherwise the scale always prints its global format regardless of this field.

	// ShelfLifeDays triggers the L3 register (shelf-life + other article
	// attributes) when non-nil. Left nil, L3 is never sent — see
	// BuildL3Register for why L3 carries real risk beyond shelf-life.
	ShelfLifeDays *int   `json:"shelf_life_days,omitempty"`
	FrozenDate    string `json:"frozen_date,omitempty"` // DDMMYY (6 digits); also triggers L3 when set

	// CongelacionDate (DDMMYY): H3 register's FechaCongelacion field —
	// distinct from FrozenDate, which is L3's FechaEnvasado ("packaging").
	// Exploratory field, not yet wired into BuildArticleRegisters: a real
	// backup export from the production scale showed FechaCongelacion sits
	// untouched (all zeros) on an article whose FechaEnvasado (FrozenDate)
	// we do set — "Congelación" literally means "freezing" in Spanish, so
	// this is the prime suspect for what actually drives the scale's
	// "przechowuj zamrożone" (store frozen) printed message. Use
	// BuildH3Register directly (e.g. via the debugtools dibal500_send_h3
	// command) to test this on hardware before wiring it into the normal
	// push path.
	CongelacionDate string `json:"congelacion_date,omitempty"`
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

		// L3 also carries the barcode-format slot (for a fixed EAN) and the
		// freezing date, so send it whenever any of the three is set.
		if plu.ShelfLifeDays != nil || strings.TrimSpace(plu.EAN) != "" || strings.TrimSpace(plu.FrozenDate) != "" {
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
// no word-wrap) starting at slot 1 (Tek2) — slot 0 (Tek1) sits directly
// under the product name and is reserved, left blank here. That leaves 9
// usable slots; anything beyond 216 characters (9×24) total is dropped.
//
// Register layout (2 lines per 130-byte register):
//
//	[0:2]   DireccionLogica; [2:4] "L4"; [4:6] Grupo
//	[6:12]  article code; [12] line-A marker ('0','2','4','6','8')
//	[13:61] line-A text, 48 bytes — see l4EncodeLine (2 bytes per character)
//	[61:67] article code (repeated); [67] line-B marker ('1','3','5','7','9')
//	[68:116] line-B text, 48 bytes — see l4EncodeLine
//	[116:130] zero-filled
func BuildL4Registers(logicalAddr, group, code, text string) ([][]byte, error) {
	codeBytes, err := numericField(code, 6)
	if err != nil {
		return nil, fmt.Errorf("kod artykułu: %w", err)
	}

	logicalBytes := pad2Digits(logicalAddr)
	groupBytes := pad2Digits(group)

	// Composition starts at Tek2 (slot 1), not Tek1 (slot 0) — slot 0 sits
	// directly under the product name and is reserved, not part of the
	// composition text.
	runes := []rune(strings.TrimSpace(text))
	lines := make([]string, l4MaxLines)
	for i := 1; i < l4MaxLines && len(runes) > 0; i++ {
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
		copy(buf[13:61], l4EncodeLine(lines[2*reg]))

		copy(buf[61:67], codeBytes)
		buf[67] = byte('0' + 2*reg + 1)
		copy(buf[68:116], l4EncodeLine(lines[2*reg+1]))

		registers = append(registers, buf)
	}

	return registers, nil
}

// l4EncodeLine renders one Article Text Line into its 48-byte wire slot.
// Confirmed on hardware: the field expects 2 bytes per visible character —
// a filler byte followed by the real character — not 1 byte per character
// like every other text field (L2 name, X4, AS). This is presumably a
// wide-char storage convention shared with the keypad's own character-by-
// character line editor. It also explains the 48-byte slot holding only 24
// visible characters, exactly matching the scale's documented per-line limit
// (49-MH000PL04, PLU creation step 18: "10 lines of 24 characters").
//
// The filler byte's own value appears not to matter for display (space
// works cleanly); using anything else (e.g. the NUL byte a real 2-byte
// charset would use) is unverified and risks being read as a string
// terminator somewhere in the pipeline, so space is deliberately kept.
func l4EncodeLine(text string) []byte {
	out := make([]byte, l4LineWidth)
	fillSpaces(out)

	encoded := cp1250Encode(text)
	if len(encoded) > l4LineChars {
		encoded = encoded[:l4LineChars]
	}

	for i, c := range encoded {
		out[i*2] = ' '
		out[i*2+1] = c
	}

	return out
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

	// [19:25] extra date: unused, left zero.

	// [25:31] packaging/freezing date (FechaEnvasado in the original DFS
	// schema — matches "data zamrożenia" on the scale). Written as DDMMYY
	// (6 ASCII digits, no separators) when provided. Left as spaces (not the
	// register's usual zero-fill) when absent: hardware testing showed
	// "000000" here still reads as a valid date to the scale, so it kept
	// printing "przechowuj zamrożone" (store frozen) even with no date set —
	// blank/spaces is what actually turns that message off.
	fillSpaces(buf[25:31])
	if fd := strings.TrimSpace(plu.FrozenDate); fd != "" {
		frozen, err := numericField(fd, 6)
		if err != nil {
			return nil, fmt.Errorf("data zamrożenia: %w", err)
		}
		copy(buf[25:31], frozen)
	}

	// [31:38] fixed + percentage tare: zero (confirmed safe — no manual tare in use).

	labelNum, err := numericField(plu.LabelNum, 2)
	if err != nil {
		return nil, fmt.Errorf("numer formatu etykiety: %w", err)
	}
	copy(buf[38:40], labelNum)

	// Barcode format slot (1-10, KONF. EANC01..EANC10 on the scale): defaults
	// to "01" when EAN is set, overridable via plu.BarcodeSlot for testing
	// which slot actually means "use the article's fixed EAN" — hardware
	// testing showed slot 01 still prints the scale's auto-computed
	// weight/price code, so slot 1 may be hardwired to "auto" and a
	// different slot (or reconfiguring slot 1 on the scale itself) may be
	// needed. "00" leaves barcode printing on whatever default applies.
	if slot := strings.TrimSpace(plu.BarcodeSlot); slot != "" {
		slotBytes, err := numericField(slot, 2)
		if err != nil {
			return nil, fmt.Errorf("slot kodu kreskowego: %w", err)
		}
		copy(buf[40:42], slotBytes)
	} else if strings.TrimSpace(plu.EAN) != "" {
		copy(buf[40:42], []byte("01"))
	}

	// [42:44] fixed literal: unused, left zero.
	copy(buf[44:48], []byte("0001")) // section: default 1
	// [48:130] VAT slot, smiley, class, associated element, recipe, logo,
	// reserved, price-override flag: all unused, left zero.

	return buf, nil
}

// BuildH3Register renders the 130-byte H3 register — the 500-series'
// extended article record. Exploratory/testing only, not yet wired into
// BuildArticleRegisters: a real backup export from the production scale
// (LBS "Articles Only" backup) confirmed H3 shares the same underlying
// article data as L2/L3 (its Caducidad and FechaEnvasado bytes matched
// exactly what BuildL3Register had already sent for that article), but adds
// a field L3 doesn't have: FechaCongelacion ("freezing date" — distinct
// from FechaEnvasado, "packaging date", which is what plu.FrozenDate/L3
// already sets). FechaCongelacion sat untouched (all zeros) on that real
// article despite its FrozenDate being set, making it the prime suspect for
// what actually drives the scale's "przechowuj zamrożone" (store frozen)
// printed message — use this via the debugtools dibal500_send_h3 command to
// test the hypothesis on hardware before wiring it into the normal push
// path.
//
// Byte layout (0-indexed), reverse-engineered from
// ComunicacionesBalPC.GenerarH3_EnBytes — fields shared with L3 mirror its
// values so sending this doesn't clobber already-correct article data:
//
//	[0:2]     logical address, default "00"
//	[2:4]     register type "H3"
//	[4:6]     group, default "00"
//	[6:12]    article code — zero-padded 6 digits
//	[12]      sale mode / article-type byte — mirrors L3[12]
//	[13]      literal '0'
//	[14:20]   Caducidad (shelf-life/expiry) — mirrors L3[13:19]
//	[20:26]   extra date — unused, left zero (mirrors L3[19:25])
//	[26:32]   FechaEnvasado (packaging) — mirrors L3[25:31]; spaces when unset
//	[32:39]   fixed + percentage tare — unused, left zero
//	[39:41]   FormatoEtiquetaSerieL (label format) — mirrors L3[38:40]
//	[41:43]   barcode format slot — mirrors L3[40:42]
//	[43:45]   fixed literal — left zero
//	[45:49]   section — "0001" default, mirrors L3[44:48]
//	[49:64]   VAT slot, logo id, class id, associated element, smiley code —
//	          unused, left zero
//	[64:77]   EAN scanner code, 13 chars — space-padded, or the fixed EAN when set
//	[77:98]   logo color, associated element, temp-offer id, piece weight —
//	          unused, left zero
//	[98:104]  FechaCongelacion (freezing date) — the field under test;
//	          spaces when unset, DDMMYY when provided
//	[104:130] alternate label format, color, stock warning, ad image,
//	          reserved — unused, left zero
func BuildH3Register(plu Dibal500PLU, shelfLifeDays *int) ([]byte, error) {
	buf := make([]byte, Dibal500RegisterLen)
	fillZeros(buf)

	copy(buf[0:2], pad2Digits(plu.LogicalAddr))
	buf[2] = 'H'
	buf[3] = '3'
	copy(buf[4:6], pad2Digits(plu.Group))

	code, err := numericField(plu.Code, 6)
	if err != nil {
		return nil, fmt.Errorf("kod artykułu: %w", err)
	}
	copy(buf[6:12], code)

	buf[12] = '0' // sale mode: weight-based default, mirrors L3
	buf[13] = '0' // literal padding byte

	days := 0
	if shelfLifeDays != nil {
		days = *shelfLifeDays
	}
	expiry, err := intField(days, 6)
	if err != nil {
		return nil, fmt.Errorf("termin ważności: %w", err)
	}
	copy(buf[14:20], expiry)

	// [20:26] extra date: unused, left zero.

	fillSpaces(buf[26:32])
	if fd := strings.TrimSpace(plu.FrozenDate); fd != "" {
		envasado, err := numericField(fd, 6)
		if err != nil {
			return nil, fmt.Errorf("data pakowania: %w", err)
		}
		copy(buf[26:32], envasado)
	}

	// [32:39] fixed + percentage tare: unused, left zero.

	labelNum, err := numericField(plu.LabelNum, 2)
	if err != nil {
		return nil, fmt.Errorf("numer formatu etykiety: %w", err)
	}
	copy(buf[39:41], labelNum)

	if slot := strings.TrimSpace(plu.BarcodeSlot); slot != "" {
		slotBytes, err := numericField(slot, 2)
		if err != nil {
			return nil, fmt.Errorf("slot kodu kreskowego: %w", err)
		}
		copy(buf[41:43], slotBytes)
	} else if strings.TrimSpace(plu.EAN) != "" {
		copy(buf[41:43], []byte("01"))
	}

	// [43:45] fixed literal: unused, left zero.
	copy(buf[45:49], []byte("0001")) // section: default 1
	// [49:64] VAT slot, logo id, class id, associated element, smiley code:
	// all unused, left zero.

	fillSpaces(buf[64:77])
	if ean := strings.TrimSpace(plu.EAN); ean != "" {
		copy(buf[64:77], textField(ean, 13))
	}

	// [77:98] logo color, associated element, temp-offer id, piece weight:
	// all unused, left zero.

	// [98:104] FechaCongelacion — the field under test. Spaces when unset,
	// matching the lesson learned from L3's FechaEnvasado (all-zeros reads
	// as a valid date 00/00/00 to the scale, not as "not set").
	fillSpaces(buf[98:104])
	if fd := strings.TrimSpace(plu.CongelacionDate); fd != "" {
		congelacion, err := numericField(fd, 6)
		if err != nil {
			return nil, fmt.Errorf("data zamrożenia (Congelación): %w", err)
		}
		copy(buf[98:104], congelacion)
	}

	// [104:130] alternate label format, color, stock warning, ad image,
	// reserved: all unused, left zero.

	return buf, nil
}

// Dibal500FormatField describes one placed field ("H6" register entry) on a
// Dibal 500-series label FORMAT — the physical layout uploaded to the scale
// (what Dibal's own DLD tool designs), as opposed to article DATA
// (L2/L3/L4/X4/AS, built by the functions above). Reverse-engineered from
// LN.dll (Serie500.EscribirTxCampos) and cross-checked against real .dld
// format exports read off the scale.
type Dibal500FormatField struct {
	FieldID  int // TIPO: campo_id from the sys_campos_etiqueta catalog (12=product name, 6=price, 124=free text, ...)
	X        int
	Y        int
	Rotation int
	Font     int // magnification + fontFamilyId*20 (LN.dll formula); raw numeric code for now, no font-name table yet
	Extra    int // "ANCHO" slot: width for variable-size fields, header/question index for a few special field types — 0 for simple fields
}

// dibal500FormatFieldsPerLine is how many 17-byte field entries fit after the
// 9-byte H6 header within one 130-byte register: (130-9)/17 = 7, with 2
// bytes to spare — matches LN.dll's own LIBRES(128,2).
const dibal500FormatFieldsPerLine = 7

// BuildFormatRegisters renders the "4R" header register plus as many "H6"
// field-placement registers as needed for a Dibal 500 label format. Byte
// layout (0-indexed), reverse-engineered from LN.dll's Serie500 class:
//
//	4R header (once):
//	 [0:2]    logical address, default "00"
//	 [2:4]    register type "4R"
//	 [4:6]    group, default "00"
//	 [6]      operation, 'A' (add/replace)
//	 [7:9]    format number
//	 [9:11]   field count (APARTADOS), 2 digits
//	 [11:41]  unused, spaces
//	 [41:45]  width, 4 digits
//	 [45:49]  height, 4 digits
//	 [49:130] unused, spaces
//
//	H6 field lines (as many as needed — up to 7 field entries per line):
//	 [0:2] logical address, [2:4] "H6", [4:6] group, [6] 'A', [7:9] format number
//	 then per field entry (17 bytes each, base = 9 + 17*i):
//	  [+0:+3]   TIPO (campo_id)
//	  [+3:+6]   X
//	  [+6:+10]  Y
//	  [+10:+11] rotation
//	  [+11:+14] font
//	  [+14:+17] extra ("ANCHO")
//	 remaining bytes on the last line: spaces
func BuildFormatRegisters(logicalAddr, group, formatNum string, width, height int, fields []Dibal500FormatField) ([][]byte, error) {
	formatBytes, err := numericField(formatNum, 2)
	if err != nil {
		return nil, fmt.Errorf("numer formatu: %w", err)
	}

	header := make([]byte, Dibal500RegisterLen)
	fillSpaces(header)
	copy(header[0:2], pad2Digits(logicalAddr))
	header[2] = '4'
	header[3] = 'R'
	copy(header[4:6], pad2Digits(group))
	header[6] = 'A'
	copy(header[7:9], formatBytes)

	count, err := intField(len(fields), 2)
	if err != nil {
		return nil, fmt.Errorf("liczba pól: %w", err)
	}
	copy(header[9:11], count)

	widthBytes, err := intField(width, 4)
	if err != nil {
		return nil, fmt.Errorf("szerokość: %w", err)
	}
	copy(header[41:45], widthBytes)

	heightBytes, err := intField(height, 4)
	if err != nil {
		return nil, fmt.Errorf("wysokość: %w", err)
	}
	copy(header[45:49], heightBytes)

	registers := [][]byte{header}

	for i := 0; i < len(fields); i += dibal500FormatFieldsPerLine {
		end := i + dibal500FormatFieldsPerLine
		if end > len(fields) {
			end = len(fields)
		}
		chunk := fields[i:end]

		line := make([]byte, Dibal500RegisterLen)
		fillSpaces(line)
		copy(line[0:2], pad2Digits(logicalAddr))
		line[2] = 'H'
		line[3] = '6'
		copy(line[4:6], pad2Digits(group))
		line[6] = 'A'
		copy(line[7:9], formatBytes)

		for j, field := range chunk {
			base := 9 + j*17

			tipo, err := intField(field.FieldID, 3)
			if err != nil {
				return nil, fmt.Errorf("pole (TIPO %d): %w", field.FieldID, err)
			}
			copy(line[base:base+3], tipo)

			x, err := intField(field.X, 3)
			if err != nil {
				return nil, fmt.Errorf("pole %d, X: %w", field.FieldID, err)
			}
			copy(line[base+3:base+6], x)

			y, err := intField(field.Y, 4)
			if err != nil {
				return nil, fmt.Errorf("pole %d, Y: %w", field.FieldID, err)
			}
			copy(line[base+6:base+10], y)

			rot, err := intField(field.Rotation, 1)
			if err != nil {
				return nil, fmt.Errorf("pole %d, rotacja: %w", field.FieldID, err)
			}
			copy(line[base+10:base+11], rot)

			font, err := intField(field.Font, 3)
			if err != nil {
				return nil, fmt.Errorf("pole %d, font: %w", field.FieldID, err)
			}
			copy(line[base+11:base+14], font)

			extra, err := intField(field.Extra, 3)
			if err != nil {
				return nil, fmt.Errorf("pole %d, extra: %w", field.FieldID, err)
			}
			copy(line[base+14:base+17], extra)
		}

		registers = append(registers, line)
	}

	return registers, nil
}

// BuildFormatRequestRegister renders the "PB" register that asks the scale
// to push back label-format registers over a server connection — the same
// register DFS/DLD send when the user clicks "Pedir formato"/"Recibir".
// Reverse-engineered from ComunicacionesBalPC.dll's FinDeDia.PB getter.
//
// Requesting a single format only works for 21-80 (scale-side protocol
// rule, confirmed in the decompiled getter: "if (!todos && numeroFormato >
// 20 && numeroFormato <= 80) mode='01' else mode='00', num=0"); anything
// else requests ALL formats and the caller must pick the wanted one out of
// the response by its own 4R header.
//
//	[0:2]    logical address
//	[2:4]    "PB"
//	[4:6]    group
//	[6:9]    "001" (fixed)
//	[9:11]   mode: "01" = single format, "00" = all formats
//	[11:13]  "00" (fixed)
//	[13:23]  requested format number, 10 digits zero-padded (0 when mode "00")
//	[23:130] '0' padding — this register goes through ComunicacionesBalPC.dll's
//	 generic string-register path (RellenarCadenaConCeros), which zero-pads
//	 short registers. This differs from 4R/H6, which LN.dll builds directly
//	 as a full 130-byte space-padded buffer — do not reuse fillSpaces here.
func BuildFormatRequestRegister(logicalAddr, group string, formatNum int) ([]byte, error) {
	reg := make([]byte, Dibal500RegisterLen)
	for i := range reg {
		reg[i] = '0'
	}
	copy(reg[0:2], pad2Digits(logicalAddr))
	reg[2] = 'P'
	reg[3] = 'B'
	copy(reg[4:6], pad2Digits(group))
	copy(reg[6:9], []byte("001"))

	mode := 0
	num := 0
	if formatNum > 20 && formatNum <= 80 {
		mode = 1
		num = formatNum
	}

	modeBytes, err := intField(mode, 2)
	if err != nil {
		return nil, fmt.Errorf("tryb: %w", err)
	}
	copy(reg[9:11], modeBytes)
	copy(reg[11:13], []byte("00"))

	numBytes, err := intField(num, 10)
	if err != nil {
		return nil, fmt.Errorf("numer formatu: %w", err)
	}
	copy(reg[13:23], numBytes)

	return reg, nil
}

// IsFormatTransferEnd reports whether reg is the "PB" echo the scale sends
// back to mark the end of a format transfer (GestionarRegistro in
// ComunicacionesBalPC.dll: register type "PB" with byte [11:13] "00" or
// "01" ends the collection loop).
func IsFormatTransferEnd(reg []byte) bool {
	if len(reg) != Dibal500RegisterLen {
		return false
	}
	if string(reg[2:4]) != "PB" {
		return false
	}
	mode := string(reg[11:13])
	return mode == "00" || mode == "01"
}

// ParsedDibalFormat is one label format decoded out of a "4R" header plus
// its "H6" field lines, as returned by the scale after a format-request PB.
type ParsedDibalFormat struct {
	FormatNumber string
	Width        int
	Height       int
	Fields       []Dibal500FormatField
}

// ParseFormatRegisters decodes raw registers received from the scale
// (4R/H6, in the order they arrive) back into one or more label formats —
// the inverse of BuildFormatRegisters. Multiple formats appear in one
// response when the request was for "all formats" (see
// BuildFormatRequestRegister); each new "4R" register starts a new format,
// and "H6" registers immediately after it are consumed until its declared
// field count is reached or the next "4R" appears. Non-4R/H6 registers
// (e.g. the terminating "PB" echo) are ignored.
func ParseFormatRegisters(registers [][]byte) ([]ParsedDibalFormat, error) {
	var formats []ParsedDibalFormat
	var current *ParsedDibalFormat
	var wantFields int

	for _, reg := range registers {
		if len(reg) != Dibal500RegisterLen {
			continue
		}
		switch string(reg[2:4]) {
		case "4R":
			formatNum := strings.TrimSpace(string(reg[7:9]))
			fieldCount, err := parseIntField(reg[9:11])
			if err != nil {
				return nil, fmt.Errorf("format %s: liczba pól: %w", formatNum, err)
			}
			width, err := parseIntField(reg[41:45])
			if err != nil {
				return nil, fmt.Errorf("format %s: szerokość: %w", formatNum, err)
			}
			height, err := parseIntField(reg[45:49])
			if err != nil {
				return nil, fmt.Errorf("format %s: wysokość: %w", formatNum, err)
			}
			formats = append(formats, ParsedDibalFormat{FormatNumber: formatNum, Width: width, Height: height})
			current = &formats[len(formats)-1]
			wantFields = fieldCount

		case "H6":
			if current == nil || len(current.Fields) >= wantFields {
				continue
			}
			remaining := wantFields - len(current.Fields)
			count := dibal500FormatFieldsPerLine
			if remaining < count {
				count = remaining
			}
			for j := 0; j < count; j++ {
				base := 9 + j*17
				if base+17 > len(reg) {
					break
				}
				tipo, err := parseIntField(reg[base : base+3])
				if err != nil {
					return nil, fmt.Errorf("format %s, pole %d: TIPO: %w", current.FormatNumber, len(current.Fields), err)
				}
				x, err := parseIntField(reg[base+3 : base+6])
				if err != nil {
					return nil, fmt.Errorf("format %s, pole %d: X: %w", current.FormatNumber, len(current.Fields), err)
				}
				y, err := parseIntField(reg[base+6 : base+10])
				if err != nil {
					return nil, fmt.Errorf("format %s, pole %d: Y: %w", current.FormatNumber, len(current.Fields), err)
				}
				rot, err := parseIntField(reg[base+10 : base+11])
				if err != nil {
					return nil, fmt.Errorf("format %s, pole %d: rotacja: %w", current.FormatNumber, len(current.Fields), err)
				}
				font, err := parseIntField(reg[base+11 : base+14])
				if err != nil {
					return nil, fmt.Errorf("format %s, pole %d: font: %w", current.FormatNumber, len(current.Fields), err)
				}
				extra, err := parseIntField(reg[base+14 : base+17])
				if err != nil {
					return nil, fmt.Errorf("format %s, pole %d: extra: %w", current.FormatNumber, len(current.Fields), err)
				}
				current.Fields = append(current.Fields, Dibal500FormatField{
					FieldID: tipo, X: x, Y: y, Rotation: rot, Font: font, Extra: extra,
				})
			}
		}
	}

	return formats, nil
}

// parseIntField reads a fixed-width ASCII-digit field, treating blank
// (space-filled) fields as zero.
func parseIntField(b []byte) (int, error) {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("wartość nienumeryczna %q", s)
	}
	return n, nil
}
