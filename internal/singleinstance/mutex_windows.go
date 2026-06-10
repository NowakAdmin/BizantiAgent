//go:build windows

package singleinstance

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modkernel32      = windows.NewLazySystemDLL("kernel32.dll")
	procCreateMutexW = modkernel32.NewProc("CreateMutexW")

	mutexHandle windows.Handle
)

// ErrAlreadyRunning is returned when another instance holds the mutex.
var ErrAlreadyRunning = errors.New("inna instancja BizantiAgent jest już uruchomiona")

// Acquire creates a named Windows mutex. Returns ErrAlreadyRunning if another
// instance already holds it. The mutex is released automatically when the
// process exits; call Release for an explicit release.
func Acquire(name string) error {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}

	h, _, callErr := procCreateMutexW.Call(
		0, // lpMutexAttributes (NULL)
		1, // bInitialOwner = TRUE
		uintptr(unsafe.Pointer(namePtr)),
	)
	if h == 0 {
		return callErr
	}

	// ERROR_ALREADY_EXISTS (0xB7) means another process created the mutex first.
	if callErr == windows.ERROR_ALREADY_EXISTS {
		_ = windows.CloseHandle(windows.Handle(h))
		return ErrAlreadyRunning
	}

	mutexHandle = windows.Handle(h)
	return nil
}

// Release releases the named mutex acquired by Acquire.
func Release() {
	if mutexHandle != 0 {
		_ = windows.ReleaseMutex(mutexHandle)
		_ = windows.CloseHandle(mutexHandle)
		mutexHandle = 0
	}
}
