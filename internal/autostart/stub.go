//go:build !windows

package autostart

func IsEnabled(_ string) (bool, error) {
	return false, nil
}

func Enable(_ string, _ string, _ ...string) error {
	return nil
}

func Disable(_ string) error {
	return nil
}
