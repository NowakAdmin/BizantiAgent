package devices

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHExecConfig is the payload for the "ssh_exec" command: run a single
// command on a remote host over SSH and return its output. This is a
// diagnostic primitive (analogous to tcp_probe/serial_probe) for inspecting
// devices that expose an SSH shell (e.g. a printer's embedded Linux OS)
// without needing RDP into the agent machine first.
type SSHExecConfig struct {
	Host      string `json:"host"`
	Port      int    `json:"port,omitempty"`
	User      string `json:"user"`
	Password  string `json:"password"`
	Command   string `json:"command"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

// SSHExec opens an SSH connection, runs Command once, and returns combined
// stdout+stderr.
func SSHExec(cfg SSHExecConfig) (string, error) {
	if strings.TrimSpace(cfg.Host) == "" || strings.TrimSpace(cfg.User) == "" {
		return "", fmt.Errorf("ssh_exec: wymagane pola 'host' i 'user'")
	}
	if strings.TrimSpace(cfg.Command) == "" {
		return "", fmt.Errorf("ssh_exec: wymagane pole 'command'")
	}

	port := cfg.Port
	if port <= 0 {
		port = 22
	}

	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	if cfg.TimeoutMs <= 0 {
		timeout = 10 * time.Second
	}

	config := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            []ssh.AuthMethod{ssh.Password(cfg.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // ponytail: diagnostic tool against known-IP devices, not a hardened SSH client
		Timeout:         timeout,
	}

	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", port))
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return "", fmt.Errorf("ssh_exec: błąd połączenia: %w", err)
	}
	defer func() {
		_ = client.Close()
	}()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("ssh_exec: błąd sesji: %w", err)
	}
	defer func() {
		_ = session.Close()
	}()

	var output bytes.Buffer
	session.Stdout = &output
	session.Stderr = &output

	if err := session.Run(cfg.Command); err != nil {
		// A non-zero remote exit status is a normal diagnostic outcome, not a
		// tool failure — return whatever the command printed either way.
		if _, isExitErr := err.(*ssh.ExitError); isExitErr {
			return output.String(), nil
		}
		return output.String(), fmt.Errorf("ssh_exec: błąd wykonania komendy: %w", err)
	}

	return output.String(), nil
}
