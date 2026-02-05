package fakepty

import (
	"io"
	"testing"
	"time"
)

func TestFakePTY_SetBlockReads(t *testing.T) {
	pty := New()
	pty.SetBlockReads(true)

	// Set a very short deadline so the test doesn't hang
	pty.SetReadDeadline(time.Now().Add(10 * time.Millisecond))

	buf := make([]byte, 1024)
	n, err := pty.Read(buf)
	if err != nil {
		t.Errorf("Read with block should return nil error, got %v", err)
	}
	if n != 0 {
		t.Errorf("Read with block should return 0 bytes, got %d", n)
	}
}

func TestFakePTY_SetReadDelay(t *testing.T) {
	pty := New()
	pty.AddResponse("delayed response")
	pty.SetReadDelay(20 * time.Millisecond)

	buf := make([]byte, 1024)
	start := time.Now()
	n, err := pty.Read(buf)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if n == 0 {
		t.Error("expected data from Read")
	}
	if string(buf[:n]) != "delayed response" {
		t.Errorf("Read = %q, want %q", string(buf[:n]), "delayed response")
	}
	if elapsed < 15*time.Millisecond {
		t.Errorf("Read completed too fast (%v), expected delay of ~20ms", elapsed)
	}
}

func TestFakePTY_SetReadDeadline(t *testing.T) {
	pty := New()

	deadline := time.Now().Add(1 * time.Hour)
	err := pty.SetReadDeadline(deadline)
	if err != nil {
		t.Fatalf("SetReadDeadline error: %v", err)
	}

	// SetReadDeadline should not error
	err = pty.SetReadDeadline(time.Time{})
	if err != nil {
		t.Fatalf("SetReadDeadline(zero) error: %v", err)
	}
}

func TestFakePTY_WrittenBytes(t *testing.T) {
	pty := New()

	data := []byte{0x01, 0x02, 0x03, 0xFF}
	_, err := pty.Write(data)
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	got := pty.WrittenBytes()
	if len(got) != len(data) {
		t.Fatalf("WrittenBytes length = %d, want %d", len(got), len(data))
	}
	for i, b := range got {
		if b != data[i] {
			t.Errorf("WrittenBytes[%d] = 0x%02x, want 0x%02x", i, b, data[i])
		}
	}
}

func TestFakePTY_ReadAfterClose(t *testing.T) {
	pty := New()
	pty.AddResponse("should not read this")
	pty.Close()

	buf := make([]byte, 1024)
	n, err := pty.Read(buf)
	if err != io.EOF {
		t.Errorf("Read after Close should return io.EOF, got %v", err)
	}
	if n != 0 {
		t.Errorf("Read after Close should return 0 bytes, got %d", n)
	}
}

func TestFakePTY_WriteAfterClose(t *testing.T) {
	pty := New()
	pty.Close()

	_, err := pty.Write([]byte("data"))
	if err != io.ErrClosedPipe {
		t.Errorf("Write after Close should return io.ErrClosedPipe, got %v", err)
	}
}

func TestFakePTY_WriteStringAfterClose(t *testing.T) {
	pty := New()
	pty.Close()

	_, err := pty.WriteString("data")
	if err != io.ErrClosedPipe {
		t.Errorf("WriteString after Close should return io.ErrClosedPipe, got %v", err)
	}
}

func TestFakePTY_ResetClearsAllState(t *testing.T) {
	pty := New()

	// Set up all kinds of state
	pty.AddResponse("response")
	pty.WriteString("input")
	pty.Interrupt()
	pty.SetBlockReads(true)
	pty.SetReadDelay(100 * time.Millisecond)
	pty.SetReadDeadline(time.Now().Add(1 * time.Hour))
	pty.Close()

	// Reset everything
	result := pty.Reset()

	// Reset should return self for chaining
	if result != pty {
		t.Error("Reset should return the same PTY for chaining")
	}

	// Check all state is cleared
	if pty.Written() != "" {
		t.Error("Reset should clear written data")
	}
	if pty.WasInterrupted() {
		t.Error("Reset should clear interrupted flag")
	}
	if pty.IsClosed() {
		t.Error("Reset should clear closed flag")
	}

	// Should be able to write after reset (not closed)
	_, err := pty.WriteString("after reset")
	if err != nil {
		t.Errorf("WriteString after Reset should succeed, got %v", err)
	}
	if pty.Written() != "after reset" {
		t.Errorf("Written() = %q, want %q", pty.Written(), "after reset")
	}

	// Should have no responses queued
	buf := make([]byte, 1024)
	n, _ := pty.Read(buf)
	if n != 0 {
		t.Errorf("Read after Reset should return 0 bytes, got %d", n)
	}
}

func TestFakePTY_ChainedSetup(t *testing.T) {
	// Test that builder methods can be chained
	pty := New()
	result := pty.AddResponse("r1").AddResponses("r2", "r3").SetBlockReads(false).SetReadDelay(0)

	if result != pty {
		t.Error("chained methods should return the same PTY")
	}
}

func TestFakePTY_BlockReadsWithZeroDeadline(t *testing.T) {
	pty := New()
	pty.SetBlockReads(true)

	// With zero deadline (default), blockReads should check !deadline.IsZero()
	// Since deadline is zero, it should fall through to normal read behavior
	buf := make([]byte, 1024)
	n, err := pty.Read(buf)
	if err != nil {
		t.Errorf("Read error: %v", err)
	}
	// No responses queued, so should return 0
	if n != 0 {
		t.Errorf("Read should return 0 bytes with no responses, got %d", n)
	}
}

func TestFakePTY_ReadNoResponsesReturnsZero(t *testing.T) {
	pty := New()

	buf := make([]byte, 1024)
	n, err := pty.Read(buf)
	if err != nil {
		t.Errorf("Read with no responses should return nil error, got %v", err)
	}
	if n != 0 {
		t.Errorf("Read with no responses should return 0, got %d", n)
	}
}

func TestFakePTY_MultipleWrites(t *testing.T) {
	pty := New()

	pty.WriteString("first ")
	pty.Write([]byte("second "))
	pty.WriteString("third")

	if pty.Written() != "first second third" {
		t.Errorf("Written() = %q, want %q", pty.Written(), "first second third")
	}
}
