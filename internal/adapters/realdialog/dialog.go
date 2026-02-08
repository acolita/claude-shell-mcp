// Package realdialog provides a TUI-based DialogProvider using charmbracelet/huh.
//
// Since the MCP server runs as a child of Claude Code (a TUI app), we can't directly
// take over the terminal — both processes would fight for /dev/tty. Instead, we:
//  1. Encrypt prefill data to a temp file (AES-256-GCM)
//  2. Create a wrapper shell script with env vars pointing to the encrypted file
//  3. Launch a new terminal window (Terminal.app on macOS, detected emulator on Linux)
//  4. Helper runs the TUI form in its own terminal, encrypts result, writes done marker
//  5. MCP handler polls for done marker, reads and decrypts the result
//  6. MCP handler closes the terminal window by its captured ID
//
// The encryption key is passed via the wrapper script (0700 perms, self-deleting).
// The temp file has 0600 permissions and is deleted after reading.
package realdialog

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/ports"
)

const (
	envFormFile = "CLAUDE_SHELL_FORM_FILE"
	envFormKey  = "CLAUDE_SHELL_FORM_KEY"
)

// Provider implements ports.DialogProvider by launching a TUI form in a separate terminal.
type Provider struct{}

// New returns a new TUI dialog provider.
func New() *Provider {
	return &Provider{}
}

// ServerConfigForm launches a TUI form in a new terminal window.
func (p *Provider) ServerConfigForm(prefill ports.ServerFormData) (ports.ServerFormData, error) {
	key, err := generateKey()
	if err != nil {
		return prefill, fmt.Errorf("generate key: %w", err)
	}
	defer wipe([]byte(key))

	tmpPath, err := writeEncryptedPrefill(prefill, key)
	if err != nil {
		return prefill, err
	}
	defer os.Remove(tmpPath)

	donePath := tmpPath + ".done"
	defer os.Remove(donePath)

	wrapperPath, err := writeWrapperScript(tmpPath, key)
	if err != nil {
		return prefill, err
	}
	defer os.Remove(wrapperPath)

	closeTerminal, err := launchTerminal(wrapperPath)
	if err != nil {
		return prefill, fmt.Errorf("launch terminal: %w", err)
	}

	if err := waitForDone(donePath); err != nil {
		return prefill, err
	}

	if closeTerminal != nil {
		closeTerminal()
	}

	return readEncryptedResult(tmpPath, key)
}

// writeEncryptedPrefill marshals and encrypts prefill data to a temp file.
func writeEncryptedPrefill(prefill ports.ServerFormData, key string) (string, error) {
	prefillJSON, err := json.Marshal(prefill)
	if err != nil {
		return "", fmt.Errorf("marshal prefill: %w", err)
	}

	encrypted, err := encrypt(prefillJSON, key)
	wipe(prefillJSON)
	if err != nil {
		return "", fmt.Errorf("encrypt prefill: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "claude-shell-form-*.enc")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if err := os.Chmod(tmpPath, 0600); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmpFile.Write(encrypted); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	return tmpPath, nil
}

// writeWrapperScript creates a self-deleting shell script that launches the form helper.
func writeWrapperScript(tmpPath, key string) (string, error) {
	selfPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find executable: %w", err)
	}

	content := fmt.Sprintf("#!/bin/sh\nrm -f \"$0\"\nexport %s='%s'\nexport %s='%s'\nexec '%s' --form\n",
		envFormFile, tmpPath,
		envFormKey, key,
		selfPath,
	)

	f, err := os.CreateTemp("", "claude-shell-wrapper-*.sh")
	if err != nil {
		return "", fmt.Errorf("create wrapper: %w", err)
	}
	wrapperPath := f.Name()

	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return "", fmt.Errorf("write wrapper: %w", err)
	}
	f.Close()

	if err := os.Chmod(wrapperPath, 0700); err != nil {
		return "", fmt.Errorf("chmod wrapper: %w", err)
	}

	return wrapperPath, nil
}

// waitForDone polls for the done marker file, returning nil on success or an error.
func waitForDone(donePath string) error {
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("form timed out after 5 minutes")
		case <-ticker.C:
			data, err := os.ReadFile(donePath)
			if err != nil {
				continue
			}
			if string(data) != "ok" {
				return fmt.Errorf("form helper: %s", string(data))
			}
			return nil
		}
	}
}

// readEncryptedResult reads and decrypts the form result from the temp file.
func readEncryptedResult(tmpPath, key string) (ports.ServerFormData, error) {
	var zero ports.ServerFormData

	encResult, err := os.ReadFile(tmpPath)
	if err != nil {
		return zero, fmt.Errorf("read result: %w", err)
	}

	decResult, err := decrypt(encResult, key)
	if err != nil {
		return zero, fmt.Errorf("decrypt result: %w", err)
	}
	defer wipe(decResult)

	var result ports.ServerFormData
	if err := json.Unmarshal(decResult, &result); err != nil {
		return zero, fmt.Errorf("unmarshal result: %w", err)
	}

	return result, nil
}

// launchTerminal opens a new terminal window running the given script.
// Returns a cleanup function to close the window (nil if not needed).
func launchTerminal(scriptPath string) (cleanup func(), err error) {
	switch runtime.GOOS {
	case "darwin":
		return launchTerminalDarwin(scriptPath)
	case "linux":
		return launchTerminalLinux(scriptPath)
	default:
		return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// launchTerminalDarwin opens a new Terminal.app window via osascript.
// Returns a cleanup function that closes the window by its ID.
func launchTerminalDarwin(scriptPath string) (func(), error) {
	appleScript := fmt.Sprintf(`tell application "Terminal"
	activate
	do script "%s"
	return id of front window
end tell`, scriptPath)

	out, err := exec.Command("osascript", "-e", appleScript).Output()
	if err != nil {
		return nil, err
	}

	windowID := strings.TrimSpace(string(out))

	cleanup := func() {
		// Small delay to let the wrapper script fully exit so the window has
		// no running processes — this avoids the "close?" confirmation dialog.
		time.Sleep(500 * time.Millisecond)
		closeScript := fmt.Sprintf(`tell application "Terminal"
	close (every window whose id is %s)
end tell`, windowID)
		exec.Command("osascript", "-e", closeScript).Run()
	}

	return cleanup, nil
}

// launchTerminalLinux tries common terminal emulators in order of preference.
// On Linux, terminal emulators typically close when the shell exits.
func launchTerminalLinux(scriptPath string) (func(), error) {
	terminals := []struct {
		name string
		args []string
	}{
		{"x-terminal-emulator", []string{"-e", scriptPath}},
		{"gnome-terminal", []string{"--", scriptPath}},
		{"konsole", []string{"-e", scriptPath}},
		{"xfce4-terminal", []string{"-e", scriptPath}},
		{"xterm", []string{"-e", scriptPath}},
	}

	for _, t := range terminals {
		binPath, err := exec.LookPath(t.name)
		if err != nil {
			continue
		}
		cmd := exec.Command(binPath, t.args...)
		if err := cmd.Start(); err != nil {
			continue
		}
		return nil, nil
	}

	return nil, fmt.Errorf("no terminal emulator found; tried: x-terminal-emulator, gnome-terminal, konsole, xfce4-terminal, xterm")
}
