// Simple PTY test
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/pty"
)

func main() {
	fmt.Println("Creating local PTY...")

	opts := pty.DefaultOptions()
	fmt.Printf("Options: shell=%s, term=%s\n", opts.Shell, opts.Term)

	localPTY, err := pty.NewLocalPTY(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating PTY: %v\n", err)
		os.Exit(1)
	}
	defer localPTY.Close()

	fmt.Printf("PTY created, shell: %s\n", localPTY.Shell())

	// Wait a bit for shell startup
	time.Sleep(200 * time.Millisecond)

	// Drain initial output
	fmt.Println("Draining initial output...")
	buf := make([]byte, 4096)
	localPTY.File().SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	n, _ := localPTY.Read(buf)
	if n > 0 {
		fmt.Printf("Initial output (%d bytes): %q\n", n, string(buf[:n]))
	}

	// Send a simple command
	fmt.Println("\nSending: echo test")
	localPTY.WriteString("echo test\n")

	// Read response with timeout
	time.Sleep(100 * time.Millisecond)
	localPTY.File().SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err = localPTY.Read(buf)
	if err != nil {
		fmt.Printf("Read error: %v\n", err)
	}
	if n > 0 {
		fmt.Printf("Output (%d bytes): %q\n", n, string(buf[:n]))
	}

	fmt.Println("\nPTY test complete!")
}
