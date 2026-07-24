package devices

import (
	"strings"
	"testing"
)

func TestBuildL2Register(t *testing.T) {
	reg, err := BuildL2Register(Dibal500PLU{
		Mode:        "M",
		Code:        "1",
		Name:        "Produkt testowy",
		PriceGrosze: 12300,
	})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if len(reg) != 130 {
		t.Fatalf("length = %d, want 130", len(reg))
	}

	cases := []struct {
		off  int
		want string
	}{
		{0, "00"},               // logical address default
		{2, "L2"},               // register type
		{4, "00"},               // group default
		{6, "M"},                // operation
		{7, "000001"},           // code, 6 digits
		{13, "-01"},             // direct key none
		{16, "Produkt testowy"}, // name (prefix, space-padded to 24)
		{88, "00012300"},        // gross price (grosze), 8 digits
	}
	for _, c := range cases {
		got := string(reg[c.off : c.off+len(c.want)])
		if got != c.want {
			t.Errorf("offset %d = %q, want %q", c.off, got, c.want)
		}
	}

	// name field must be space-padded, not null-padded
	if reg[16+len("Produkt testowy")] != ' ' {
		t.Errorf("name field not space-padded at end")
	}

	// delete op blanks the payload region
	del, err := BuildL2Register(Dibal500PLU{Mode: "B", Code: "1"})
	if err != nil {
		t.Fatalf("build delete error: %v", err)
	}
	for i := 13; i < 130; i++ {
		if del[i] != ' ' {
			t.Fatalf("delete: byte %d = %d, want space", i, del[i])
		}
	}

	// direct key in range renders 3 digits
	dk, _ := BuildL2Register(Dibal500PLU{Code: "5", DirectKey: "7"})
	if string(dk[13:16]) != "007" {
		t.Errorf("direct key = %q, want 007", string(dk[13:16]))
	}

	// Polish name is encoded to Windows-1250 (one byte per char), not UTF-8
	pl, _ := BuildL2Register(Dibal500PLU{Code: "9", Name: "Łosoś"})
	wantName := []byte{0xA3, 'o', 's', 'o', 0x9C} // Ł o s o ś
	if got := pl[16 : 16+len(wantName)]; string(got) != string(wantName) {
		t.Errorf("CP1250 name = % X, want % X", got, wantName)
	}
	// 5 chars -> 5 bytes, then spaces
	if pl[16+5] != ' ' {
		t.Errorf("CP1250 name not space-padded after 5 bytes")
	}
}

func TestBuildX4Registers(t *testing.T) {
	// Empty text still yields exactly one ESC-only page (clears stale text).
	empty, err := BuildX4Registers("00", "00", "1", "")
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if len(empty) != 1 {
		t.Fatalf("empty text: got %d pages, want 1", len(empty))
	}
	if got := string(empty[0][0:14]); got != "00X400"+"000001"+"01" {
		t.Errorf("empty page header = %q", got)
	}
	if empty[0][14] != 0x1B {
		t.Errorf("empty page: byte 14 = %d, want ESC (27)", empty[0][14])
	}
	for i := 15; i < 130; i++ {
		if empty[0][i] != ' ' {
			t.Fatalf("empty page: byte %d = %d, want space", i, empty[0][i])
		}
	}

	// Short text: one page, text then ESC then spaces.
	short, err := BuildX4Registers("00", "00", "2", "Mleko, cukier")
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if len(short) != 1 {
		t.Fatalf("short text: got %d pages, want 1", len(short))
	}
	wantText := "Mleko, cukier"
	if got := string(short[0][14 : 14+len(wantText)]); got != wantText {
		t.Errorf("short text content = %q, want %q", got, wantText)
	}
	if short[0][14+len(wantText)] != 0x1B {
		t.Errorf("short text: no ESC terminator after text")
	}

	// Exact multiple of the 116-byte chunk size: full page with no ESC, plus
	// a trailing ESC-only page.
	exact := make([]byte, x4ChunkSize)
	for i := range exact {
		exact[i] = 'A'
	}
	pages, err := BuildX4Registers("00", "00", "3", string(exact))
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("exact multiple: got %d pages, want 2", len(pages))
	}
	if got := string(pages[0][12:14]); got != "01" {
		t.Errorf("page 1 number = %q, want 01", got)
	}
	for i := 14; i < 130; i++ {
		if pages[0][i] != 'A' {
			t.Fatalf("page 1: byte %d = %d, want 'A' (no ESC expected)", i, pages[0][i])
		}
	}
	if got := string(pages[1][12:14]); got != "02" {
		t.Errorf("page 2 number = %q, want 02", got)
	}
	if pages[1][14] != 0x1B {
		t.Errorf("page 2: byte 14 = %d, want ESC (27)", pages[1][14])
	}

	// Long text respects the SERIE_L cap and doesn't error.
	long := make([]byte, x4MaxTextBytes+500)
	for i := range long {
		long[i] = 'B'
	}
	capped, err := BuildX4Registers("00", "00", "4", string(long))
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	totalTextBytes := 0
	for _, p := range capped {
		for i := 14; i < 130; i++ {
			if p[i] == 0x1B {
				break
			}
			if p[i] == 'B' {
				totalTextBytes++
			}
		}
	}
	if totalTextBytes != x4MaxTextBytes {
		t.Errorf("capped text bytes = %d, want %d", totalTextBytes, x4MaxTextBytes)
	}
}

func TestBuildL3Register(t *testing.T) {
	days := 14
	reg, err := BuildL3Register(Dibal500PLU{Code: "5", LabelNum: "3"}, &days)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if len(reg) != 130 {
		t.Fatalf("length = %d, want 130", len(reg))
	}

	cases := []struct {
		off  int
		want string
	}{
		{0, "00"},      // logical address default
		{2, "L3"},      // register type
		{4, "00"},      // group default
		{6, "000005"},  // code, 6 digits
		{12, "0"},      // sale mode default
		{13, "000014"}, // shelf-life days, 6 digits
		{38, "03"},     // label format number
		{40, "00"},     // barcode format slot: no EAN, stays off
		{44, "0001"},   // section default
	}
	for _, c := range cases {
		got := string(reg[c.off : c.off+len(c.want)])
		if got != c.want {
			t.Errorf("offset %d = %q, want %q", c.off, got, c.want)
		}
	}

	// Everything not explicitly tracked defaults to ASCII zero, not spaces.
	if reg[19] != '0' || reg[36] != '0' || reg[129] != '0' {
		t.Errorf("untracked fields not zero-filled: [19]=%c [36]=%c [129]=%c", reg[19], reg[36], reg[129])
	}

	// No shelf life set -> "000000", not an error.
	noExpiry, err := BuildL3Register(Dibal500PLU{Code: "5"}, nil)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if got := string(noExpiry[13:19]); got != "000000" {
		t.Errorf("no-expiry days = %q, want 000000", got)
	}

	// EAN present -> barcode format slot 01 enabled.
	withEAN, err := BuildL3Register(Dibal500PLU{Code: "5", EAN: "5901234123457"}, nil)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if got := string(withEAN[40:42]); got != "01" {
		t.Errorf("barcode format slot = %q, want 01 when EAN is set", got)
	}

	// Frozen date present -> written as DDMMYY at [25:31]; absent -> zeros.
	withFrozen, err := BuildL3Register(Dibal500PLU{Code: "5", FrozenDate: "230724"}, nil)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if got := string(withFrozen[25:31]); got != "230724" {
		t.Errorf("frozen date = %q, want 230724", got)
	}
	noFrozen, err := BuildL3Register(Dibal500PLU{Code: "5"}, nil)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if got := string(noFrozen[25:31]); got != "000000" {
		t.Errorf("no frozen date = %q, want 000000", got)
	}
}

func TestBuildArticleRegisters(t *testing.T) {
	regs, err := BuildArticleRegisters(Dibal500PLU{
		Code:        "1",
		Name:        "Produkt",
		PriceGrosze: 100,
		Composition: "Mleko",
	})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	// L2 + one X4 page + five L4 registers + one AS register.
	if len(regs) != 8 {
		t.Fatalf("got %d registers, want 8 (L2 + X4 + 5×L4 + AS)", len(regs))
	}
	if string(regs[0][2:4]) != "L2" {
		t.Errorf("register 0 type = %q, want L2", string(regs[0][2:4]))
	}
	if string(regs[1][2:4]) != "X4" {
		t.Errorf("register 1 type = %q, want X4", string(regs[1][2:4]))
	}
	for i := 2; i < 7; i++ {
		if string(regs[i][2:4]) != "L4" {
			t.Errorf("register %d type = %q, want L4", i, string(regs[i][2:4]))
		}
	}
	if string(regs[7][2:4]) != "AS" {
		t.Errorf("register 7 type = %q, want AS", string(regs[7][2:4]))
	}

	// ShelfLifeDays set -> L3 is appended after L2+X4+5×L4+AS.
	days := 7
	withExpiry, err := BuildArticleRegisters(Dibal500PLU{
		Code:          "1",
		Name:          "Produkt",
		PriceGrosze:   100,
		ShelfLifeDays: &days,
	})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if len(withExpiry) != 9 {
		t.Fatalf("got %d registers, want 9 (L2 + X4 + 5×L4 + AS + L3)", len(withExpiry))
	}
	if string(withExpiry[8][2:4]) != "L3" {
		t.Errorf("last register type = %q, want L3", string(withExpiry[8][2:4]))
	}

	// EAN alone (no ShelfLifeDays) also triggers L3 — it carries the barcode
	// format slot needed to enable the fixed EAN.
	withEAN, err := BuildArticleRegisters(Dibal500PLU{
		Code:        "1",
		Name:        "Produkt",
		PriceGrosze: 100,
		EAN:         "5901234123457",
	})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if len(withEAN) != 9 {
		t.Fatalf("got %d registers, want 9 (L2 + X4 + 5×L4 + AS + L3)", len(withEAN))
	}
	if string(withEAN[8][2:4]) != "L3" {
		t.Errorf("last register type = %q, want L3", string(withEAN[8][2:4]))
	}

	// FrozenDate alone also triggers L3.
	withFrozen, err := BuildArticleRegisters(Dibal500PLU{
		Code:        "1",
		Name:        "Produkt",
		PriceGrosze: 100,
		FrozenDate:  "230724",
	})
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if len(withFrozen) != 9 {
		t.Fatalf("got %d registers, want 9 (L2 + X4 + 5×L4 + AS + L3)", len(withFrozen))
	}
	if string(withFrozen[8][2:4]) != "L3" {
		t.Errorf("last register type = %q, want L3", string(withFrozen[8][2:4]))
	}

	// Delete operations don't touch X4, L4, AS or L3 (nothing to sync).
	del, err := BuildArticleRegisters(Dibal500PLU{Mode: "B", Code: "1", ShelfLifeDays: &days})
	if err != nil {
		t.Fatalf("build delete error: %v", err)
	}
	if len(del) != 1 {
		t.Fatalf("delete: got %d registers, want 1 (L2 only)", len(del))
	}
}

func TestBuildASRegister(t *testing.T) {
	reg, err := BuildASRegister("00", "00", "6", "5901234123457")
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if len(reg) != 130 {
		t.Fatalf("length = %d, want 130", len(reg))
	}

	cases := []struct {
		off  int
		want string
	}{
		{0, "00"},             // logical address default
		{2, "AS"},             // register type
		{4, "00"},             // group default
		{6, "000006"},         // code, 6 digits
		{12, "5901234123457"}, // EAN, exactly 13 chars
	}
	for _, c := range cases {
		got := string(reg[c.off : c.off+len(c.want)])
		if got != c.want {
			t.Errorf("offset %d = %q, want %q", c.off, got, c.want)
		}
	}

	for i := 25; i < 130; i++ {
		if reg[i] != '0' {
			t.Fatalf("tail: byte %d = %d, want '0'", i, reg[i])
		}
	}

	// Shorter EAN is space-padded to fill the 13-byte field.
	short, err := BuildASRegister("00", "00", "1", "123")
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if got := string(short[12:15]); got != "123" {
		t.Errorf("short EAN content = %q, want 123", got)
	}
	if short[15] != ' ' {
		t.Errorf("short EAN not space-padded")
	}

	// Empty EAN clears the field (all spaces) rather than erroring.
	empty, err := BuildASRegister("00", "00", "1", "")
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	for i := 12; i < 25; i++ {
		if empty[i] != ' ' {
			t.Fatalf("empty EAN: byte %d = %d, want space", i, empty[i])
		}
	}
}

// l4Interleaved builds the expected 48-byte wire encoding for a line: a
// space filler before each character, space-padded to the full slot width —
// mirrors l4EncodeLine so tests assert against the documented wire format,
// not the implementation.
func l4Interleaved(text string) []byte {
	out := make([]byte, l4LineWidth)
	for i := range out {
		out[i] = ' '
	}
	for i, c := range []byte(text) {
		out[i*2] = ' '
		out[i*2+1] = c
	}
	return out
}

func TestBuildL4Registers(t *testing.T) {
	// Short text: slot 0 (Tek1, under the product name) is reserved and
	// always blank; composition starts at slot 1 (Tek2, register 0 slot B).
	regs, err := BuildL4Registers("00", "00", "6", "Mleko, cukier")
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if len(regs) != 5 {
		t.Fatalf("got %d registers, want 5", len(regs))
	}
	if got := string(regs[0][12]); got != "0" {
		t.Errorf("register 0 line-A marker = %q, want \"0\"", got)
	}
	// Tek1 (slot 0) stays blank.
	for i := 13; i < 61; i++ {
		if regs[0][i] != ' ' {
			t.Fatalf("Tek1 (reserved): byte %d = %d, want space", i, regs[0][i])
		}
	}
	if got := string(regs[0][67]); got != "1" {
		t.Errorf("register 0 line-B marker = %q, want \"1\"", got)
	}
	// Confirmed on hardware: 2 bytes per character (space filler + char),
	// not 1 byte per character like every other text field.
	wantLineB := l4Interleaved("Mleko, cukier")
	if got := regs[0][68:116]; string(got) != string(wantLineB) {
		t.Errorf("Tek2 (composition line 1) = % X, want % X", got, wantLineB)
	}
	// tail always zero-filled
	for i := 116; i < 130; i++ {
		if regs[0][i] != '0' {
			t.Fatalf("tail: byte %d = %d, want '0'", i, regs[0][i])
		}
	}
	// register markers increment 2/3, 4/5, 6/7, 8/9 across the 5 registers
	for reg := 1; reg < 5; reg++ {
		wantA := byte('0' + 2*reg)
		wantB := byte('0' + 2*reg + 1)
		if regs[reg][12] != wantA {
			t.Errorf("register %d line-A marker = %c, want %c", reg, regs[reg][12], wantA)
		}
		if regs[reg][67] != wantB {
			t.Errorf("register %d line-B marker = %c, want %c", reg, regs[reg][67], wantB)
		}
	}

	// Long text spanning multiple 24-char lines (2-byte encoding still caps
	// each line at 24 visible characters), starting at Tek2.
	long := strings.Repeat("A", 24) + strings.Repeat("B", 24) + "C"
	multi, err := BuildL4Registers("00", "00", "7", long)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	// Tek2 (register 0 slot B) = first 24 A's.
	wantLine1 := l4Interleaved(strings.Repeat("A", 24))
	if got := multi[0][68:116]; string(got) != string(wantLine1) {
		t.Errorf("Tek2 = % X, want % X", got, wantLine1)
	}
	// Tek3 (register 1 slot A) = 24 B's.
	wantLine2 := l4Interleaved(strings.Repeat("B", 24))
	if got := multi[1][13:61]; string(got) != string(wantLine2) {
		t.Errorf("Tek3 = % X, want % X", got, wantLine2)
	}
	// Tek4 (register 1 slot B) = "C": filler then 'C' at the start.
	if multi[1][68] != ' ' || multi[1][69] != 'C' {
		t.Errorf("Tek4 start = %c%c, want space+C", multi[1][68], multi[1][69])
	}

	// Text beyond 216 chars (9 usable lines x 24, since Tek1 is reserved) is
	// dropped, not an error.
	tooLong := strings.Repeat("Z", 300)
	capped, err := BuildL4Registers("00", "00", "8", tooLong)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if len(capped) != 5 {
		t.Fatalf("got %d registers, want 5 even when input exceeds capacity", len(capped))
	}
	// Tek1 (slot 0) stays blank even for very long input.
	for i := 13; i < 61; i++ {
		if capped[0][i] != ' ' {
			t.Fatalf("Tek1 (reserved) with long input: byte %d = %d, want space", i, capped[0][i])
		}
	}
}
