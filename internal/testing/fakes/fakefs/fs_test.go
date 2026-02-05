package fakefs

import (
	"io/fs"
	"testing"
)

func TestFS_ReadWriteFile(t *testing.T) {
	f := New()

	// WriteFile auto-creates parent directories (like production behavior)
	err := f.WriteFile("/nonexistent/nested/file.txt", []byte("data"), 0644)
	if err != nil {
		t.Fatalf("WriteFile() should auto-create parents, got error: %v", err)
	}

	// Verify data was written
	data, err := f.ReadFile("/nonexistent/nested/file.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "data" {
		t.Errorf("ReadFile() = %q, want %q", data, "data")
	}

	// Test overwrite
	err = f.WriteFile("/nonexistent/nested/file.txt", []byte("updated"), 0644)
	if err != nil {
		t.Fatalf("WriteFile() overwrite error = %v", err)
	}

	data, err = f.ReadFile("/nonexistent/nested/file.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "updated" {
		t.Errorf("ReadFile() = %q, want %q", data, "updated")
	}
}

func TestFS_Stat(t *testing.T) {
	f := New()
	f.AddFile("/tmp/test.txt", []byte("hello"), 0644)

	info, err := f.Stat("/tmp/test.txt")
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if info.Name() != "test.txt" {
		t.Errorf("Name() = %q, want %q", info.Name(), "test.txt")
	}
	if info.Size() != 5 {
		t.Errorf("Size() = %d, want %d", info.Size(), 5)
	}
	if info.IsDir() {
		t.Error("IsDir() = true, want false")
	}
}

func TestFS_StatNotExist(t *testing.T) {
	f := New()

	_, err := f.Stat("/nonexistent")
	if err == nil {
		t.Error("Stat() should return error for nonexistent file")
	}
	if !isNotExist(err) {
		t.Errorf("expected ErrNotExist, got %v", err)
	}
}

func TestFS_MkdirAll(t *testing.T) {
	f := New()

	err := f.MkdirAll("/a/b/c/d", 0755)
	if err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Verify directories exist via Stat
	for _, path := range []string{"/a", "/a/b", "/a/b/c", "/a/b/c/d"} {
		info, err := f.Stat(path)
		if err != nil {
			t.Errorf("Stat(%q) error = %v", path, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("Stat(%q).IsDir() = false, want true", path)
		}
	}
}

func TestFS_Remove(t *testing.T) {
	f := New()
	f.AddFile("/tmp/test.txt", []byte("data"), 0644)

	err := f.Remove("/tmp/test.txt")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	_, err = f.Stat("/tmp/test.txt")
	if err == nil {
		t.Error("file should not exist after Remove()")
	}
}

func TestFS_Rename(t *testing.T) {
	f := New()
	f.AddFile("/tmp/old.txt", []byte("data"), 0644)

	err := f.Rename("/tmp/old.txt", "/tmp/new.txt")
	if err != nil {
		t.Fatalf("Rename() error = %v", err)
	}

	// Old should not exist
	_, err = f.Stat("/tmp/old.txt")
	if err == nil {
		t.Error("old file should not exist after Rename()")
	}

	// New should exist
	data, err := f.ReadFile("/tmp/new.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "data" {
		t.Errorf("ReadFile() = %q, want %q", data, "data")
	}
}

func TestFS_AddFile(t *testing.T) {
	f := New()
	f.AddFile("/deep/nested/path/file.txt", []byte("content"), 0644)

	data, err := f.ReadFile("/deep/nested/path/file.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "content" {
		t.Errorf("ReadFile() = %q, want %q", data, "content")
	}
}

func isNotExist(err error) bool {
	if pathErr, ok := err.(*fs.PathError); ok {
		return pathErr.Err == fs.ErrNotExist
	}
	return false
}
