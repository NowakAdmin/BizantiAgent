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
