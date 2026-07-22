//go:build windows

// dibalcom-bridge is a tiny 32-bit helper that reuses Dibal's native commL.dll
// (which is 32-bit i386) to program a Dibal 500-series scale. The main agent is
// 64-bit and cannot load a 32-bit DLL, so it spawns this bridge.
//
// The bridge is deliberately dumb: it receives an already-built 130-byte L2
// register (as hex) plus the scale/PC addresses, opens the commL.dll client
// socket, sends the register, and reports the result as JSON on stdout.
//
// Build: GOOS=windows GOARCH=386 go build -o dibalcom-bridge.exe ./cmd/dibalcom-bridge
// Ship commL.dll alongside dibalcom-bridge.exe (same directory as the agent).
package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"unsafe"
)

func main() {
	if len(os.Args) < 6 {
		fail("usage: dibalcom-bridge <scaleIP> <scalePort> <pcIP> <pcPort> <hexRegister> [timeoutMs]")
	}

	scaleIP := os.Args[1]
	scalePort, _ := strconv.Atoi(os.Args[2])
	pcIP := os.Args[3]
	pcPort, _ := strconv.Atoi(os.Args[4])

	reg, err := hex.DecodeString(os.Args[5])
	if err != nil {
		fail("hex: " + err.Error())
	}
	if len(reg) != 130 {
		fail(fmt.Sprintf("rejestr ma %d bajtów, wymagane 130", len(reg)))
	}

	timeout := 3000
	if len(os.Args) >= 7 {
		if t, convErr := strconv.Atoi(os.Args[6]); convErr == nil && t > 0 {
			timeout = t
		}
	}

	// bTransformacion: 0 = send raw bytes (correct for our CP1250 path), 1 = let
	// commL.dll apply its byte transform. Defaults to 0.
	transform := uintptr(0)
	if len(os.Args) >= 8 && os.Args[7] == "1" {
		transform = 1
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

	if closeProc != nil {
		_, _, _ = closeProc.Call(uintptr(handle), uintptr(unsafe.Pointer(&emptyLog[0])))
	}

	emit(map[string]any{"ok": result == 1, "stage": "send", "handle": handle, "result": result})
	if result != 1 {
		os.Exit(1)
	}
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
