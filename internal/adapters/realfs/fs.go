// Package realfs provides a real implementation of the FileSystem port using the os package.
package realfs

import (
	"io/fs"
	"os"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/ports"
)

// FS implements ports.FileSystem using the standard os package.
type FS struct{}

// New returns a new real FileSystem.
func New() *FS {
	return &FS{}
}

// ReadFile reads the named file and returns its contents.
func (f *FS) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

// WriteFile writes data to the named file, creating it if necessary.
func (f *FS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(name, data, perm)
}

// Stat returns file info for the named file.
func (f *FS) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(name)
}

// Lstat returns file info without following symlinks.
func (f *FS) Lstat(name string) (fs.FileInfo, error) {
	return os.Lstat(name)
}

// MkdirAll creates a directory and all parent directories.
func (f *FS) MkdirAll(path string, perm fs.FileMode) error {
	return os.MkdirAll(path, perm)
}

// Remove removes the named file or empty directory.
func (f *FS) Remove(name string) error {
	return os.Remove(name)
}

// Rename renames (moves) oldpath to newpath.
func (f *FS) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

// Chtimes changes the access and modification times of the named file.
func (f *FS) Chtimes(name string, atime, mtime time.Time) error {
	return os.Chtimes(name, atime, mtime)
}

// UserHomeDir returns the current user's home directory.
func (f *FS) UserHomeDir() (string, error) {
	return os.UserHomeDir()
}

// Getenv retrieves the value of the environment variable named by the key.
func (f *FS) Getenv(key string) string {
	return os.Getenv(key)
}

// Getwd returns the current working directory.
func (f *FS) Getwd() (string, error) {
	return os.Getwd()
}

// Open opens the named file for reading.
func (f *FS) Open(name string) (ports.FileHandle, error) {
	return os.Open(name)
}

// Create creates or truncates the named file.
func (f *FS) Create(name string) (ports.FileHandle, error) {
	return os.Create(name)
}

// OpenFile opens the named file with specified flag and perm.
func (f *FS) OpenFile(name string, flag int, perm fs.FileMode) (ports.FileHandle, error) {
	return os.OpenFile(name, flag, perm)
}

// Symlink creates newname as a symbolic link to oldname.
func (f *FS) Symlink(oldname, newname string) error {
	return os.Symlink(oldname, newname)
}

// Readlink returns the destination of the named symbolic link.
func (f *FS) Readlink(name string) (string, error) {
	return os.Readlink(name)
}

// Executable returns the path of the current executable.
func (f *FS) Executable() (string, error) {
	return os.Executable()
}

// Ensure FS implements ports.FileSystem.
var _ ports.FileSystem = (*FS)(nil)
