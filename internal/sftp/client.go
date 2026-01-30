// Package sftp provides SFTP client functionality for file transfers over SSH.
package sftp

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Client wraps an SFTP client for file transfer operations.
// It uses an existing SSH connection and can be initialized lazily.
type Client struct {
	sshConn    *ssh.Client
	sftpClient *sftp.Client
	mu         sync.Mutex
	closed     bool
}

// NewClient creates a new SFTP client wrapper using an existing SSH connection.
// The SFTP subsystem is initialized lazily on first use.
func NewClient(sshConn *ssh.Client) *Client {
	return &Client{
		sshConn: sshConn,
	}
}

// ensureConnected initializes the SFTP client if not already done.
func (c *Client) ensureConnected() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("sftp client is closed")
	}

	if c.sftpClient != nil {
		return nil
	}

	if c.sshConn == nil {
		return fmt.Errorf("ssh connection is nil")
	}

	client, err := sftp.NewClient(c.sshConn)
	if err != nil {
		return fmt.Errorf("create sftp client: %w", err)
	}

	c.sftpClient = client
	return nil
}

// Close closes the SFTP client.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.sftpClient != nil {
		err := c.sftpClient.Close()
		c.sftpClient = nil
		return err
	}
	return nil
}

// IsConnected returns true if the SFTP client is connected.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sftpClient != nil && !c.closed
}

// Stat returns file information for the given path.
func (c *Client) Stat(path string) (os.FileInfo, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.Stat(path)
}

// Lstat returns file information without following symlinks.
func (c *Client) Lstat(path string) (os.FileInfo, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.Lstat(path)
}

// ReadDir reads the contents of a directory.
func (c *Client) ReadDir(path string) ([]os.FileInfo, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.ReadDir(path)
}

// ReadLink returns the destination of a symbolic link.
func (c *Client) ReadLink(path string) (string, error) {
	if err := c.ensureConnected(); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.ReadLink(path)
}

// Mkdir creates a directory.
func (c *Client) Mkdir(path string) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.Mkdir(path)
}

// MkdirAll creates a directory and all parent directories.
func (c *Client) MkdirAll(path string) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.MkdirAll(path)
}

// Remove removes a file or empty directory.
func (c *Client) Remove(path string) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.Remove(path)
}

// Rename renames a file or directory.
func (c *Client) Rename(oldPath, newPath string) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.Rename(oldPath, newPath)
}

// Chmod changes the permissions of a file.
func (c *Client) Chmod(path string, mode os.FileMode) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.Chmod(path, mode)
}

// Chtimes changes the access and modification times of a file.
func (c *Client) Chtimes(path string, atime, mtime time.Time) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.Chtimes(path, atime, mtime)
}

// Symlink creates a symbolic link.
func (c *Client) Symlink(oldPath, newPath string) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.Symlink(oldPath, newPath)
}

// Open opens a file for reading.
func (c *Client) Open(path string) (*sftp.File, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.Open(path)
}

// Create creates or truncates a file for writing.
func (c *Client) Create(path string) (*sftp.File, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.Create(path)
}

// OpenFile opens a file with the specified flags and mode.
func (c *Client) OpenFile(path string, flags int) (*sftp.File, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.OpenFile(path, flags)
}

// ReadFile reads the entire contents of a file.
func (c *Client) ReadFile(path string) ([]byte, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	client := c.sftpClient
	c.mu.Unlock()

	file, err := client.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	return io.ReadAll(file)
}

// WriteFile writes data to a file, creating it if necessary.
func (c *Client) WriteFile(path string, data []byte, perm os.FileMode) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	client := c.sftpClient
	c.mu.Unlock()

	file, err := client.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	if perm != 0 {
		if err := file.Chmod(perm); err != nil {
			return fmt.Errorf("chmod file: %w", err)
		}
	}

	return nil
}

// GetFile downloads a file and returns its contents.
// For large files, use GetFileStream instead.
func (c *Client) GetFile(remotePath string) ([]byte, os.FileInfo, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, nil, err
	}

	c.mu.Lock()
	client := c.sftpClient
	c.mu.Unlock()

	file, err := client.Open(remotePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open remote file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("stat remote file: %w", err)
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, fmt.Errorf("read remote file: %w", err)
	}

	return data, info, nil
}

// PutFile uploads data to a remote file.
// For large files, use PutFileStream instead.
func (c *Client) PutFile(remotePath string, data []byte, perm os.FileMode) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	client := c.sftpClient
	c.mu.Unlock()

	file, err := client.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote file: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write remote file: %w", err)
	}

	if perm != 0 {
		if err := file.Chmod(perm); err != nil {
			return fmt.Errorf("chmod remote file: %w", err)
		}
	}

	return nil
}

// GetFileStream opens a remote file for streaming reads.
// Caller is responsible for closing the returned file.
func (c *Client) GetFileStream(remotePath string) (*sftp.File, os.FileInfo, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, nil, err
	}

	c.mu.Lock()
	client := c.sftpClient
	c.mu.Unlock()

	file, err := client.Open(remotePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open remote file: %w", err)
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, fmt.Errorf("stat remote file: %w", err)
	}

	return file, info, nil
}

// PutFileStream creates a remote file for streaming writes.
// Caller is responsible for closing the returned file.
func (c *Client) PutFileStream(remotePath string) (*sftp.File, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	client := c.sftpClient
	c.mu.Unlock()

	file, err := client.Create(remotePath)
	if err != nil {
		return nil, fmt.Errorf("create remote file: %w", err)
	}

	return file, nil
}

// Getwd returns the current working directory on the remote server.
func (c *Client) Getwd() (string, error) {
	if err := c.ensureConnected(); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.Getwd()
}

// RealPath returns the real path of a file (resolves symlinks and relative paths).
func (c *Client) RealPath(path string) (string, error) {
	if err := c.ensureConnected(); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sftpClient.RealPath(path)
}

// FileInfo contains metadata about a remote file.
type FileInfo struct {
	Name    string      `json:"name"`
	Size    int64       `json:"size"`
	Mode    os.FileMode `json:"mode"`
	ModTime int64       `json:"mod_time"` // Unix timestamp
	IsDir   bool        `json:"is_dir"`
	IsLink  bool        `json:"is_link"`
}

// ToFileInfo converts os.FileInfo to our FileInfo struct.
func ToFileInfo(info os.FileInfo) FileInfo {
	return FileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime().Unix(),
		IsDir:   info.IsDir(),
		IsLink:  info.Mode()&os.ModeSymlink != 0,
	}
}
