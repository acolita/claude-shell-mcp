package ports

import (
	"io/fs"
	"time"
)

// FileSystem abstracts file operations for testing.
type FileSystem interface {
	// ReadFile reads the named file and returns its contents.
	ReadFile(name string) ([]byte, error)

	// WriteFile writes data to the named file, creating it if necessary.
	WriteFile(name string, data []byte, perm fs.FileMode) error

	// Stat returns file info for the named file.
	Stat(name string) (fs.FileInfo, error)

	// MkdirAll creates a directory and all parent directories.
	MkdirAll(path string, perm fs.FileMode) error

	// Remove removes the named file or empty directory.
	Remove(name string) error

	// Rename renames (moves) oldpath to newpath.
	Rename(oldpath, newpath string) error

	// Chtimes changes the access and modification times of the named file.
	Chtimes(name string, atime, mtime time.Time) error

	// UserHomeDir returns the current user's home directory.
	UserHomeDir() (string, error)

	// Getenv retrieves the value of the environment variable named by the key.
	Getenv(key string) string
}
