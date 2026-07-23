package devices

import (
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
	if len(regs) != 2 {
		t.Fatalf("got %d registers, want 2 (L2 + one X4 page)", len(regs))
	}
	if string(regs[0][2:4]) != "L2" {
		t.Errorf("register 0 type = %q, want L2", string(regs[0][2:4]))
	}
	if string(regs[1][2:4]) != "X4" {
		t.Errorf("register 1 type = %q, want X4", string(regs[1][2:4]))
	}

	// Delete operations don't touch X4 (nothing to sync).
	del, err := BuildArticleRegisters(Dibal500PLU{Mode: "B", Code: "1"})
	if err != nil {
		t.Fatalf("build delete error: %v", err)
	}
	if len(del) != 1 {
		t.Fatalf("delete: got %d registers, want 1 (L2 only)", len(del))
	}
}
