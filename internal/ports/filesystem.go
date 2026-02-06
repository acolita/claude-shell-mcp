package ports

import (
	"io"
	"io/fs"
	"time"
)

// FileHandle abstracts file operations on an open file.
type FileHandle interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
	ReadAt(b []byte, off int64) (int, error)
	WriteAt(b []byte, off int64) (int, error)
	Truncate(size int64) error
	Name() string
}

// FileSystem abstracts file operations for testing.
type FileSystem interface {
	// ReadFile reads the named file and returns its contents.
	ReadFile(name string) ([]byte, error)

	// WriteFile writes data to the named file, creating it if necessary.
	WriteFile(name string, data []byte, perm fs.FileMode) error

	// Stat returns file info for the named file.
	Stat(name string) (fs.FileInfo, error)

	// Lstat returns file info without following symlinks.
	Lstat(name string) (fs.FileInfo, error)

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

	// Getwd returns the current working directory.
	Getwd() (string, error)

	// Open opens the named file for reading.
	Open(name string) (FileHandle, error)

	// Create creates or truncates the named file.
	Create(name string) (FileHandle, error)

	// OpenFile opens the named file with specified flag and perm.
	OpenFile(name string, flag int, perm fs.FileMode) (FileHandle, error)

	// Symlink creates newname as a symbolic link to oldname.
	Symlink(oldname, newname string) error

	// Readlink returns the destination of the named symbolic link.
	Readlink(name string) (string, error)

	// Executable returns the path of the current executable.
	Executable() (string, error)
}
