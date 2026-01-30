// Package mockssh provides a mock SSH server for testing.
package mockssh

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)

// Server is a mock SSH server for testing.
type Server struct {
	listener   net.Listener
	config     *ssh.ServerConfig
	addr       string
	shell      string
	users      map[string]string // username -> password
	mu         sync.RWMutex
	done       chan struct{}
	wg         sync.WaitGroup
	sessions   []*session
	sessionsMu sync.Mutex
}

type session struct {
	channel ssh.Channel
	pty     *os.File
	cmd     *exec.Cmd
}

// Option configures the mock SSH server.
type Option func(*Server)

// WithShell sets the shell to use for exec requests.
func WithShell(shell string) Option {
	return func(s *Server) {
		s.shell = shell
	}
}

// WithUser adds a user/password pair for authentication.
func WithUser(username, password string) Option {
	return func(s *Server) {
		s.users[username] = password
	}
}

// New creates a new mock SSH server.
func New(opts ...Option) (*Server, error) {
	// Generate a temporary host key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate host key: %w", err)
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer: %w", err)
	}

	s := &Server{
		shell: "/bin/sh",
		users: map[string]string{
			"test": "test", // Default test user
		},
		done: make(chan struct{}),
	}

	for _, opt := range opts {
		opt(s)
	}

	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			s.mu.RLock()
			expectedPass, ok := s.users[c.User()]
			s.mu.RUnlock()

			if ok && string(password) == expectedPass {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
	}
	config.AddHostKey(signer)
	s.config = config

	// Start listening on a random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}
	s.listener = listener
	s.addr = listener.Addr().String()

	// Start accepting connections
	s.wg.Add(1)
	go s.acceptLoop()

	slog.Debug("mock SSH server started", slog.String("addr", s.addr))
	return s, nil
}

// Addr returns the address the server is listening on.
func (s *Server) Addr() string {
	return s.addr
}

// Host returns the host part of the address.
func (s *Server) Host() string {
	host, _, _ := net.SplitHostPort(s.addr)
	return host
}

// Port returns the port the server is listening on.
func (s *Server) Port() string {
	_, port, _ := net.SplitHostPort(s.addr)
	return port
}

// Close shuts down the mock SSH server.
func (s *Server) Close() error {
	close(s.done)
	err := s.listener.Close()

	// Close all active sessions
	s.sessionsMu.Lock()
	for _, sess := range s.sessions {
		if sess.pty != nil {
			sess.pty.Close()
		}
		if sess.cmd != nil && sess.cmd.Process != nil {
			sess.cmd.Process.Kill()
		}
		if sess.channel != nil {
			sess.channel.Close()
		}
	}
	s.sessions = nil
	s.sessionsMu.Unlock()

	s.wg.Wait()
	return err
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				slog.Debug("accept error", slog.String("error", err.Error()))
				continue
			}
		}

		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(netConn net.Conn) {
	defer s.wg.Done()
	defer netConn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(netConn, s.config)
	if err != nil {
		slog.Debug("SSH handshake failed", slog.String("error", err.Error()))
		return
	}
	defer sshConn.Close()

	// Discard global requests
	go ssh.DiscardRequests(reqs)

	// Handle channels
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			slog.Debug("channel accept failed", slog.String("error", err.Error()))
			continue
		}

		s.wg.Add(1)
		go s.handleChannel(channel, requests)
	}
}

func (s *Server) handleChannel(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer s.wg.Done()
	defer channel.Close()

	sess := &session{channel: channel}
	s.sessionsMu.Lock()
	s.sessions = append(s.sessions, sess)
	s.sessionsMu.Unlock()

	var ptyReq *ptyRequest

	for req := range requests {
		switch req.Type {
		case "pty-req":
			ptyReq = parsePtyRequest(req.Payload)
			if req.WantReply {
				req.Reply(true, nil)
			}

		case "shell":
			if ptyReq != nil {
				s.handleShell(sess, ptyReq)
			}
			if req.WantReply {
				req.Reply(true, nil)
			}

		case "exec":
			cmd := parseExecRequest(req.Payload)
			s.handleExec(sess, cmd, ptyReq)
			if req.WantReply {
				req.Reply(true, nil)
			}

		case "window-change":
			if sess.pty != nil {
				winReq := parseWindowChangeRequest(req.Payload)
				setWinsize(sess.pty, winReq.Width, winReq.Height)
			}
			if req.WantReply {
				req.Reply(true, nil)
			}

		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

func (s *Server) handleShell(sess *session, ptyReq *ptyRequest) {
	s.runCommand(sess, s.shell, ptyReq)
}

func (s *Server) handleExec(sess *session, command string, ptyReq *ptyRequest) {
	s.runCommand(sess, s.shell, ptyReq, "-c", command)
}

func (s *Server) runCommand(sess *session, name string, ptyReq *ptyRequest, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()

	if ptyReq != nil {
		// Use PTY
		ptmx, err := pty.Start(cmd)
		if err != nil {
			slog.Debug("pty start failed", slog.String("error", err.Error()))
			sendExitStatus(sess.channel, 1)
			return
		}
		sess.pty = ptmx
		sess.cmd = cmd

		// Set initial size
		setWinsize(ptmx, ptyReq.Width, ptyReq.Height)

		// Copy data between channel and PTY
		done := make(chan struct{})
		go func() {
			io.Copy(sess.channel, ptmx)
			close(done)
		}()
		go func() {
			io.Copy(ptmx, sess.channel)
		}()

		// Wait for command to complete
		exitCode := 0
		if err := cmd.Wait(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}

		ptmx.Close()
		<-done // Wait for output to be flushed

		sendExitStatus(sess.channel, exitCode)
	} else {
		// No PTY - capture output directly
		output, err := cmd.CombinedOutput()
		sess.cmd = cmd

		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}

		// Write output to channel
		sess.channel.Write(output)

		sendExitStatus(sess.channel, exitCode)
	}
}

func sendExitStatus(channel ssh.Channel, code int) {
	// Close writes first to signal EOF on our output
	channel.CloseWrite()

	// Then send exit status
	payload := make([]byte, 4)
	payload[0] = byte(code >> 24)
	payload[1] = byte(code >> 16)
	payload[2] = byte(code >> 8)
	payload[3] = byte(code)
	channel.SendRequest("exit-status", false, payload)

	// Finally close the channel
	channel.Close()
}

type ptyRequest struct {
	Term   string
	Width  uint32
	Height uint32
}

func parsePtyRequest(payload []byte) *ptyRequest {
	if len(payload) < 4 {
		return &ptyRequest{Term: "xterm", Width: 80, Height: 24}
	}

	termLen := int(payload[3])
	if len(payload) < 4+termLen+8 {
		return &ptyRequest{Term: "xterm", Width: 80, Height: 24}
	}

	term := string(payload[4 : 4+termLen])
	width := uint32(payload[4+termLen])<<24 | uint32(payload[5+termLen])<<16 | uint32(payload[6+termLen])<<8 | uint32(payload[7+termLen])
	height := uint32(payload[8+termLen])<<24 | uint32(payload[9+termLen])<<16 | uint32(payload[10+termLen])<<8 | uint32(payload[11+termLen])

	return &ptyRequest{
		Term:   term,
		Width:  width,
		Height: height,
	}
}

type windowChangeRequest struct {
	Width  uint32
	Height uint32
}

func parseWindowChangeRequest(payload []byte) *windowChangeRequest {
	if len(payload) < 8 {
		return &windowChangeRequest{Width: 80, Height: 24}
	}
	width := uint32(payload[0])<<24 | uint32(payload[1])<<16 | uint32(payload[2])<<8 | uint32(payload[3])
	height := uint32(payload[4])<<24 | uint32(payload[5])<<16 | uint32(payload[6])<<8 | uint32(payload[7])
	return &windowChangeRequest{Width: width, Height: height}
}

func parseExecRequest(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	cmdLen := int(payload[0])<<24 | int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3])
	if len(payload) < 4+cmdLen {
		return ""
	}
	return string(payload[4 : 4+cmdLen])
}

func setWinsize(f *os.File, width, height uint32) {
	ws := struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}{
		Row: uint16(height),
		Col: uint16(width),
	}
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(&ws)))
}
