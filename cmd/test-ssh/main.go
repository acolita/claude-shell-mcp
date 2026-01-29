// Simple SSH test program
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/session"
)

func main() {
	// Get credentials from args or env
	host := getEnvOrArg("SSH_HOST", 1, "192.168.15.200")
	user := getEnvOrArg("SSH_USER", 2, "ralmeida")
	password := getEnvOrArg("SSH_PASSWORD", 3, "")

	fmt.Printf("Connecting to %s@%s...\n", user, host)

	cfg := config.DefaultConfig()
	mgr := session.NewManager(cfg)

	// Create SSH session
	sess, err := mgr.Create(session.CreateOptions{
		Mode:     "ssh",
		Host:     host,
		Port:     22,
		User:     user,
		Password: password,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating session: %v\n", err)
		os.Exit(1)
	}
	defer mgr.Close(sess.ID)

	fmt.Printf("Session created: %s\n", sess.ID)

	// Test basic commands
	commands := []string{
		"hostname",
		"whoami",
		"pwd",
		"uname -a",
	}

	for _, cmd := range commands {
		fmt.Printf("\nExecuting: %s\n", cmd)
		result, err := sess.Exec(cmd, 10000)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}
		printResult(result)
	}

	// Test state persistence
	fmt.Println("\n--- Testing state persistence ---")

	fmt.Println("\nExecuting: cd /tmp")
	result, _ := sess.Exec("cd /tmp", 5000)
	printResult(result)

	fmt.Println("\nExecuting: pwd")
	result, _ = sess.Exec("pwd", 5000)
	printResult(result)

	fmt.Println("\nExecuting: export TEST_VAR=hello_ssh")
	result, _ = sess.Exec("export TEST_VAR=hello_ssh", 5000)
	printResult(result)

	fmt.Println("\nExecuting: echo $TEST_VAR")
	result, _ = sess.Exec("echo $TEST_VAR", 5000)
	printResult(result)

	fmt.Println("\n--- Session status ---")
	status := sess.Status()
	data, _ := json.MarshalIndent(status, "", "  ")
	fmt.Println(string(data))

	fmt.Println("\n--- Environment capture ---")
	envVars := sess.CaptureEnv()
	fmt.Printf("Captured %d environment variables\n", len(envVars))
	// Print a few key ones
	keyVars := []string{"HOME", "USER", "SHELL", "PATH", "LANG", "TEST_VAR"}
	for _, k := range keyVars {
		if v, ok := envVars[k]; ok {
			if len(v) > 60 {
				v = v[:60] + "..."
			}
			fmt.Printf("  %s=%s\n", k, v)
		}
	}

	fmt.Println("\nSSH test complete!")
}

func printResult(r *session.ExecResult) {
	data, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(data))
}

func getEnvOrArg(envKey string, argIdx int, defaultVal string) string {
	if val := os.Getenv(envKey); val != "" {
		return val
	}
	if len(os.Args) > argIdx {
		return os.Args[argIdx]
	}
	return defaultVal
}
