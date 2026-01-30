package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

func main() {
	// Connect to SSH
	config := &ssh.ClientConfig{
		User: "ralmeida",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
				key, err := os.ReadFile(os.ExpandEnv("$HOME/.ssh/acolita_pair.pem"))
				if err != nil {
					return nil, err
				}
				signer, err := ssh.ParsePrivateKey(key)
				if err != nil {
					return nil, err
				}
				return []ssh.Signer{signer}, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", "192.168.15.200:22", config)
	if err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()
	fmt.Println("Connected!")

	// Create session
	session, err := client.NewSession()
	if err != nil {
		fmt.Printf("Failed to create session: %v\n", err)
		os.Exit(1)
	}
	defer session.Close()

	// Request PTY
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("dumb", 24, 120, modes); err != nil {
		fmt.Printf("Failed to request PTY: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("PTY allocated!")

	// Get stdin/stdout
	stdin, err := session.StdinPipe()
	if err != nil {
		fmt.Printf("Failed to get stdin: %v\n", err)
		os.Exit(1)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		fmt.Printf("Failed to get stdout: %v\n", err)
		os.Exit(1)
	}

	// Start shell
	if err := session.Shell(); err != nil {
		fmt.Printf("Failed to start shell: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Shell started!")

	// Read initial output
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if err != nil {
				if err != io.EOF {
					fmt.Printf("\n[Read error: %v]\n", err)
				}
				return
			}
			fmt.Printf("[OUT %d bytes]: %q\n", n, string(buf[:n]))
		}
	}()

	time.Sleep(500 * time.Millisecond)

	// Run sudo
	fmt.Println("\n--- Sending: sudo ls /root ---")
	stdin.Write([]byte("sudo ls /root\n"))

	time.Sleep(1 * time.Second)

	// Wait for password prompt, then send password
	fmt.Println("\n--- Waiting 100ms then sending password ---")
	time.Sleep(100 * time.Millisecond)

	password := "nolpvesc30"
	fmt.Printf("--- Sending password (%d chars) + newline ---\n", len(password))
	n, err := stdin.Write([]byte(password + "\n"))
	fmt.Printf("--- Write returned: n=%d, err=%v ---\n", n, err)

	time.Sleep(2 * time.Second)

	fmt.Println("\n--- Done ---")
}
