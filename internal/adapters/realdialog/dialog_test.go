package realdialog

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/ports"
)

func TestWriteEncryptedPrefill(t *testing.T) {
	key, err := generateKey()
	if err != nil {
		t.Fatalf("generateKey() error: %v", err)
	}

	prefill := ports.ServerFormData{
		Name: "prod",
		Host: "10.0.0.1",
		Port: 22,
		User: "deploy",
	}

	tmpPath, err := writeEncryptedPrefill(prefill, key)
	if err != nil {
		t.Fatalf("writeEncryptedPrefill() error: %v", err)
	}
	defer os.Remove(tmpPath)

	// File should exist with restricted permissions
	info, err := os.Stat(tmpPath)
	if err != nil {
		t.Fatalf("stat temp file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %o, want 0600", info.Mode().Perm())
	}

	// Should be decryptable back to the original data
	encData, _ := os.ReadFile(tmpPath)
	decData, err := decrypt(encData, key)
	if err != nil {
		t.Fatalf("decrypt() error: %v", err)
	}

	var result ports.ServerFormData
	if err := json.Unmarshal(decData, &result); err != nil {
		t.Fatalf("unmarshal() error: %v", err)
	}
	if result.Name != "prod" || result.Host != "10.0.0.1" || result.User != "deploy" {
		t.Errorf("round-trip mismatch: got %+v", result)
	}
}

func TestWriteWrapperScript(t *testing.T) {
	key, _ := generateKey()
	tmpPath := "/tmp/test-form-data.enc"

	wrapperPath, err := writeWrapperScript(tmpPath, key)
	if err != nil {
		t.Fatalf("writeWrapperScript() error: %v", err)
	}
	defer os.Remove(wrapperPath)

	// File should exist and be executable
	info, err := os.Stat(wrapperPath)
	if err != nil {
		t.Fatalf("stat wrapper: %v", err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("wrapper permissions = %o, want 0700", info.Mode().Perm())
	}

	// Should contain shebang, env vars, and exec
	content, _ := os.ReadFile(wrapperPath)
	s := string(content)

	checks := []string{
		"#!/bin/sh",
		"rm -f \"$0\"",
		envFormFile + "='" + tmpPath + "'",
		envFormKey + "='" + key + "'",
		"--form",
	}
	for _, check := range checks {
		if !contains(s, check) {
			t.Errorf("wrapper missing %q", check)
		}
	}
}

func TestWaitForDoneSuccess(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-done-*.done")
	if err != nil {
		t.Fatal(err)
	}
	donePath := tmpFile.Name()
	tmpFile.Close()
	os.Remove(donePath) // start clean

	// Write done file after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.WriteFile(donePath, []byte("ok"), 0600)
	}()

	err = waitForDone(donePath)
	os.Remove(donePath)
	if err != nil {
		t.Fatalf("waitForDone() error: %v", err)
	}
}

func TestWaitForDoneError(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-done-*.done")
	if err != nil {
		t.Fatal(err)
	}
	donePath := tmpFile.Name()
	tmpFile.Close()
	os.Remove(donePath)

	// Write error to done file
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.WriteFile(donePath, []byte("decrypt form data: cipher: message authentication failed"), 0600)
	}()

	err = waitForDone(donePath)
	os.Remove(donePath)
	if err == nil {
		t.Fatal("waitForDone() should return error for non-ok content")
	}
	if !contains(err.Error(), "form helper:") {
		t.Errorf("error = %q, want prefix 'form helper:'", err.Error())
	}
}

func TestReadEncryptedResult(t *testing.T) {
	key, _ := generateKey()

	original := ports.ServerFormData{
		Name:      "staging",
		Host:      "10.0.0.2",
		Port:      2222,
		User:      "admin",
		AuthType:  "password",
		Confirmed: true,
	}

	// Write encrypted result
	data, _ := json.Marshal(original)
	enc, _ := encrypt(data, key)
	wipe(data)

	tmpFile, _ := os.CreateTemp("", "test-result-*.enc")
	tmpPath := tmpFile.Name()
	tmpFile.Write(enc)
	tmpFile.Close()
	defer os.Remove(tmpPath)

	result, err := readEncryptedResult(tmpPath, key)
	if err != nil {
		t.Fatalf("readEncryptedResult() error: %v", err)
	}

	if result.Name != "staging" {
		t.Errorf("Name = %q, want %q", result.Name, "staging")
	}
	if result.Port != 2222 {
		t.Errorf("Port = %d, want %d", result.Port, 2222)
	}
	if !result.Confirmed {
		t.Error("Confirmed = false, want true")
	}
}

func TestReadEncryptedResultWrongKey(t *testing.T) {
	key1, _ := generateKey()
	key2, _ := generateKey()

	data, _ := json.Marshal(ports.ServerFormData{Name: "test"})
	enc, _ := encrypt(data, key1)

	tmpFile, _ := os.CreateTemp("", "test-result-*.enc")
	tmpPath := tmpFile.Name()
	tmpFile.Write(enc)
	tmpFile.Close()
	defer os.Remove(tmpPath)

	_, err := readEncryptedResult(tmpPath, key2)
	if err == nil {
		t.Fatal("readEncryptedResult with wrong key should fail")
	}
}

func TestReadEncryptedResultMissingFile(t *testing.T) {
	key, _ := generateKey()
	_, err := readEncryptedResult("/nonexistent/file.enc", key)
	if err == nil {
		t.Fatal("readEncryptedResult with missing file should fail")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
