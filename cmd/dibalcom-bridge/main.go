//go:build windows

// dibalcom-bridge is a tiny 32-bit helper that reuses Dibal's native commL.dll
// (which is 32-bit i386) to program a Dibal 500-series scale. The main agent is
// 64-bit and cannot load a 32-bit DLL, so it spawns this bridge.
//
// The bridge is deliberately dumb: it receives already-built 130-byte registers
// (one hex-encoded line per register, on stdin — an article's L2 record plus
// any X4 composition pages), opens one commL.dll client socket, sends every
// register over it in order, and reports the result as JSON on stdout.
//
// Build: GOOS=windows GOARCH=386 go build -o dibalcom-bridge.exe ./cmd/dibalcom-bridge
// Ship commL.dll alongside dibalcom-bridge.exe (same directory as the agent).
package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/NowakAdmin/BizantiAgent/internal/devices"
)

type registerResult struct {
	Index    int    `json:"index"`
	OK       bool   `json:"ok"`
	Result   int    `json:"result"`
	EchoHex  string `json:"echo_hex,omitempty"`
	EchoLen  int    `json:"echo_len,omitempty"`
	EchoCode int    `json:"echo_code,omitempty"`
}

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "-read-format" {
		runReadFormat(os.Args[2:])
		return
	}

	if len(os.Args) < 5 {
		fail("usage: dibalcom-bridge <scaleIP> <scalePort> <pcIP> <pcPort> [timeoutMs] [transform] [echoTest] < registers.hex")
	}

	scaleIP := os.Args[1]
	scalePort, _ := strconv.Atoi(os.Args[2])
	pcIP := os.Args[3]
	pcPort, _ := strconv.Atoi(os.Args[4])

	timeout := 3000
	if len(os.Args) >= 6 {
		if t, convErr := strconv.Atoi(os.Args[5]); convErr == nil && t > 0 {
			timeout = t
		}
	}

	// bTransformacion: 0 = send raw bytes (correct for our CP1250 path), 1 = let
	// commL.dll apply its byte transform. Defaults to 0.
	transform := uintptr(0)
	if len(os.Args) >= 7 && os.Args[6] == "1" {
		transform = 1
	}

	// echoTest: diagnostic-only. When "1", attempt a ReadRegisterWEx2 right
	// after each successful send, on the same handle, to see whether the
	// scale echoes back the just-written bytes — lets us compare sent vs.
	// stored content electronically instead of reading it off a printout.
	echoTest := len(os.Args) >= 8 && os.Args[7] == "1"

	registers, err := readRegisters(os.Stdin)
	if err != nil {
		fail(err.Error())
	}
	if len(registers) == 0 {
		fail("brak rejestrów na stdin")
	}

	dll, err := loadCommL()
	if err != nil {
		fail(err.Error())
	}
	defer func() {
		_ = dll.Release()
	}()

	connectProc, err := dll.FindProc("ClientConnectFromWEx2")
	if err != nil {
		fail("brak eksportu ClientConnectFromWEx2: " + err.Error())
	}
	sendProc, err := dll.FindProc("SendRegisterWEx3")
	if err != nil {
		fail("brak eksportu SendRegisterWEx3: " + err.Error())
	}
	closeProc, _ := dll.FindProc("ClientCloseWEx2")

	var readProc *syscall.Proc
	if echoTest {
		readProc, err = dll.FindProc("ReadRegisterWEx2")
		if err != nil {
			fail("brak eksportu ReadRegisterWEx2: " + err.Error())
		}
	}

	// commL.dll takes C strings for addresses and a UTF-16 log path; an empty
	// (single-NUL) log buffer disables logging.
	emptyLog := []byte{0, 0}
	scaleB := append([]byte(scaleIP), 0)
	pcB := append([]byte(pcIP), 0)

	// int ClientConnectFromWEx2(char* sDirBal, int iPuertoBal, char* sDirPC, int iPuertoPC, char* sRutaLogs, int iTimer)
	h, _, _ := connectProc.Call(
		uintptr(unsafe.Pointer(&scaleB[0])),
		uintptr(scalePort),
		uintptr(unsafe.Pointer(&pcB[0])),
		uintptr(pcPort),
		uintptr(unsafe.Pointer(&emptyLog[0])),
		uintptr(timeout),
	)
	handle := int32(h)
	if handle <= 0 {
		emit(map[string]any{"ok": false, "stage": "connect", "handle": handle})
		os.Exit(2)
	}

	results := make([]registerResult, 0, len(registers))
	allOK := true

	for i, reg := range registers {
		if i > 0 {
			// ponytail: guards against a suspected scale-side receive-buffer
			// overrun when many registers are sent back-to-back on one
			// connection (diagnostic test showed byte corruption on
			// multi-register text pushes). Raise this if corruption persists.
			time.Sleep(100 * time.Millisecond)
		}

		// int SendRegisterWEx3(int handle, byte* buf, int len, char* log, int timer, BOOL transform, BOOL soloError)
		r, _, _ := sendProc.Call(
			uintptr(handle),
			uintptr(unsafe.Pointer(&reg[0])),
			uintptr(len(reg)),
			uintptr(unsafe.Pointer(&emptyLog[0])),
			uintptr(timeout),
			transform, // bTransformacion (0 = raw CP1250)
			1,         // bSoloError = true
		)
		result := int32(r)
		ok := result == 1
		rr := registerResult{Index: i, OK: ok, Result: int(result)}

		if ok && readProc != nil {
			echoBuf := make([]byte, 130)
			echoLen := make([]int32, 1)
			// int ReadRegisterWEx2(int handle, byte* buf, int* outLen, char* log, int timer, BOOL transform)
			er, _, _ := readProc.Call(
				uintptr(handle),
				uintptr(unsafe.Pointer(&echoBuf[0])),
				uintptr(unsafe.Pointer(&echoLen[0])),
				uintptr(unsafe.Pointer(&emptyLog[0])),
				uintptr(500), // short timeout — this is a best-effort peek, not a required ack
				transform,
			)
			rr.EchoCode = int(int32(er))
			rr.EchoLen = int(echoLen[0])
			if echoLen[0] > 0 && int(echoLen[0]) <= len(echoBuf) {
				rr.EchoHex = hex.EncodeToString(echoBuf[:echoLen[0]])
			}
		}

		results = append(results, rr)
		if !ok {
			allOK = false
			break // stop at the first failed register; nothing further would be consistent
		}
	}

	if closeProc != nil {
		_, _, _ = closeProc.Call(uintptr(handle), uintptr(unsafe.Pointer(&emptyLog[0])))
	}

	emit(map[string]any{"ok": allOK, "stage": "send", "handle": handle, "registers": results})
	if !allOK {
		os.Exit(1)
	}
}

// readRegisters parses one hex-encoded 130-byte register per non-empty line.
func readRegisters(r *os.File) ([][]byte, error) {
	var registers [][]byte
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		reg, err := hex.DecodeString(line)
		if err != nil {
			return nil, fmt.Errorf("hex: %w", err)
		}
		if len(reg) != 130 {
			return nil, fmt.Errorf("rejestr ma %d bajtów, wymagane 130", len(reg))
		}
		registers = append(registers, reg)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("stdin: %w", err)
	}
	return registers, nil
}

// runReadFormat implements "Pedir formato"/"Recibir" — requesting a label
// format's 4R/H6 registers back from the scale. This is a genuinely
// different flow from the write path above: DFS/DLD's own protocol (see
// ComunicacionesBalPC.dll's FinDeDia.RecogerDiseniosBalanza) sends the
// request ("PB" register) over a CLIENT connection, but receives the
// response over a separate SERVER connection that the scale connects back
// to — reusing ReadRegisterWEx2 on a CLIENT handle here would not work,
// this DLL only pushes format data to a listening server socket.
//
// usage: dibalcom-bridge -read-format <formatNum> <scaleIP> <scalePort> <pcIP> <pcPort> [timeoutMs]
func runReadFormat(args []string) {
	if len(args) < 5 {
		fail("usage: dibalcom-bridge -read-format <formatNum> <scaleIP> <scalePort> <pcIP> <pcPort> [timeoutMs]")
	}

	formatNum, err := strconv.Atoi(args[0])
	if err != nil {
		fail("format_num: " + err.Error())
	}
	scaleIP := args[1]
	scalePort, _ := strconv.Atoi(args[2])
	pcIP := args[3]
	pcPort, _ := strconv.Atoi(args[4])

	timeout := 3000
	if len(args) >= 6 {
		if t, convErr := strconv.Atoi(args[5]); convErr == nil && t > 0 {
			timeout = t
		}
	}

	dll, err := loadCommL()
	if err != nil {
		fail(err.Error())
	}
	defer func() {
		_ = dll.Release()
	}()

	clientConnectProc, err := dll.FindProc("ClientConnectFromWEx2")
	if err != nil {
		fail("brak eksportu ClientConnectFromWEx2: " + err.Error())
	}
	sendProc, err := dll.FindProc("SendRegisterWEx3")
	if err != nil {
		fail("brak eksportu SendRegisterWEx3: " + err.Error())
	}
	clientCloseProc, _ := dll.FindProc("ClientCloseWEx2")
	serverConnectProc, err := dll.FindProc("ServerConnectToWEx2")
	if err != nil {
		fail("brak eksportu ServerConnectToWEx2: " + err.Error())
	}
	serverCloseProc, _ := dll.FindProc("ServerCloseWEx2")
	readProc, err := dll.FindProc("ReadRegisterWEx2")
	if err != nil {
		fail("brak eksportu ReadRegisterWEx2: " + err.Error())
	}

	emptyLog := []byte{0, 0}
	scaleB := append([]byte(scaleIP), 0)
	pcB := append([]byte(pcIP), 0)

	// 1. Open the client connection and send the format request ("PB").
	ch, _, _ := clientConnectProc.Call(
		uintptr(unsafe.Pointer(&scaleB[0])),
		uintptr(scalePort),
		uintptr(unsafe.Pointer(&pcB[0])),
		uintptr(scalePort),
		uintptr(unsafe.Pointer(&emptyLog[0])),
		uintptr(timeout),
	)
	clientHandle := int32(ch)
	if clientHandle <= 0 {
		emit(map[string]any{"ok": false, "stage": "client_connect", "handle": int(clientHandle)})
		os.Exit(2)
	}

	pbReg, err := devices.BuildFormatRequestRegister("00", "00", formatNum)
	if err != nil {
		_, _, _ = clientCloseProc.Call(uintptr(clientHandle), uintptr(unsafe.Pointer(&emptyLog[0])))
		fail("PB: " + err.Error())
	}

	sr, _, _ := sendProc.Call(
		uintptr(clientHandle),
		uintptr(unsafe.Pointer(&pbReg[0])),
		uintptr(len(pbReg)),
		uintptr(unsafe.Pointer(&emptyLog[0])),
		uintptr(timeout),
		0, // bTransformacion = false
		1, // bSoloError = true
	)
	if sendResult := int32(sr); sendResult != 1 {
		_, _, _ = clientCloseProc.Call(uintptr(clientHandle), uintptr(unsafe.Pointer(&emptyLog[0])))
		emit(map[string]any{"ok": false, "stage": "send_pb", "result": int(sendResult)})
		os.Exit(2)
	}

	// 2. Open the server socket the scale will connect back to with the
	// design registers. Uses pcPort for both sides, matching
	// AbrirConexionServidor's own call (oBalanza.PuertoEnvio for both
	// iPuertoBal and iPuertoPC).
	sh, _, _ := serverConnectProc.Call(
		uintptr(unsafe.Pointer(&scaleB[0])),
		uintptr(pcPort),
		uintptr(unsafe.Pointer(&pcB[0])),
		uintptr(pcPort),
		uintptr(unsafe.Pointer(&emptyLog[0])),
		uintptr(timeout),
	)
	serverHandle := int32(sh)
	if serverHandle <= 0 {
		_, _, _ = clientCloseProc.Call(uintptr(clientHandle), uintptr(unsafe.Pointer(&emptyLog[0])))
		emit(map[string]any{"ok": false, "stage": "server_connect", "handle": int(serverHandle)})
		os.Exit(2)
	}

	// 3. Read registers until the PB terminator echo, a hard iteration
	// cap, or a fixed overall wall-clock budget — whichever comes first.
	// Every ReadRegisterWEx2 call itself is bounded by `timeout`, so this
	// loop cannot hang the process indefinitely even if the scale never
	// answers; the caller's own process-level timeout (see
	// runDibal500BridgeReadFormat in the agent) is a second, independent
	// backstop.
	const maxIterations = 60
	const overallBudget = 20 * time.Second
	deadline := time.Now().Add(overallBudget)

	var collected [][]byte
	terminatedBy := "max_iterations"

readLoop:
	for i := 0; i < maxIterations; i++ {
		if time.Now().After(deadline) {
			terminatedBy = "overall_timeout"
			break
		}

		buf := make([]byte, 130)
		outLen := make([]int32, 1)
		rr, _, _ := readProc.Call(
			uintptr(serverHandle),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&outLen[0])),
			uintptr(unsafe.Pointer(&emptyLog[0])),
			uintptr(timeout),
			0, // bTransformacion = false, matches our raw CP1250 write path
		)
		switch readResult := int32(rr); readResult {
		case 1:
			if outLen[0] == 130 {
				reg := make([]byte, 130)
				copy(reg, buf)
				collected = append(collected, reg)
				if devices.IsFormatTransferEnd(reg) {
					terminatedBy = "pb"
					break readLoop
				}
			}
		case 0:
			time.Sleep(100 * time.Millisecond)
		default:
			terminatedBy = fmt.Sprintf("read_error_%d", readResult)
			break readLoop
		}
	}

	_, _, _ = serverCloseProc.Call(uintptr(serverHandle), uintptr(1), uintptr(unsafe.Pointer(&emptyLog[0])))
	_, _, _ = clientCloseProc.Call(uintptr(clientHandle), uintptr(unsafe.Pointer(&emptyLog[0])))

	hexRegs := make([]string, len(collected))
	for i, reg := range collected {
		hexRegs[i] = hex.EncodeToString(reg)
	}
	emit(map[string]any{"ok": true, "stage": "read_format", "terminated_by": terminatedBy, "registers": hexRegs})
}

// loadCommL loads commL.dll from the bridge's own directory first, then the
// default search path.
func loadCommL() (*syscall.DLL, error) {
	if exe, err := os.Executable(); err == nil {
		local := filepath.Join(filepath.Dir(exe), "commL.dll")
		if dll, loadErr := syscall.LoadDLL(local); loadErr == nil {
			return dll, nil
		}
	}
	dll, err := syscall.LoadDLL("commL.dll")
	if err != nil {
		return nil, fmt.Errorf("nie można załadować commL.dll (umieść ją obok dibalcom-bridge.exe): %w", err)
	}
	return dll, nil
}

func emit(m map[string]any) {
	b, _ := json.Marshal(m)
	fmt.Println(string(b))
}

func fail(msg string) {
	emit(map[string]any{"ok": false, "error": msg})
	os.Exit(3)
}
