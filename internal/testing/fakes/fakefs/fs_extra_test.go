package fakefs

import (
	"io"
	"io/fs"
	"os"
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

// ==================== Open / Create / OpenFile tests ====================

func TestFS_Open_ReadFile(t *testing.T) {
	f := New()
	f.AddFile("/data/test.txt", []byte("hello world"), 0644)

	fh, err := f.Open("/data/test.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer fh.Close()

	buf := make([]byte, 20)
	n, err := fh.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "hello world" {
		t.Errorf("Read=%q, want 'hello world'", buf[:n])
	}
}

func TestFS_Open_NotExist(t *testing.T) {
	f := New()

	_, err := f.Open("/nonexistent.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestFS_Create_NewFile(t *testing.T) {
	f := New()

	fh, err := f.Create("/newfile.txt")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	n, err := fh.Write([]byte("created"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 7 {
		t.Errorf("Write returned %d, want 7", n)
	}

	fh.Close()

	// Verify data was written back
	data, err := f.ReadFile("/newfile.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "created" {
		t.Errorf("content=%q, want 'created'", string(data))
	}
}

func TestFS_Create_TruncatesExisting(t *testing.T) {
	f := New()
	f.AddFile("/existing.txt", []byte("old content"), 0644)

	fh, err := f.Create("/existing.txt")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	fh.Write([]byte("new"))
	fh.Close()

	data, _ := f.ReadFile("/existing.txt")
	if string(data) != "new" {
		t.Errorf("content=%q, want 'new' (should be truncated)", string(data))
	}
}

func TestFS_OpenFile_CreateFlag(t *testing.T) {
	f := New()

	fh, err := f.OpenFile("/created.txt", os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	fh.Write([]byte("via openfile"))
	fh.Close()

	data, _ := f.ReadFile("/created.txt")
	if string(data) != "via openfile" {
		t.Errorf("content=%q, want 'via openfile'", string(data))
	}
}

func TestFS_OpenFile_NoCreate_NotExist(t *testing.T) {
	f := New()

	_, err := f.OpenFile("/nonexistent.txt", os.O_RDONLY, 0644)
	if err == nil {
		t.Fatal("expected error opening nonexistent file without O_CREATE")
	}
}

// ==================== Symlink / Readlink tests ====================

func TestFS_Symlink_And_Readlink(t *testing.T) {
	f := New()

	err := f.Symlink("/real/target", "/my/link")
	if err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	target, err := f.Readlink("/my/link")
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if target != "/real/target" {
		t.Errorf("Readlink=%q, want '/real/target'", target)
	}
}

func TestFS_Readlink_NotSymlink(t *testing.T) {
	f := New()

	_, err := f.Readlink("/nonexistent")
	if err == nil {
		t.Fatal("expected error for non-symlink path")
	}
}

func TestFS_AddSymlink_And_Readlink(t *testing.T) {
	f := New()
	f.AddSymlink("/link", "/target")

	target, err := f.Readlink("/link")
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if target != "/target" {
		t.Errorf("Readlink=%q, want '/target'", target)
	}
}

// ==================== Lstat tests ====================

func TestFS_Lstat_Symlink(t *testing.T) {
	f := New()
	f.Symlink("/real/target", "/my/link")

	info, err := f.Lstat("/my/link")
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("Lstat should report ModeSymlink for symlinks")
	}
	if info.Name() != "link" {
		t.Errorf("Name()=%q, want 'link'", info.Name())
	}
}

func TestFS_Lstat_Directory(t *testing.T) {
	f := New()
	f.MkdirAll("/mydir", 0755)

	info, err := f.Lstat("/mydir")
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if !info.IsDir() {
		t.Error("Lstat should report directory")
	}
}

func TestFS_Lstat_File(t *testing.T) {
	f := New()
	f.AddFile("/data.txt", []byte("data"), 0644)

	info, err := f.Lstat("/data.txt")
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.IsDir() {
		t.Error("Lstat should not report directory for regular file")
	}
	if info.Size() != 4 {
		t.Errorf("Size()=%d, want 4", info.Size())
	}
}

func TestFS_Lstat_NotExist(t *testing.T) {
	f := New()

	_, err := f.Lstat("/nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

// ==================== Executable tests ====================

func TestFS_Executable(t *testing.T) {
	f := New()
	f.SetExecutable("/usr/bin/myapp")

	path, err := f.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}
	if path != "/usr/bin/myapp" {
		t.Errorf("Executable()=%q, want '/usr/bin/myapp'", path)
	}
}

func TestFS_Executable_Default(t *testing.T) {
	f := New()

	path, err := f.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}
	if path != "/usr/local/bin/claude-shell-mcp" {
		t.Errorf("Executable()=%q, want '/usr/local/bin/claude-shell-mcp' (default)", path)
	}
}

// ==================== FileHandle method tests ====================

func TestFileHandle_Name(t *testing.T) {
	f := New()
	f.AddFile("/test.txt", []byte("data"), 0644)

	fh, _ := f.Open("/test.txt")
	defer fh.Close()

	if fh.Name() != "/test.txt" {
		t.Errorf("Name()=%q, want '/test.txt'", fh.Name())
	}
}

func TestFileHandle_ReadAt(t *testing.T) {
	f := New()
	f.AddFile("/test.txt", []byte("hello world"), 0644)

	fh, _ := f.Open("/test.txt")
	defer fh.Close()

	buf := make([]byte, 5)
	n, err := fh.ReadAt(buf, 6)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt: %v", err)
	}
	if string(buf[:n]) != "world" {
		t.Errorf("ReadAt=%q, want 'world'", buf[:n])
	}
}

func TestFileHandle_WriteAt(t *testing.T) {
	f := New()

	fh, _ := f.Create("/test.txt")
	fh.Write([]byte("hello world"))

	n, err := fh.WriteAt([]byte("EARTH"), 6)
	if err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if n != 5 {
		t.Errorf("WriteAt returned %d, want 5", n)
	}

	fh.Close()

	data, _ := f.ReadFile("/test.txt")
	if string(data) != "hello EARTH" {
		t.Errorf("content=%q, want 'hello EARTH'", string(data))
	}
}

func TestFileHandle_WriteAt_Extends(t *testing.T) {
	f := New()

	fh, _ := f.Create("/test.txt")
	fh.Write([]byte("hi"))

	// Write past end
	fh.WriteAt([]byte("!"), 10)
	fh.Close()

	data, _ := f.ReadFile("/test.txt")
	if len(data) != 11 {
		t.Errorf("len=%d, want 11 (should extend with zeros)", len(data))
	}
	if data[10] != '!' {
		t.Errorf("data[10]=%d, want '!'", data[10])
	}
}

func TestFileHandle_Seek(t *testing.T) {
	f := New()
	f.AddFile("/test.txt", []byte("abcdef"), 0644)

	fh, _ := f.Open("/test.txt")
	defer fh.Close()

	// Seek to offset 3
	pos, err := fh.Seek(3, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if pos != 3 {
		t.Errorf("pos=%d, want 3", pos)
	}

	buf := make([]byte, 3)
	n, _ := fh.Read(buf)
	if string(buf[:n]) != "def" {
		t.Errorf("Read after Seek=%q, want 'def'", buf[:n])
	}
}

func TestFileHandle_Seek_FromEnd(t *testing.T) {
	f := New()
	f.AddFile("/test.txt", []byte("abcdef"), 0644)

	fh, _ := f.Open("/test.txt")
	defer fh.Close()

	pos, err := fh.Seek(-2, io.SeekEnd)
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if pos != 4 {
		t.Errorf("pos=%d, want 4", pos)
	}

	buf := make([]byte, 2)
	n, _ := fh.Read(buf)
	if string(buf[:n]) != "ef" {
		t.Errorf("Read after SeekEnd=%q, want 'ef'", buf[:n])
	}
}

func TestFileHandle_Truncate_Shrink(t *testing.T) {
	f := New()

	fh, _ := f.Create("/test.txt")
	fh.Write([]byte("hello world"))
	fh.Truncate(5)
	fh.Close()

	data, _ := f.ReadFile("/test.txt")
	if string(data) != "hello" {
		t.Errorf("content=%q, want 'hello'", string(data))
	}
}

func TestFileHandle_Truncate_Grow(t *testing.T) {
	f := New()

	fh, _ := f.Create("/test.txt")
	fh.Write([]byte("hi"))
	fh.Truncate(10)
	fh.Close()

	data, _ := f.ReadFile("/test.txt")
	if len(data) != 10 {
		t.Errorf("len=%d, want 10", len(data))
	}
	if string(data[:2]) != "hi" {
		t.Errorf("prefix=%q, want 'hi'", data[:2])
	}
}

func TestFileHandle_Close_Idempotent(t *testing.T) {
	f := New()
	f.AddFile("/test.txt", []byte("data"), 0644)

	fh, _ := f.Open("/test.txt")
	err1 := fh.Close()
	if err1 != nil {
		t.Fatalf("first Close: %v", err1)
	}

	err2 := fh.Close()
	if err2 != nil {
		t.Errorf("second Close should not error, got: %v", err2)
	}
}

func TestFileHandle_ReadAfterClose(t *testing.T) {
	f := New()
	f.AddFile("/test.txt", []byte("data"), 0644)

	fh, _ := f.Open("/test.txt")
	fh.Close()

	_, err := fh.Read(make([]byte, 4))
	if err == nil {
		t.Fatal("Read after Close should return error")
	}
}

func TestFileHandle_WriteAfterClose(t *testing.T) {
	f := New()

	fh, _ := f.Create("/test.txt")
	fh.Close()

	_, err := fh.Write([]byte("data"))
	if err == nil {
		t.Fatal("Write after Close should return error")
	}
}

func TestFileHandle_SeekAfterClose(t *testing.T) {
	f := New()
	f.AddFile("/test.txt", []byte("data"), 0644)

	fh, _ := f.Open("/test.txt")
	fh.Close()

	_, err := fh.Seek(0, io.SeekStart)
	if err == nil {
		t.Fatal("Seek after Close should return error")
	}
}

func TestFileHandle_ReadAtAfterClose(t *testing.T) {
	f := New()
	f.AddFile("/test.txt", []byte("data"), 0644)

	fh, _ := f.Open("/test.txt")
	fh.Close()

	_, err := fh.ReadAt(make([]byte, 4), 0)
	if err == nil {
		t.Fatal("ReadAt after Close should return error")
	}
}

func TestFileHandle_WriteAtAfterClose(t *testing.T) {
	f := New()

	fh, _ := f.Create("/test.txt")
	fh.Close()

	_, err := fh.WriteAt([]byte("data"), 0)
	if err == nil {
		t.Fatal("WriteAt after Close should return error")
	}
}

func TestFileHandle_TruncateAfterClose(t *testing.T) {
	f := New()

	fh, _ := f.Create("/test.txt")
	fh.Close()

	err := fh.Truncate(0)
	if err == nil {
		t.Fatal("Truncate after Close should return error")
	}
}

func TestFileHandle_Write_ExtendsBeyondData(t *testing.T) {
	f := New()

	fh, _ := f.Create("/test.txt")
	fh.Write([]byte("abc"))
	fh.Seek(0, io.SeekEnd) // at position 3
	fh.Write([]byte("defgh"))
	fh.Close()

	data, _ := f.ReadFile("/test.txt")
	if string(data) != "abcdefgh" {
		t.Errorf("content=%q, want 'abcdefgh'", string(data))
	}
}
