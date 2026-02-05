// Package fakefs provides an in-memory FileSystem implementation for testing.
package fakefs

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/ports"
)

// FS is an in-memory filesystem for testing.
type FS struct {
	mu      sync.RWMutex
	files   map[string]*fakeFile
	dirs    map[string]bool
	homeDir string
	env     map[string]string
}

type fakeFile struct {
	data    []byte
	mode    fs.FileMode
	modTime time.Time
}

// New creates a new in-memory filesystem.
func New() *FS {
	return &FS{
		files:   make(map[string]*fakeFile),
		dirs:    map[string]bool{"/": true},
		homeDir: "/home/test",
		env:     make(map[string]string),
	}
}

// ReadFile reads the named file and returns its contents.
func (f *FS) ReadFile(name string) ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	name = filepath.Clean(name)
	file, ok := f.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	// Return a copy to prevent mutation
	data := make([]byte, len(file.data))
	copy(data, file.data)
	return data, nil
}

// WriteFile writes data to the named file, creating it if necessary.
// Parent directories are automatically created (like os.WriteFile with MkdirAll).
func (f *FS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	name = filepath.Clean(name)

	// Auto-create parent directories
	dir := filepath.Dir(name)
	f.mkdirAllLocked(dir)

	// Store a copy to prevent mutation
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	f.files[name] = &fakeFile{
		data:    dataCopy,
		mode:    perm,
		modTime: time.Now(),
	}
	return nil
}

// mkdirAllLocked creates directories (must be called with lock held).
func (f *FS) mkdirAllLocked(path string) {
	path = filepath.Clean(path)
	parts := strings.Split(path, string(filepath.Separator))

	current := ""
	for _, part := range parts {
		if part == "" {
			current = "/"
			continue
		}
		if current == "/" {
			current = "/" + part
		} else {
			current = current + "/" + part
		}
		f.dirs[current] = true
	}
}

// Stat returns file info for the named file.
func (f *FS) Stat(name string) (fs.FileInfo, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	name = filepath.Clean(name)

	// Check if it's a directory
	if f.dirs[name] {
		return &fakeFileInfo{
			name:    filepath.Base(name),
			size:    0,
			mode:    fs.ModeDir | 0755,
			modTime: time.Now(),
			isDir:   true,
		}, nil
	}

	// Check if it's a file
	file, ok := f.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}

	return &fakeFileInfo{
		name:    filepath.Base(name),
		size:    int64(len(file.data)),
		mode:    file.mode,
		modTime: file.modTime,
		isDir:   false,
	}, nil
}

// MkdirAll creates a directory and all parent directories.
func (f *FS) MkdirAll(path string, perm fs.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.mkdirAllLocked(path)
	return nil
}

// Remove removes the named file or empty directory.
func (f *FS) Remove(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	name = filepath.Clean(name)

	if _, ok := f.files[name]; ok {
		delete(f.files, name)
		return nil
	}

	if f.dirs[name] {
		// Check if directory is empty
		for path := range f.files {
			if strings.HasPrefix(path, name+"/") {
				return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrInvalid}
			}
		}
		delete(f.dirs, name)
		return nil
	}

	return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
}

// Rename renames (moves) oldpath to newpath.
func (f *FS) Rename(oldpath, newpath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	oldpath = filepath.Clean(oldpath)
	newpath = filepath.Clean(newpath)

	file, ok := f.files[oldpath]
	if !ok {
		return &fs.PathError{Op: "rename", Path: oldpath, Err: fs.ErrNotExist}
	}

	f.files[newpath] = file
	delete(f.files, oldpath)
	return nil
}

// Chtimes changes the access and modification times of the named file.
func (f *FS) Chtimes(name string, atime, mtime time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	name = filepath.Clean(name)
	file, ok := f.files[name]
	if !ok {
		return &fs.PathError{Op: "chtimes", Path: name, Err: fs.ErrNotExist}
	}

	file.modTime = mtime
	return nil
}

// UserHomeDir returns the configured home directory.
func (f *FS) UserHomeDir() (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.homeDir, nil
}

// Getenv retrieves the value of the environment variable.
func (f *FS) Getenv(key string) string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.env[key]
}

// --- Test helpers ---

// AddFile adds a file to the fake filesystem.
func (f *FS) AddFile(name string, data []byte, mode fs.FileMode) {
	f.mu.Lock()
	defer f.mu.Unlock()

	name = filepath.Clean(name)

	// Ensure parent directories exist
	dir := filepath.Dir(name)
	parts := strings.Split(dir, string(filepath.Separator))
	current := ""
	for _, part := range parts {
		if part == "" {
			current = "/"
			continue
		}
		if current == "/" {
			current = "/" + part
		} else {
			current = current + "/" + part
		}
		f.dirs[current] = true
	}

	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	f.files[name] = &fakeFile{
		data:    dataCopy,
		mode:    mode,
		modTime: time.Now(),
	}
}

// SetHomeDir sets the home directory returned by UserHomeDir.
func (f *FS) SetHomeDir(dir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.homeDir = dir
}

// SetEnv sets an environment variable.
func (f *FS) SetEnv(key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.env[key] = value
}

// Files returns a sorted list of all file paths.
func (f *FS) Files() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	paths := make([]string, 0, len(f.files))
	for path := range f.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// fakeFileInfo implements fs.FileInfo.
type fakeFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	isDir   bool
}

func (fi *fakeFileInfo) Name() string       { return fi.name }
func (fi *fakeFileInfo) Size() int64        { return fi.size }
func (fi *fakeFileInfo) Mode() fs.FileMode  { return fi.mode }
func (fi *fakeFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *fakeFileInfo) IsDir() bool        { return fi.isDir }
func (fi *fakeFileInfo) Sys() any           { return nil }

// Ensure FS implements ports.FileSystem.
var _ ports.FileSystem = (*FS)(nil)

// Ensure fakeFileInfo implements fs.FileInfo.
var _ os.FileInfo = (*fakeFileInfo)(nil)
