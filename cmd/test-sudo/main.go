// Test sudo password caching
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/security"
	"github.com/acolita/claude-shell-mcp/internal/session"
)

func main() {
	host := getEnvOrArg("SSH_HOST", 1, "192.168.15.200")
	user := getEnvOrArg("SSH_USER", 2, "ralmeida")
	password := getEnvOrArg("SSH_PASSWORD", 3, "")
	sudoPassword := getEnvOrArg("SUDO_PASSWORD", 4, password) // Default to same as SSH password

	fmt.Printf("Connecting to %s@%s...\n", user, host)

	cfg := config.DefaultConfig()
	mgr := session.NewManager(cfg)
	sudoCache := security.NewSudoCache(5 * time.Minute)

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

	fmt.Printf("Session created: %s\n\n", sess.ID)

	// Test 1: Run sudo command - should prompt for password
	fmt.Println("=== Test 1: sudo whoami (should prompt for password) ===")
	result, err := sess.Exec("sudo whoami", 10000)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	printResult(result)

	// If we got a password prompt, provide the password
	if result.Status == "awaiting_input" && result.PromptType == "password" {
		fmt.Println("\n=== Detected password prompt, providing password and caching ===")

		// Cache the password
		sudoCache.Set(sess.ID, []byte(sudoPassword))

		// Provide the password
		result, err = sess.ProvideInput(sudoPassword)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		printResult(result)
	}

	// Test 2: Run another sudo command - should use cached password (on server side)
	fmt.Println("\n=== Test 2: sudo id (should use sudo's cached credentials) ===")
	result, err = sess.Exec("sudo id", 10000)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// If prompted again, use our cache
	if result.Status == "awaiting_input" && result.PromptType == "password" {
		fmt.Println("Prompted for password again, using cached password...")
		if cachedPwd := sudoCache.Get(sess.ID); cachedPwd != nil {
			result, err = sess.ProvideInput(string(cachedPwd))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}
	}
	printResult(result)

	// Test 3: Run non-sudo command
	fmt.Println("\n=== Test 3: Regular command (whoami) ===")
	result, err = sess.Exec("whoami", 5000)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	printResult(result)

	// Show cache status
	fmt.Println("\n=== Sudo cache status ===")
	fmt.Printf("  Cached: %v\n", sudoCache.IsValid(sess.ID))
	fmt.Printf("  Expires in: %v\n", sudoCache.ExpiresIn(sess.ID))

	fmt.Println("\nSudo test complete!")
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
