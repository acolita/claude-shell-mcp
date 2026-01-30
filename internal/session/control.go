// Package session provides shell session management.
package session

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	localpty "github.com/acolita/claude-shell-mcp/internal/pty"
	"github.com/acolita/claude-shell-mcp/internal/ssh"
)

// ControlSession is a lightweight session for management commands.
// It executes commands directly without prompt detection or state tracking.
// Used for process inspection (ps), termination (kill), and PTY management.
type ControlSession struct {
	host      string // "local" or hostname
	mode      string // "local" or "ssh"
	pty       PTY
	sshClient *ssh.Client
	mu        sync.Mutex

	// SSH connection info (for ssh mode)
	port     int
	user     string
	password string
	keyPath  string
}

// ControlSessionOptions defines options for creating a control session.
type ControlSessionOptions struct {
	Mode     string // "local" or "ssh"
	Host     string
	Port     int
	User     string
	Password string
	KeyPath  string
}

// NewControlSession creates a new control session.
func NewControlSession(opts ControlSessionOptions) (*ControlSession, error) {
	cs := &ControlSession{
		mode:     opts.Mode,
		host:     opts.Host,
		port:     opts.Port,
		user:     opts.User,
		password: opts.Password,
		keyPath:  opts.KeyPath,
	}

	if cs.mode == "" {
		cs.mode = "local"
	}
	if cs.mode == "local" {
		cs.host = "local"
	}

	if err := cs.initialize(); err != nil {
		return nil, err
	}

	return cs, nil
}

// initialize sets up the PTY connection.
func (cs *ControlSession) initialize() error {
	if cs.mode == "ssh" {
		return cs.initializeSSH()
	}
	return cs.initializeLocal()
}

// initializeLocal sets up a local PTY.
func (cs *ControlSession) initializeLocal() error {
	opts := localpty.DefaultOptions()
	opts.NoRC = true // Don't source rc files for control session

	pty, err := localpty.NewLocalPTY(opts)
	if err != nil {
		return fmt.Errorf("create local pty: %w", err)
	}

	cs.pty = pty

	// Wait for shell to be ready
	time.Sleep(100 * time.Millisecond)
	cs.drainOutput()

	return nil
}

// initializeSSH sets up an SSH PTY.
func (cs *ControlSession) initializeSSH() error {
	if cs.host == "" {
		return fmt.Errorf("host is required for ssh mode")
	}
	if cs.user == "" {
		return fmt.Errorf("user is required for ssh mode")
	}
	if cs.port == 0 {
		cs.port = 22
	}

	// Build auth methods
	authCfg := ssh.AuthConfig{
		UseAgent: true,
		Password: cs.password,
		KeyPath:  cs.keyPath,
		Host:     cs.host,
	}

	authMethods, err := ssh.BuildAuthMethods(authCfg)
	if err != nil {
		return fmt.Errorf("build auth methods: %w", err)
	}

	hostKeyCallback, err := ssh.BuildHostKeyCallback("")
	if err != nil {
		hostKeyCallback = ssh.InsecureHostKeyCallback()
	}

	clientOpts := ssh.ClientOptions{
		Host:            cs.host,
		Port:            cs.port,
		User:            cs.user,
		AuthMethods:     authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         30 * time.Second,
	}

	client, err := ssh.NewClient(clientOpts)
	if err != nil {
		return fmt.Errorf("create ssh client: %w", err)
	}

	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect to %s: %w", cs.host, err)
	}

	ptyOpts := ssh.DefaultSSHPTYOptions()
	pty, err := ssh.NewSSHPTY(client, ptyOpts)
	if err != nil {
		client.Close()
		return fmt.Errorf("create ssh pty: %w", err)
	}

	cs.sshClient = client
	cs.pty = pty

	// Wait for shell to be ready
	time.Sleep(200 * time.Millisecond)
	cs.drainOutput()

	return nil
}

// Exec executes a command and returns the output.
// This is a simple blocking execution without prompt detection.
func (cs *ControlSession) Exec(ctx context.Context, command string) (string, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Use a unique marker to detect command completion
	marker := fmt.Sprintf("__CTRL_%d__", time.Now().UnixNano())
	fullCmd := fmt.Sprintf("%s; echo %s $?", command, marker)

	// Write command
	if _, err := cs.pty.WriteString(fullCmd + "\n"); err != nil {
		return "", fmt.Errorf("write command: %w", err)
	}

	// Read output until marker
	var output bytes.Buffer
	buf := make([]byte, 4096)

	for {
		select {
		case <-ctx.Done():
			return output.String(), ctx.Err()
		default:
		}

		cs.pty.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := cs.pty.Read(buf)
		if err != nil && !os.IsTimeout(err) && err != io.EOF && !isTimeoutError(err) {
			return output.String(), fmt.Errorf("read output: %w", err)
		}

		if n > 0 {
			output.Write(buf[:n])

			// Check for marker
			if strings.Contains(output.String(), marker) {
				break
			}
		}
	}

	// Extract output (remove command echo and marker line)
	result := cs.cleanOutput(output.String(), fullCmd, marker)
	return result, nil
}

// ExecSimple executes a command with a default timeout.
func (cs *ControlSession) ExecSimple(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return cs.Exec(ctx, command)
}

// cleanOutput removes command echo and marker from output.
func (cs *ControlSession) cleanOutput(output, command, marker string) string {
	lines := strings.Split(output, "\n")
	var result []string

	skipFirst := true
	for _, line := range lines {
		// Skip command echo (first line usually)
		if skipFirst && strings.Contains(line, command[:min(len(command), 20)]) {
			skipFirst = false
			continue
		}
		// Skip marker line
		if strings.Contains(line, marker) {
			continue
		}
		// Skip empty prompt lines
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "$" || strings.HasSuffix(trimmed, "$ ") {
			continue
		}
		result = append(result, line)
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}

// drainOutput drains any pending output from the PTY.
func (cs *ControlSession) drainOutput() {
	buf := make([]byte, 4096)
	for i := 0; i < 10; i++ {
		cs.pty.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		n, err := cs.pty.Read(buf)
		if err != nil || n == 0 {
			break
		}
	}
}

// KillPTY kills all processes attached to a PTY device.
// ptyName should be just the pts number, e.g., "3" for /dev/pts/3
func (cs *ControlSession) KillPTY(ctx context.Context, ptyName string) error {
	// pkill -9 -t pts/X kills all processes on that terminal
	cmd := fmt.Sprintf("pkill -9 -t pts/%s 2>/dev/null || true", ptyName)
	_, err := cs.Exec(ctx, cmd)
	return err
}

// GetPTYProcesses returns PIDs of processes attached to a PTY.
func (cs *ControlSession) GetPTYProcesses(ctx context.Context, ptyName string) ([]string, error) {
	cmd := fmt.Sprintf("ps -t pts/%s -o pid= 2>/dev/null", ptyName)
	output, err := cs.Exec(ctx, cmd)
	if err != nil {
		return nil, err
	}

	var pids []string
	for _, line := range strings.Split(output, "\n") {
		pid := strings.TrimSpace(line)
		if pid != "" {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// KillProcess kills a specific process by PID.
func (cs *ControlSession) KillProcess(ctx context.Context, pid string, signal int) error {
	cmd := fmt.Sprintf("kill -%d %s 2>/dev/null || true", signal, pid)
	_, err := cs.Exec(ctx, cmd)
	return err
}

// IsProcessRunning checks if a process is still running.
func (cs *ControlSession) IsProcessRunning(ctx context.Context, pid string) (bool, error) {
	cmd := fmt.Sprintf("ps -p %s -o pid= 2>/dev/null", pid)
	output, err := cs.Exec(ctx, cmd)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

// IsPTYAlive checks if a PTY has any processes (i.e., shell is alive).
func (cs *ControlSession) IsPTYAlive(ctx context.Context, ptyName string) (bool, error) {
	pids, err := cs.GetPTYProcesses(ctx, ptyName)
	if err != nil {
		return false, err
	}
	return len(pids) > 0, nil
}

// Host returns the host this control session is connected to.
func (cs *ControlSession) Host() string {
	return cs.host
}

// Close closes the control session.
func (cs *ControlSession) Close() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	var errs []error

	if cs.pty != nil {
		if err := cs.pty.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if cs.sshClient != nil {
		if err := cs.sshClient.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
