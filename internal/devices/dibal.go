package devices

// Dibal K-235 / K-265 ETH communication protocol.
//
// The W025S/K-265 scale uses a Lantronix ETS-1 Ethernet-RS232 converter
// configured in "connected mode": the adapter initiates persistent TCP
// connections FROM the scale TO the PC. The PC must listen as a TCP server
// on two ports:
//
//	RX port (default 3000) – scale connects here to receive commands from PC
//	TX port (default 3001) – scale connects here to send weight data to PC
//
// Physical frame format (K-235 series binary protocol):
//
//	[ENQ] → (scale answers ACK) → [STX][ADDR][DATA][ETX][BCC] → (scale answers ACK)
//
// BCC = XOR of ADDR through ETX inclusive.
// FS (0x1C, ASCII "File Separator") divides fields inside DATA.
//
// High-level register lines (as used by dibaldrv.exe) are semicolon-delimited:
//
//	"KB;00;02;01"  – lock scale keyboard (required before programming)
//	"X1;00;M;000001;MĄKA;001099;000000000000;0;000000;000000;00000;01;000000;0000;00;0;0;0;00;00000" – PLU record
//	"KB;00;02;03"  – unlock scale keyboard
//
// The agent converts each semicolon-delimited line into a binary frame and
// exchanges ENQ/ACK handshakes just like dibaldrv.exe would.

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	dibalENQ byte = 0x05 // Enquiry – PC initiates exchange
	dibalACK byte = 0x06 // Acknowledge – scale confirms reception
	dibalNAK byte = 0x15 // Not-acknowledge – scale signals error
	dibalSTX byte = 0x02 // Start of frame data
	dibalETX byte = 0x03 // End of frame data
	dibalFS  byte = 0x1C // Field separator used inside each frame

	// DibalDefaultAddr is the default K-235 scale address used when
	// ScaleConfig.DibalAddr is zero.
	DibalDefaultAddr byte = 0x01

	dibalDefaultTimeout time.Duration = 5 * time.Second
)

// DibalPLU contains all fields of a K-series PLU (Article) record.
// Maps to register "X1" in the Dibal protocol.
type DibalPLU struct {
	// Group is the 2-digit department code. Defaults to "00".
	Group string
	// Mode: "M" = modify/upsert (default), "A" = add only, "D" = delete.
	Mode string
	// Code is the PLU number, up to 6 digits (zero-padded to 6), e.g. "1" → "000001".
	Code string
	// Name is the product name displayed on the scale / printed on label (≤20 chars for K-235).
	Name string
	// Price is the price in the smallest currency unit (grosze), up to 6 digits.
	// 1099 = 10.99 PLN.
	Price string
	// Barcode is the EAN-13 / EAN-8 code, without check digit padding.
	// Leave empty or "0" to use "000000000000".
	Barcode string
	// LabelNum is the label template number stored in the scale (default "01").
	LabelNum string
}

// buildDibalFrame converts a semicolon-delimited high-level Dibal register line
// into a binary K-series frame.
//
//	Input:  "X1;00;M;000001;MĄKA;001099;000000000000;0;..."
//	Output: [STX][ADDR][X1][FS][00][FS][M][FS]...[ETX][BCC]
func buildDibalFrame(addr byte, semicolonLine string) []byte {
	parts := strings.Split(semicolonLine, ";")

	// Build DATA section with FS between fields
	var data []byte
	for i, part := range parts {
		data = append(data, []byte(part)...)
		if i < len(parts)-1 {
			data = append(data, dibalFS)
		}
	}

	// [STX][ADDR][DATA][ETX][BCC]
	frame := make([]byte, 0, 3+len(data)+2)
	frame = append(frame, dibalSTX)
	frame = append(frame, addr)
	frame = append(frame, data...)
	frame = append(frame, dibalETX)

	// BCC = XOR of ADDR + every DATA byte + ETX
	bcc := addr
	for _, b := range data {
		bcc ^= b
	}
	bcc ^= dibalETX
	frame = append(frame, bcc)

	return frame
}

// sendDibalPacket executes one ENQ → ACK → frame → ACK exchange.
func sendDibalPacket(conn net.Conn, frame []byte, timeout time.Duration) error {
	_ = conn.SetDeadline(time.Now().Add(timeout))

	// Send ENQ
	if _, err := conn.Write([]byte{dibalENQ}); err != nil {
		return fmt.Errorf("błąd wysyłania ENQ: %w", err)
	}

	// Wait for ACK from scale
	ack := make([]byte, 1)
	if _, err := conn.Read(ack); err != nil {
		return fmt.Errorf("brak ACK po ENQ: %w", err)
	}
	if ack[0] != dibalACK {
		return fmt.Errorf("oczekiwano ACK (0x06) po ENQ, otrzymano 0x%02X", ack[0])
	}

	// Send frame
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(frame); err != nil {
		return fmt.Errorf("błąd wysyłania ramki Dibal: %w", err)
	}

	// Wait for final ACK confirming data reception
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Read(ack); err != nil {
		return fmt.Errorf("brak ACK po ramce Dibal: %w", err)
	}
	if ack[0] != dibalACK {
		if ack[0] == dibalNAK {
			return fmt.Errorf("waga odrzuciła pakiet Dibal (NAK 0x15)")
		}
		return fmt.Errorf("nieoczekiwana odpowiedź 0x%02X po ramce Dibal", ack[0])
	}

	return nil
}

// SendDibalLines sends a sequence of high-level semicolon-delimited register
// lines over an already-established TCP connection.
//
// Example lines:
//
//	[]string{
//	    "KB;00;02;01",                                               // lock
//	    "X1;00;M;000001;MĄKA;001099;000000000000;0;...",            // PLU
//	    "KB;00;02;03",                                               // unlock
//	}
func SendDibalLines(conn net.Conn, addr byte, lines []string, timeout time.Duration) error {
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimRight(line, ";"))
		if line == "" {
			continue
		}
		frame := buildDibalFrame(addr, line)
		if err := sendDibalPacket(conn, frame, timeout); err != nil {
			preview := line
			if len(preview) > 30 {
				preview = preview[:30] + "…"
			}
			return fmt.Errorf("błąd Dibal dla '%s': %w", preview, err)
		}
	}
	return nil
}

// SendDibalPLU programs a single PLU into the scale over an open TCP
// connection, wrapped in the mandatory KB lock/unlock sequence.
func SendDibalPLU(conn net.Conn, addr byte, plu DibalPLU, timeout time.Duration) error {
	mode := plu.Mode
	if mode == "" {
		mode = "M"
	}
	group := dibalPad(plu.Group, 2)
	code := dibalPad(plu.Code, 6)
	price := dibalPad(plu.Price, 6)

	barcode := strings.TrimSpace(plu.Barcode)
	if barcode == "" || barcode == "0" {
		barcode = "000000000000"
	}
	barcode = dibalPad(barcode, 12)

	labelNum := dibalPad(plu.LabelNum, 2)
	if plu.LabelNum == "" {
		labelNum = "01"
	}

	// Build X1 register line.
	// Fields 7-20 are K-235 fixed defaults (flags, tare, sell-by date, etc.).
	pluLine := strings.Join([]string{
		"X1", group, mode, code, plu.Name, price,
		barcode, "0", "000000", "000000", "00000",
		labelNum, "000000", "0000", "00", "0", "0", "0", "00", "00000",
	}, ";")

	lines := []string{
		"KB;00;02;01", // lock keyboard / begin programming session
		pluLine,
		"KB;00;02;03", // unlock keyboard / end programming session
	}

	return SendDibalLines(conn, addr, lines, timeout)
}

// SendDibalContentOverTCPServer listens on the RX port for the scale's inbound
// connection (Lantronix "connected" mode) and then sends the given register
// lines. content is a newline-separated string of high-level semicolon lines.
//
// This replaces Windows Spooler for Dibal scales — no Windows driver needed.
func SendDibalContentOverTCPServer(cfg PrinterConfig, content string) error {
	bindHost := strings.TrimSpace(cfg.DibalBindHost)
	if bindHost == "" {
		bindHost = "0.0.0.0"
	}

	rxPort := cfg.DibalRXPort
	if rxPort <= 0 {
		rxPort = 3000
	}

	addr := cfg.DibalAddr
	if addr == 0 {
		addr = DibalDefaultAddr
	}

	timeout := dibalDefaultTimeout
	if cfg.WriteTimeoutS > 0 {
		timeout = time.Duration(cfg.WriteTimeoutS) * time.Second
	}

	lines := strings.Split(content, "\n")

	rxAddr := net.JoinHostPort(bindHost, strconv.Itoa(rxPort))
	listener, err := net.Listen("tcp", rxAddr)
	if err != nil {
		return fmt.Errorf("nie można uruchomić nasłuchu Dibal RX na %s: %w", rxAddr, err)
	}
	defer func() {
		_ = listener.Close()
	}()

	if tcpL, ok := listener.(*net.TCPListener); ok {
		_ = tcpL.SetDeadline(time.Now().Add(timeout))
	}

	conn, err := listener.Accept()
	if err != nil {
		return fmt.Errorf("brak połączenia od wagi Dibal (RX %s) w ciągu %v: %w", rxAddr, timeout, err)
	}
	defer func() {
		_ = conn.Close()
	}()

	return SendDibalLines(conn, addr, lines, timeout)
}

// dibalPad zero-pads a string on the left to the requested length.
func dibalPad(s string, length int) string {
	s = strings.TrimSpace(s)
	for len(s) < length {
		s = "0" + s
	}
	return s
}
