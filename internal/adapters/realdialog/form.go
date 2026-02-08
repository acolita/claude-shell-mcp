package realdialog

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/acolita/claude-shell-mcp/internal/ports"
	"github.com/charmbracelet/huh"
)

// RunFormHelper is the entry point for the --form helper subprocess.
// It runs in its own terminal window, reads encrypted prefill from the temp file,
// shows the TUI form, encrypts the result back, and writes a done marker.
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
	result := prefill
	portStr := strconv.Itoa(prefill.Port)
	if portStr == "0" {
		portStr = "22"
	}

	var confirmed bool

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Server Name").
				Description("Short name for this server (e.g., 'production', 's1')").
				Value(&result.Name),

			huh.NewInput().
				Title("Host").
				Description("SSH hostname or IP address").
				Value(&result.Host),

			huh.NewInput().
				Title("Port").
				Description("SSH port").
				Value(&portStr),

			huh.NewInput().
				Title("User").
				Description("SSH username").
				Value(&result.User),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("SSH Key Path").
				Description("Path to SSH private key (leave empty for ssh-agent)").
				Value(&result.KeyPath),

			huh.NewSelect[string]().
				Title("Auth Type").
				Options(
					huh.NewOption("Key-based", "key"),
					huh.NewOption("Password-based", "password"),
				).
				Value(&result.AuthType),

			huh.NewInput().
				Title("Sudo Password Env Var").
				Description("Environment variable containing sudo password (optional)").
				Value(&result.SudoPasswordEnv),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Save this server configuration?").
				Value(&confirmed),
		),
	)

	if err := form.Run(); err != nil {
		return prefill, err
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 22
	}
	result.Port = port
	result.Confirmed = confirmed

	return result, nil
}
