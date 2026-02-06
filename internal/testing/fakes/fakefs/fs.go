// Package fakefs provides an in-memory FileSystem implementation for testing.
package fakefs

import (
	"bytes"
	"fmt"
	"io"
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
	mu         sync.RWMutex
	files      map[string]*fakeFile
	dirs       map[string]bool
	symlinks   map[string]string // target path for each symlink
	homeDir    string
	cwd        string
	env        map[string]string
	executable string // path returned by Executable()
}

type fakeFile struct {
	data    []byte
	mode    fs.FileMode
	modTime time.Time
}

// New creates a new in-memory filesystem.
func New() *FS {
	return &FS{
		files:      make(map[string]*fakeFile),
		dirs:       map[string]bool{"/": true},
		symlinks:   make(map[string]string),
		homeDir:    "/home/test",
		cwd:        "/project",
		env:        make(map[string]string),
		executable: "/usr/local/bin/claude-shell-mcp",
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

// Getwd returns the current working directory.
func (f *FS) Getwd() (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.cwd, nil
}

// Lstat returns file info without following symlinks.
func (f *FS) Lstat(name string) (fs.FileInfo, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	name = filepath.Clean(name)

	// Check if it's a symlink
	if target, ok := f.symlinks[name]; ok {
		return &fakeFileInfo{
			name:    filepath.Base(name),
			size:    int64(len(target)),
			mode:    os.ModeSymlink | 0777,
			modTime: time.Now(),
			isDir:   false,
		}, nil
	}

	// Fall through to regular stat behavior
	if f.dirs[name] {
		return &fakeFileInfo{
			name:    filepath.Base(name),
			size:    0,
			mode:    fs.ModeDir | 0755,
			modTime: time.Now(),
			isDir:   true,
		}, nil
	}

	file, ok := f.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: fs.ErrNotExist}
	}

	return &fakeFileInfo{
		name:    filepath.Base(name),
		size:    int64(len(file.data)),
		mode:    file.mode,
		modTime: file.modTime,
		isDir:   false,
	}, nil
}

// Open opens the named file for reading.
func (f *FS) Open(name string) (ports.FileHandle, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	name = filepath.Clean(name)
	file, ok := f.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	data := make([]byte, len(file.data))
	copy(data, file.data)

	return &fakeFileHandle{
		name:   name,
		data:   data,
		reader: bytes.NewReader(data),
		fs:     f,
		flag:   os.O_RDONLY,
	}, nil
}

// Create creates or truncates the named file.
func (f *FS) Create(name string) (ports.FileHandle, error) {
	return f.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
}

// OpenFile opens the named file with specified flag and perm.
func (f *FS) OpenFile(name string, flag int, perm fs.FileMode) (ports.FileHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	name = filepath.Clean(name)

	if flag&os.O_CREATE != 0 {
		// Ensure parent dir exists
		dir := filepath.Dir(name)
		f.mkdirAllLocked(dir)

		if _, ok := f.files[name]; !ok || flag&os.O_TRUNC != 0 {
			f.files[name] = &fakeFile{
				data:    nil,
				mode:    perm,
				modTime: time.Now(),
			}
		}
	}

	file, ok := f.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	data := make([]byte, len(file.data))
	copy(data, file.data)

	if flag&os.O_TRUNC != 0 {
		data = nil
	}

	return &fakeFileHandle{
		name:   name,
		data:   data,
		reader: bytes.NewReader(data),
		fs:     f,
		flag:   flag,
	}, nil
}

// Symlink creates newname as a symbolic link to oldname.
func (f *FS) Symlink(oldname, newname string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	newname = filepath.Clean(newname)
	f.symlinks[newname] = oldname
	return nil
}

// Readlink returns the destination of the named symbolic link.
func (f *FS) Readlink(name string) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	name = filepath.Clean(name)
	target, ok := f.symlinks[name]
	if !ok {
		return "", &fs.PathError{Op: "readlink", Path: name, Err: fs.ErrInvalid}
	}
	return target, nil
}

// Executable returns the path of the current executable.
func (f *FS) Executable() (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.executable, nil
}

// fakeFileHandle implements ports.FileHandle for in-memory files.
type fakeFileHandle struct {
	name   string
	data   []byte
	reader *bytes.Reader
	pos    int64
	fs     *FS
	flag   int
	closed bool
}

func (fh *fakeFileHandle) Read(b []byte) (int, error) {
	if fh.closed {
		return 0, fmt.Errorf("file closed")
	}
	return fh.reader.Read(b)
}

func (fh *fakeFileHandle) Write(b []byte) (int, error) {
	if fh.closed {
		return 0, fmt.Errorf("file closed")
	}
	pos, _ := fh.reader.Seek(0, io.SeekCurrent)
	// Extend data if writing past end
	endPos := pos + int64(len(b))
	if endPos > int64(len(fh.data)) {
		newData := make([]byte, endPos)
		copy(newData, fh.data)
		fh.data = newData
	}
	copy(fh.data[pos:], b)
	fh.reader = bytes.NewReader(fh.data)
	fh.reader.Seek(endPos, io.SeekStart)
	return len(b), nil
}

func (fh *fakeFileHandle) Seek(offset int64, whence int) (int64, error) {
	if fh.closed {
		return 0, fmt.Errorf("file closed")
	}
	return fh.reader.Seek(offset, whence)
}

func (fh *fakeFileHandle) Close() error {
	if fh.closed {
		return nil
	}
	fh.closed = true
	// Write data back to the FS
	fh.fs.mu.Lock()
	defer fh.fs.mu.Unlock()
	if file, ok := fh.fs.files[fh.name]; ok {
		dataCopy := make([]byte, len(fh.data))
		copy(dataCopy, fh.data)
		file.data = dataCopy
	}
	return nil
}

func (fh *fakeFileHandle) ReadAt(b []byte, off int64) (int, error) {
	if fh.closed {
		return 0, fmt.Errorf("file closed")
	}
	return fh.reader.ReadAt(b, off)
}

func (fh *fakeFileHandle) WriteAt(b []byte, off int64) (int, error) {
	if fh.closed {
		return 0, fmt.Errorf("file closed")
	}
	endPos := off + int64(len(b))
	if endPos > int64(len(fh.data)) {
		newData := make([]byte, endPos)
		copy(newData, fh.data)
		fh.data = newData
	}
	copy(fh.data[off:], b)
	fh.reader = bytes.NewReader(fh.data)
	return len(b), nil
}

func (fh *fakeFileHandle) Truncate(size int64) error {
	if fh.closed {
		return fmt.Errorf("file closed")
	}
	if size < int64(len(fh.data)) {
		fh.data = fh.data[:size]
	} else {
		newData := make([]byte, size)
		copy(newData, fh.data)
		fh.data = newData
	}
	fh.reader = bytes.NewReader(fh.data)
	return nil
}

func (fh *fakeFileHandle) Name() string {
	return fh.name
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

// SetCwd sets the current working directory returned by Getwd.
func (f *FS) SetCwd(dir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cwd = dir
}

// SetExecutable sets the path returned by Executable().
func (f *FS) SetExecutable(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executable = path
}

// AddSymlink adds a symlink to the fake filesystem.
func (f *FS) AddSymlink(name, target string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.symlinks[filepath.Clean(name)] = target
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
