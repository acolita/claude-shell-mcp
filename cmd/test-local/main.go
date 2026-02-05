// A simple test program for local PTY sessions.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/session"
)

const errExecCmdFmt = "Error executing command: %v\n"

func main() {
	cfg := config.DefaultConfig()
	mgr := session.NewManager(cfg)

	fmt.Println("Creating local session...")
	sess, err := mgr.Create(session.CreateOptions{Mode: "local"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating session: %v\n", err)
		os.Exit(1)
	}
	defer mgr.Close(sess.ID)

	fmt.Printf("Session created: %s (shell: %s)\n", sess.ID, sess.Shell)

	// Test echo command
	fmt.Println("\nExecuting: echo 'hello world'")
	result, err := sess.Exec("echo 'hello world'", 5000)
	if err != nil {
		fmt.Fprintf(os.Stderr, errExecCmdFmt, err)
		os.Exit(1)
	}

	printResult(result)

	// Test pwd
	fmt.Println("\nExecuting: pwd")
	result, err = sess.Exec("pwd", 5000)
	if err != nil {
		fmt.Fprintf(os.Stderr, errExecCmdFmt, err)
		os.Exit(1)
	}

	printResult(result)

	// Test cd
	fmt.Println("\nExecuting: cd /tmp")
	result, err = sess.Exec("cd /tmp", 5000)
	if err != nil {
		fmt.Fprintf(os.Stderr, errExecCmdFmt, err)
		os.Exit(1)
	}
	printResult(result)

	// Test pwd after cd
	fmt.Println("\nExecuting: pwd (should be /tmp)")
	result, err = sess.Exec("pwd", 5000)
	if err != nil {
		fmt.Fprintf(os.Stderr, errExecCmdFmt, err)
		os.Exit(1)
	}
	printResult(result)

	// Test ls (just list a few files)
	fmt.Println("\nExecuting: ls -1 | head -3")
	result, err = sess.Exec("ls -1 | head -3", 5000)
	if err != nil {
		fmt.Fprintf(os.Stderr, errExecCmdFmt, err)
		os.Exit(1)
	}

	printResult(result)

	// Test env var
	fmt.Println("\nExecuting: export FOO=bar")
	result, err = sess.Exec("export FOO=bar", 5000)
	if err != nil {
		fmt.Fprintf(os.Stderr, errExecCmdFmt, err)
		os.Exit(1)
	}
	printResult(result)

	fmt.Println("\nExecuting: echo $FOO")
	result, err = sess.Exec("echo $FOO", 5000)
	if err != nil {
		fmt.Fprintf(os.Stderr, errExecCmdFmt, err)
		os.Exit(1)
	}
	printResult(result)

	// Test status
	fmt.Println("\nSession status:")
	status := sess.Status()
	data, _ := json.MarshalIndent(status, "", "  ")
	fmt.Println(string(data))

	fmt.Println("\nAll tests passed!")
}

func printResult(r *session.ExecResult) {
	data, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(data))
}
