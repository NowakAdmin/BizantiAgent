//go:build !windows

package singleinstance

import "errors"

// ErrAlreadyRunning is returned when another instance holds the mutex.
var ErrAlreadyRunning = errors.New("inna instancja BizantiAgent jest już uruchomiona")

// Acquire is a no-op on non-Windows platforms.
func Acquire(_ string) error { return nil }

// Release is a no-op on non-Windows platforms.
func Release() {}
