package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

func fatal(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
	os.Exit(1)
}

func loadSigners() ([]ssh.Signer, error) {
	key, err := os.ReadFile(os.ExpandEnv("$HOME/.ssh/acolita_pair.pem"))
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}
	return []ssh.Signer{signer}, nil
}

func connectSSH(host, user string) *ssh.Client {
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeysCallback(loadSigners)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", host, config)
	if err != nil {
		fatal("Failed to connect: %v", err)
	}
	fmt.Println("Connected!")
	return client
}

func setupSession(client *ssh.Client) (*ssh.Session, io.WriteCloser, io.Reader) {
	session, err := client.NewSession()
	if err != nil {
		fatal("Failed to create session: %v", err)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("dumb", 24, 120, modes); err != nil {
		fatal("Failed to request PTY: %v", err)
	}
	fmt.Println("PTY allocated!")

	stdin, err := session.StdinPipe()
	if err != nil {
		fatal("Failed to get stdin: %v", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		fatal("Failed to get stdout: %v", err)
	}

	if err := session.Shell(); err != nil {
		fatal("Failed to start shell: %v", err)
	}
	fmt.Println("Shell started!")

	return session, stdin, stdout
}

func startOutputReader(stdout io.Reader) {
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
}

func main() {
	client := connectSSH("192.168.15.200:22", "ralmeida")
	defer client.Close()

	session, stdin, stdout := setupSession(client)
	defer session.Close()

	startOutputReader(stdout)
	time.Sleep(500 * time.Millisecond)

	fmt.Println("\n--- Sending: sudo ls /root ---")
	stdin.Write([]byte("sudo ls /root\n"))
	time.Sleep(1 * time.Second)

	fmt.Println("\n--- Waiting 100ms then sending password ---")
	time.Sleep(100 * time.Millisecond)

	password := "nolpvesc30"
	fmt.Printf("--- Sending password (%d chars) + newline ---\n", len(password))
	n, err := stdin.Write([]byte(password + "\n"))
	fmt.Printf("--- Write returned: n=%d, err=%v ---\n", n, err)

	time.Sleep(2 * time.Second)
	fmt.Println("\n--- Done ---")
}
