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
)

type registerResult struct {
	Index  int  `json:"index"`
	OK     bool `json:"ok"`
	Result int  `json:"result"`
}

func main() {
	if len(os.Args) < 5 {
		fail("usage: dibalcom-bridge <scaleIP> <scalePort> <pcIP> <pcPort> [timeoutMs] [transform] < registers.hex")
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
		results = append(results, registerResult{Index: i, OK: ok, Result: int(result)})
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
