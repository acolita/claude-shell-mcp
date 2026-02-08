package realdialog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/acolita/claude-shell-mcp/internal/ports"
)

// RunFormHelper is the entry point for the --form helper subprocess.
// It runs in its own terminal window, reads encrypted prefill from the temp file,
// shows the form via simple print/scan, encrypts the result back, and writes a done marker.
func RunFormHelper() error {
	formFile := os.Getenv(envFormFile)
	formKey := os.Getenv(envFormKey)

	if formFile == "" || formKey == "" {
		return fmt.Errorf("missing %s or %s", envFormFile, envFormKey)
	}

	// Read and decrypt prefill
	encData, err := os.ReadFile(formFile)
	if err != nil {
		return fmt.Errorf("read form file: %w", err)
	}

	decData, err := decrypt(encData, formKey)
	if err != nil {
		return fmt.Errorf("decrypt form data: %w", err)
	}

	var prefill ports.ServerFormData
	if err := json.Unmarshal(decData, &prefill); err != nil {
		wipe(decData)
		return fmt.Errorf("unmarshal form data: %w", err)
	}
	wipe(decData)

	// Clear screen and show header
	fmt.Print("\033[2J\033[H")
	fmt.Println("\n  claude-shell-mcp: Add Server Configuration")

	// Run the form — stdin/stdout are the terminal (we have our own window)
	result, err := runForm(prefill)
	if err != nil {
		return fmt.Errorf("form: %w", err)
	}

	// Encrypt result and write back
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	encResult, err := encrypt(resultJSON, formKey)
	wipe(resultJSON)
	if err != nil {
		return fmt.Errorf("encrypt result: %w", err)
	}

	if err := os.WriteFile(formFile, encResult, 0600); err != nil {
		return fmt.Errorf("write result: %w", err)
	}

	// Signal done — MCP handler polls for this file
	donePath := formFile + ".done"
	if err := os.WriteFile(donePath, []byte("ok"), 0600); err != nil {
		return fmt.Errorf("write done marker: %w", err)
	}

	return nil
}

func runForm(prefill ports.ServerFormData) (ports.ServerFormData, error) {
	scanner := bufio.NewScanner(os.Stdin)
	result := prefill

	if result.Port == 0 {
		result.Port = 22
	}
	if result.AuthType == "" {
		result.AuthType = "key"
	}

	fmt.Println("\n  Connection Details")
	fmt.Println("  ─────────────────")

	result.Name = prompt(scanner, "  Server Name", result.Name)
	result.Host = prompt(scanner, "  Host", result.Host)

	portStr := prompt(scanner, "  Port", strconv.Itoa(result.Port))
	if p, err := strconv.Atoi(portStr); err == nil {
		result.Port = p
	}

	result.User = prompt(scanner, "  User", result.User)

	fmt.Println("\n  Authentication")
	fmt.Println("  ──────────────")

	result.KeyPath = prompt(scanner, "  SSH Key Path (empty for ssh-agent)", result.KeyPath)

	authType := prompt(scanner, "  Auth Type (key/password)", result.AuthType)
	if authType == "key" || authType == "password" {
		result.AuthType = authType
	}

	result.SudoPasswordEnv = prompt(scanner, "  Sudo Password Env Var (optional)", result.SudoPasswordEnv)

	fmt.Println("\n  ─────────────────")
	fmt.Printf("  Server: %s@%s:%d\n", result.User, result.Host, result.Port)
	fmt.Printf("  Auth:   %s\n", result.AuthType)

	confirm := prompt(scanner, "\n  Save? (y/n)", "y")
	result.Confirmed = strings.EqualFold(confirm, "y") || strings.EqualFold(confirm, "yes")

	return result, nil
}

// prompt shows a field with a default value and reads user input.
// Returns the default if the user presses Enter without typing.
func prompt(scanner *bufio.Scanner, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}

	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			return input
		}
	}
	return defaultVal
}
