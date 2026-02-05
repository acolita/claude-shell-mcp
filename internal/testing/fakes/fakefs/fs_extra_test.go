package fakefs

import (
	"io/fs"
	"testing"
	"time"
)

func TestFS_ReadFileNotExist(t *testing.T) {
	f := New()

	_, err := f.ReadFile("/nonexistent/file.txt")
	if err == nil {
		t.Fatal("ReadFile should return error for nonexistent file")
	}

	pathErr, ok := err.(*fs.PathError)
	if !ok {
		t.Fatalf("expected *fs.PathError, got %T", err)
	}
	if pathErr.Op != "open" {
		t.Errorf("PathError.Op = %q, want %q", pathErr.Op, "open")
	}
	if pathErr.Err != fs.ErrNotExist {
		t.Errorf("PathError.Err = %v, want %v", pathErr.Err, fs.ErrNotExist)
	}
}

func TestFS_Chtimes(t *testing.T) {
	f := New()
	f.AddFile("/tmp/test.txt", []byte("hello"), 0644)

	newTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	err := f.Chtimes("/tmp/test.txt", newTime, newTime)
	if err != nil {
		t.Fatalf("Chtimes error: %v", err)
	}

	info, err := f.Stat("/tmp/test.txt")
	if err != nil {
		t.Fatalf("Stat error: %v", err)
	}
	if !info.ModTime().Equal(newTime) {
		t.Errorf("ModTime() = %v, want %v", info.ModTime(), newTime)
	}
}

func TestFS_ChtimesNotExist(t *testing.T) {
	f := New()

	err := f.Chtimes("/nonexistent.txt", time.Now(), time.Now())
	if err == nil {
		t.Fatal("Chtimes should return error for nonexistent file")
	}

	pathErr, ok := err.(*fs.PathError)
	if !ok {
		t.Fatalf("expected *fs.PathError, got %T", err)
	}
	if pathErr.Op != "chtimes" {
		t.Errorf("PathError.Op = %q, want %q", pathErr.Op, "chtimes")
	}
	if pathErr.Err != fs.ErrNotExist {
		t.Errorf("PathError.Err = %v, want %v", pathErr.Err, fs.ErrNotExist)
	}
}

func TestFS_UserHomeDir(t *testing.T) {
	f := New()

	dir, err := f.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir error: %v", err)
	}
	if dir != "/home/test" {
		t.Errorf("UserHomeDir() = %q, want %q", dir, "/home/test")
	}
}

func TestFS_SetHomeDir(t *testing.T) {
	f := New()
	f.SetHomeDir("/custom/home")

	dir, err := f.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir error: %v", err)
	}
	if dir != "/custom/home" {
		t.Errorf("UserHomeDir() = %q, want %q", dir, "/custom/home")
	}
}

func TestFS_Getenv(t *testing.T) {
	f := New()

	// Unset variable returns empty string
	if got := f.Getenv("MISSING_VAR"); got != "" {
		t.Errorf("Getenv for unset var = %q, want %q", got, "")
	}
}

func TestFS_SetEnvAndGetenv(t *testing.T) {
	f := New()
	f.SetEnv("MY_VAR", "my_value")
	f.SetEnv("PATH", "/usr/bin:/bin")

	if got := f.Getenv("MY_VAR"); got != "my_value" {
		t.Errorf("Getenv(MY_VAR) = %q, want %q", got, "my_value")
	}
	if got := f.Getenv("PATH"); got != "/usr/bin:/bin" {
		t.Errorf("Getenv(PATH) = %q, want %q", got, "/usr/bin:/bin")
	}
}

func TestFS_Getwd(t *testing.T) {
	f := New()

	cwd, err := f.Getwd()
	if err != nil {
		t.Fatalf("Getwd error: %v", err)
	}
	if cwd != "/project" {
		t.Errorf("Getwd() = %q, want %q", cwd, "/project")
	}
}

func TestFS_SetCwd(t *testing.T) {
	f := New()
	f.SetCwd("/new/working/dir")

	cwd, err := f.Getwd()
	if err != nil {
		t.Fatalf("Getwd error: %v", err)
	}
	if cwd != "/new/working/dir" {
		t.Errorf("Getwd() = %q, want %q", cwd, "/new/working/dir")
	}
}

func TestFS_Files(t *testing.T) {
	f := New()
	f.AddFile("/b/file2.txt", []byte("b"), 0644)
	f.AddFile("/a/file1.txt", []byte("a"), 0644)
	f.AddFile("/c/file3.txt", []byte("c"), 0644)

	files := f.Files()
	if len(files) != 3 {
		t.Fatalf("Files() returned %d files, want 3", len(files))
	}

	// Files should be sorted
	expected := []string{"/a/file1.txt", "/b/file2.txt", "/c/file3.txt"}
	for i, path := range files {
		if path != expected[i] {
			t.Errorf("Files()[%d] = %q, want %q", i, path, expected[i])
		}
	}
}

func TestFS_FilesEmpty(t *testing.T) {
	f := New()

	files := f.Files()
	if len(files) != 0 {
		t.Errorf("Files() on empty FS returned %d files, want 0", len(files))
	}
}

func TestFS_RemoveDirectory(t *testing.T) {
	f := New()
	f.MkdirAll("/a/b/c", 0755)

	// Remove empty leaf directory
	err := f.Remove("/a/b/c")
	if err != nil {
		t.Fatalf("Remove empty dir error: %v", err)
	}

	// Should no longer exist
	_, err = f.Stat("/a/b/c")
	if err == nil {
		t.Error("directory should not exist after Remove")
	}
}

func TestFS_RemoveNonEmptyDirectory(t *testing.T) {
	f := New()
	f.AddFile("/mydir/file.txt", []byte("data"), 0644)

	err := f.Remove("/mydir")
	if err == nil {
		t.Fatal("Remove should fail for non-empty directory")
	}

	pathErr, ok := err.(*fs.PathError)
	if !ok {
		t.Fatalf("expected *fs.PathError, got %T", err)
	}
	if pathErr.Op != "remove" {
		t.Errorf("PathError.Op = %q, want %q", pathErr.Op, "remove")
	}
	if pathErr.Err != fs.ErrInvalid {
		t.Errorf("PathError.Err = %v, want %v", pathErr.Err, fs.ErrInvalid)
	}
}

func TestFS_RemoveNotExist(t *testing.T) {
	f := New()

	err := f.Remove("/nonexistent")
	if err == nil {
		t.Fatal("Remove should fail for nonexistent path")
	}

	pathErr, ok := err.(*fs.PathError)
	if !ok {
		t.Fatalf("expected *fs.PathError, got %T", err)
	}
	if pathErr.Err != fs.ErrNotExist {
		t.Errorf("PathError.Err = %v, want %v", pathErr.Err, fs.ErrNotExist)
	}
}

func TestFS_RenameNotExist(t *testing.T) {
	f := New()

	err := f.Rename("/nonexistent.txt", "/new.txt")
	if err == nil {
		t.Fatal("Rename should fail for nonexistent source")
	}

	pathErr, ok := err.(*fs.PathError)
	if !ok {
		t.Fatalf("expected *fs.PathError, got %T", err)
	}
	if pathErr.Op != "rename" {
		t.Errorf("PathError.Op = %q, want %q", pathErr.Op, "rename")
	}
	if pathErr.Err != fs.ErrNotExist {
		t.Errorf("PathError.Err = %v, want %v", pathErr.Err, fs.ErrNotExist)
	}
}

func TestFS_StatDirectory(t *testing.T) {
	f := New()
	f.MkdirAll("/my/dir", 0755)

	info, err := f.Stat("/my/dir")
	if err != nil {
		t.Fatalf("Stat error: %v", err)
	}
	if !info.IsDir() {
		t.Error("IsDir() should be true for directory")
	}
	if info.Name() != "dir" {
		t.Errorf("Name() = %q, want %q", info.Name(), "dir")
	}
}

func TestFS_fakeFileInfo_Mode(t *testing.T) {
	f := New()
	f.AddFile("/tmp/script.sh", []byte("#!/bin/bash"), 0755)

	info, err := f.Stat("/tmp/script.sh")
	if err != nil {
		t.Fatalf("Stat error: %v", err)
	}

	if info.Mode() != 0755 {
		t.Errorf("Mode() = %o, want %o", info.Mode(), 0755)
	}
}

func TestFS_fakeFileInfo_ModTime(t *testing.T) {
	f := New()
	f.AddFile("/tmp/test.txt", []byte("data"), 0644)

	newTime := time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)
	f.Chtimes("/tmp/test.txt", newTime, newTime)

	info, err := f.Stat("/tmp/test.txt")
	if err != nil {
		t.Fatalf("Stat error: %v", err)
	}

	if !info.ModTime().Equal(newTime) {
		t.Errorf("ModTime() = %v, want %v", info.ModTime(), newTime)
	}
}

func TestFS_fakeFileInfo_Sys(t *testing.T) {
	f := New()
	f.AddFile("/tmp/test.txt", []byte("data"), 0644)

	info, err := f.Stat("/tmp/test.txt")
	if err != nil {
		t.Fatalf("Stat error: %v", err)
	}

	if info.Sys() != nil {
		t.Errorf("Sys() = %v, want nil", info.Sys())
	}
}

func TestFS_WriteFileOverwritePreservesIsolation(t *testing.T) {
	f := New()

	// Write original data
	original := []byte("original content")
	f.WriteFile("/tmp/test.txt", original, 0644)

	// Mutate the original slice
	original[0] = 'X'

	// Read should return the unmutated copy
	data, err := f.ReadFile("/tmp/test.txt")
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "original content" {
		t.Errorf("WriteFile did not copy data; ReadFile = %q, want %q", data, "original content")
	}
}

func TestFS_ReadFileReturnsIsolatedCopy(t *testing.T) {
	f := New()
	f.WriteFile("/tmp/test.txt", []byte("immutable"), 0644)

	// Read and mutate
	data1, _ := f.ReadFile("/tmp/test.txt")
	data1[0] = 'Z'

	// Second read should be unaffected
	data2, _ := f.ReadFile("/tmp/test.txt")
	if string(data2) != "immutable" {
		t.Errorf("ReadFile returned shared slice; got %q, want %q", data2, "immutable")
	}
}

func TestFS_AddFileCreatesParentDirs(t *testing.T) {
	f := New()
	f.AddFile("/a/b/c/d/file.txt", []byte("deep"), 0644)

	// All parent directories should exist
	for _, dir := range []string{"/a", "/a/b", "/a/b/c", "/a/b/c/d"} {
		info, err := f.Stat(dir)
		if err != nil {
			t.Errorf("parent dir %q should exist, got error: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q should be a directory", dir)
		}
	}
}
