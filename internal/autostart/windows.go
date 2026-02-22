//go:build windows

package autostart

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func IsEnabled(appName string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = k.Close()
	}()

	v, _, err := k.GetStringValue(appName)
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(v) != "", nil
}

func Enable(appName string, executablePath string, args ...string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() {
		_ = k.Close()
	}()

	command := fmt.Sprintf("\"%s\"", executablePath)
	if len(args) > 0 {
		command += " " + strings.Join(args, " ")
	}

	return k.SetStringValue(appName, command)
}

func Disable(appName string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() {
		_ = k.Close()
	}()

	err = k.DeleteValue(appName)
	if err == registry.ErrNotExist {
		return nil
	}

	return err
}
