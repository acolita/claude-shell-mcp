package pty

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// readAllWithTimeout reads from the PTY in a goroutine until the timeout
// expires. It collects all output available within the timeout window.
func readAllWithTimeout(p *LocalPTY, timeout time.Duration) string {
	type readResult struct {
		data []byte
		err  error
	}

	ch := make(chan readResult, 256)
	done := make(chan struct{})

	// Background reader goroutine
	go func() {
		defer close(ch)
		buf := make([]byte, 4096)
		for {
			select {
			case <-done:
				return
			default:
			}
			n, err := p.Read(buf)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, buf[:n])
				ch <- readResult{data: cp, err: err}
			}
			if err != nil {
				return
			}
		}
	}()

	var sb strings.Builder
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case r, ok := <-ch:
			if !ok {
				close(done)
				return sb.String()
			}
			sb.Write(r.data)
		case <-timer.C:
			close(done)
			return sb.String()
		}
	}
}

// waitForOutput writes a command and waits until the output contains the
// expected substring, or the timeout expires.
func waitForOutput(p *LocalPTY, cmd, expected string, timeout time.Duration) (string, bool) {
	_, _ = p.WriteString(cmd)

	deadline := time.After(timeout)
	var sb strings.Builder
	buf := make([]byte, 4096)
	ch := make(chan struct{ n int; err error }, 1)

	for {
		// Start a non-blocking read attempt via goroutine
		go func() {
			n, err := p.Read(buf)
			ch <- struct{ n int; err error }{n, err}
		}()

		select {
		case result := <-ch:
			if result.n > 0 {
				sb.Write(buf[:result.n])
			}
			if strings.Contains(sb.String(), expected) {
				return sb.String(), true
			}
			if result.err != nil {
				return sb.String(), false
			}
		case <-deadline:
			return sb.String(), false
		}
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.Shell == "" {
		t.Error("DefaultOptions().Shell should not be empty")
	}
	if opts.Term != "dumb" {
		t.Errorf("DefaultOptions().Term = %q, want %q", opts.Term, "dumb")
	}
	if opts.Rows != 24 {
		t.Errorf("DefaultOptions().Rows = %d, want 24", opts.Rows)
	}
	if opts.Cols != 120 {
		t.Errorf("DefaultOptions().Cols = %d, want 120", opts.Cols)
	}
	if len(opts.Env) == 0 {
		t.Error("DefaultOptions().Env should not be empty")
	}
}

func TestShellEnv_Bash(t *testing.T) {
	env := ShellEnv("/bin/bash")

	found := map[string]bool{}
	for _, e := range env {
		if strings.HasPrefix(e, "NO_COLOR=") {
			found["NO_COLOR"] = true
		}
		if strings.HasPrefix(e, "PS1=") {
			found["PS1"] = true
		}
		if strings.HasPrefix(e, "PROMPT_COMMAND=") {
			found["PROMPT_COMMAND"] = true
		}
	}
	if !found["NO_COLOR"] {
		t.Error("ShellEnv(bash) should contain NO_COLOR=1")
	}
	if !found["PS1"] {
		t.Error("ShellEnv(bash) should contain PS1")
	}
	if !found["PROMPT_COMMAND"] {
		t.Error("ShellEnv(bash) should contain PROMPT_COMMAND")
	}
}

func TestShellEnv_Zsh(t *testing.T) {
	env := ShellEnv("/bin/zsh")

	found := map[string]bool{}
	for _, e := range env {
		if strings.HasPrefix(e, "PROMPT=") {
			found["PROMPT"] = true
		}
		if strings.HasPrefix(e, "RPROMPT=") {
			found["RPROMPT"] = true
		}
	}
	if !found["PROMPT"] {
		t.Error("ShellEnv(zsh) should contain PROMPT")
	}
	if !found["RPROMPT"] {
		t.Error("ShellEnv(zsh) should contain RPROMPT")
	}
}

func TestShellEnv_Fish(t *testing.T) {
	env := ShellEnv("/usr/bin/fish")

	found := false
	for _, e := range env {
		if strings.HasPrefix(e, "fish_greeting=") {
			found = true
		}
	}
	if !found {
		t.Error("ShellEnv(fish) should contain fish_greeting=")
	}
}

func TestShellEnv_PlainName(t *testing.T) {
	// Shell path with no slashes at all should fall through to default.
	env := ShellEnv("bash")

	found := false
	for _, e := range env {
		if strings.HasPrefix(e, "PS1=") {
			found = true
		}
	}
	if !found {
		t.Error("ShellEnv('bash') should contain PS1")
	}
}

func TestLocalPTY_NewAndClose(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer p.Close() //nolint:errcheck

	if p.Shell() != "/bin/sh" {
		t.Errorf("Shell() = %q, want %q", p.Shell(), "/bin/sh")
	}

	// Close should succeed
	if err := p.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestLocalPTY_DefaultsApplied(t *testing.T) {
	// Pass zero-value options so all defaults kick in.
	p, err := NewLocalPTY(PTYOptions{NoRC: true})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer p.Close() //nolint:errcheck

	if p.Shell() == "" {
		t.Error("Shell() should not be empty after default detection")
	}
}

func TestLocalPTY_WriteAndRead(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		Term:  "dumb",
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer p.Close() //nolint:errcheck

	// Wait a moment for the shell to start, then send command
	time.Sleep(200 * time.Millisecond)

	cmd := "echo PTY_TEST_OUTPUT_12345\n"
	n, err := p.Write([]byte(cmd))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(cmd) {
		t.Errorf("Write returned %d, want %d", n, len(cmd))
	}

	// Read output with timeout looking for our marker
	output, found := waitForOutput(p, "", "PTY_TEST_OUTPUT_12345", 5*time.Second)
	if !found {
		t.Errorf("Read output %q does not contain expected marker", output)
	}
}

func TestLocalPTY_WriteString(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		Term:  "dumb",
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer p.Close() //nolint:errcheck

	time.Sleep(200 * time.Millisecond)

	n, err := p.WriteString("echo WRITESTRING_MARKER_67890\n")
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if n == 0 {
		t.Error("WriteString returned 0 bytes")
	}

	output, found := waitForOutput(p, "", "WRITESTRING_MARKER_67890", 5*time.Second)
	if !found {
		t.Errorf("WriteString output %q missing expected marker", output)
	}
}

func TestLocalPTY_Resize(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		Rows:  24,
		Cols:  80,
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer p.Close() //nolint:errcheck

	// Resize should not return an error on a valid PTY
	if err := p.Resize(40, 132); err != nil {
		t.Errorf("Resize: %v", err)
	}
}

func TestLocalPTY_Signal(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer p.Close() //nolint:errcheck

	// Sending SIGWINCH is harmless and should succeed
	if err := p.Signal(syscall.SIGWINCH); err != nil {
		t.Errorf("Signal(SIGWINCH): %v", err)
	}
}

func TestLocalPTY_Interrupt(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer p.Close() //nolint:errcheck

	// Interrupt sends SIGINT to the shell process; it should not error
	if err := p.Interrupt(); err != nil {
		t.Errorf("Interrupt: %v", err)
	}
}

func TestLocalPTY_Fd(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer p.Close() //nolint:errcheck

	fd := p.Fd()
	if fd == 0 {
		t.Error("Fd() returned 0, expected a valid file descriptor")
	}
}

func TestLocalPTY_File(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer p.Close() //nolint:errcheck

	f := p.File()
	if f == nil {
		t.Fatal("File() returned nil")
	}
	// The file should have a valid name (e.g. /dev/ptmx or /dev/pts/N)
	if f.Name() == "" {
		t.Error("File().Name() is empty")
	}
}

func TestLocalPTY_ReaderWriter(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer p.Close() //nolint:errcheck

	if p.Reader() == nil {
		t.Error("Reader() returned nil")
	}
	if p.Writer() == nil {
		t.Error("Writer() returned nil")
	}
}

func TestLocalPTY_SetReadDeadline(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer p.Close() //nolint:errcheck

	// SetReadDeadline should not return an error
	if err := p.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
		t.Errorf("SetReadDeadline: %v", err)
	}

	// Clear the deadline (zero time)
	if err := p.SetReadDeadline(time.Time{}); err != nil {
		t.Errorf("SetReadDeadline(zero): %v", err)
	}
}

func TestLocalPTY_Wait(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}

	// Tell the shell to exit
	time.Sleep(200 * time.Millisecond)
	_, _ = p.WriteString("exit 0\n")
	time.Sleep(200 * time.Millisecond)

	// Close the PTY master fd to unblock the shell. On macOS, the shell may
	// block writing to the PTY if the read side isn't drained.
	_ = p.pty.Close()

	// Wait should return once the shell exits
	done := make(chan error, 1)
	go func() {
		done <- p.Wait()
	}()

	select {
	case err := <-done:
		// Wait may return nil or a "signal: killed" error; both are acceptable
		// after an orderly exit.
		_ = err
	case <-time.After(5 * time.Second):
		t.Fatal("Wait() timed out")
	}
}

func TestLocalPTY_Dir(t *testing.T) {
	// Create a uniquely-named temp dir to avoid symlink resolution issues
	// on macOS (where /tmp -> /private/var/folders/...).
	dir, err := os.MkdirTemp("", "pty-dir-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	// Resolve symlinks so we match what pwd will output
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		Dir:   dir,
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer p.Close() //nolint:errcheck

	time.Sleep(200 * time.Millisecond)

	// Print working directory and look for the resolved path in output
	output, found := waitForOutput(p, "pwd\n", resolved, 5*time.Second)
	if !found {
		t.Errorf("expected pwd output to contain %q, got %q", resolved, output)
	}
}

func TestLocalPTY_DoubleClose(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}

	// First close should succeed
	if err := p.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}

	// Second close may return an error but must not panic
	_ = p.Close()
}

func TestLocalPTY_SignalAfterExit(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}

	// Tell the shell to exit
	time.Sleep(200 * time.Millisecond)
	_, _ = p.WriteString("exit 0\n")
	time.Sleep(200 * time.Millisecond)

	// Close PTY master to unblock Wait on macOS (shell may block on PTY write)
	_ = p.pty.Close()

	// Reap the process so it is fully finished
	done := make(chan error, 1)
	go func() {
		done <- p.Wait()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait() timed out")
	}

	// Signal after process has exited and been waited on should return an error
	err = p.Signal(syscall.SIGWINCH)
	if err == nil {
		t.Error("expected Signal to fail after process exited")
	}
}

func TestNoRCFlags(t *testing.T) {
	tests := []struct {
		shell    string
		noRC     bool
		expected []string
	}{
		{"/bin/bash", false, nil},
		{"/bin/bash", true, []string{"--norc", "--noprofile"}},
		{"/bin/zsh", true, []string{"--no-rcs", "--no-globalrcs"}},
		{"/usr/bin/fish", true, []string{"--no-config"}},
		{"/bin/sh", true, nil},
		{"bash", true, []string{"--norc", "--noprofile"}},
		{"zsh", true, []string{"--no-rcs", "--no-globalrcs"}},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			got := noRCFlags(tt.shell, tt.noRC)
			if len(got) != len(tt.expected) {
				t.Fatalf("noRCFlags(%q, %v) = %v, want %v", tt.shell, tt.noRC, got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("noRCFlags(%q, %v)[%d] = %q, want %q", tt.shell, tt.noRC, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestDetectShell(t *testing.T) {
	shell := detectShell()
	if shell == "" {
		t.Fatal("detectShell() returned empty string")
	}
	// Should return a path that exists
	if _, err := os.Stat(shell); err != nil {
		t.Errorf("detectShell() returned %q which does not exist: %v", shell, err)
	}
}

func TestLocalPTY_SignalNilProcess(t *testing.T) {
	// Construct a LocalPTY with a nil-process cmd to hit the "process not started" branch.
	p := &LocalPTY{
		cmd: &exec.Cmd{}, // Process is nil since the command was never started
	}
	err := p.Signal(syscall.SIGWINCH)
	if err == nil {
		t.Error("expected error when process is nil")
	}
	if !strings.Contains(err.Error(), "process not started") {
		t.Errorf("expected 'process not started' error, got: %v", err)
	}
}

func TestNewLocalPTY_InvalidShell(t *testing.T) {
	// Attempt to start a PTY with a non-existent shell binary.
	_, err := NewLocalPTY(PTYOptions{
		Shell: "/nonexistent/shell/binary",
	})
	if err == nil {
		t.Error("expected error for non-existent shell")
	}
}

func TestLocalPTY_Env(t *testing.T) {
	p, err := NewLocalPTY(PTYOptions{
		Shell: "/bin/sh",
		Env:   []string{"MY_TEST_VAR=hello_from_test"},
		NoRC:  true,
	})
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer p.Close() //nolint:errcheck

	time.Sleep(200 * time.Millisecond)

	output, found := waitForOutput(p, "echo $MY_TEST_VAR\n", "hello_from_test", 5*time.Second)
	if !found {
		t.Errorf("expected env var in output, got %q", output)
	}
}
