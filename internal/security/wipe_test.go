package security

import (
	"testing"
)

func TestWipeBytes_NonEmpty(t *testing.T) {
	data := []byte("sensitive-data-1234")
	original := make([]byte, len(data))
	copy(original, data)

	WipeBytes(data)

	// After wiping, all bytes should be zero
	for i, b := range data {
		if b != 0 {
			t.Errorf("WipeBytes did not zero byte at index %d: got %d, want 0", i, b)
		}
	}
}

func TestWipeBytes_Empty(t *testing.T) {
	// Should not panic on empty slice
	data := []byte{}
	WipeBytes(data)
}

func TestWipeBytes_Nil(t *testing.T) {
	// Should not panic on nil slice
	var data []byte
	WipeBytes(data)
}

func TestWipeBytes_SingleByte(t *testing.T) {
	data := []byte{0xFF}
	WipeBytes(data)

	if data[0] != 0 {
		t.Errorf("WipeBytes did not zero single byte: got %d, want 0", data[0])
	}
}

func TestWipeString_NonEmpty(t *testing.T) {
	s := "my-secret-password"
	WipeString(&s)

	if s != "" {
		t.Errorf("WipeString did not clear string: got %q, want %q", s, "")
	}
}

func TestWipeString_Empty(t *testing.T) {
	s := ""
	WipeString(&s)

	if s != "" {
		t.Errorf("WipeString changed empty string: got %q, want %q", s, "")
	}
}

func TestWipeString_Nil(t *testing.T) {
	// Should not panic on nil pointer
	WipeString(nil)
}

func TestNewSecureBytes_Basic(t *testing.T) {
	data := []byte("hello-secure-world")
	sb := NewSecureBytes(data)

	if sb == nil {
		t.Fatal("NewSecureBytes returned nil")
	}

	if sb.Len() != len(data) {
		t.Errorf("Len() = %d, want %d", sb.Len(), len(data))
	}

	if string(sb.Data()) != "hello-secure-world" {
		t.Errorf("Data() = %q, want %q", sb.Data(), "hello-secure-world")
	}

	if sb.String() != "hello-secure-world" {
		t.Errorf("String() = %q, want %q", sb.String(), "hello-secure-world")
	}
}

func TestNewSecureBytes_MakesCopy(t *testing.T) {
	data := []byte("original")
	sb := NewSecureBytes(data)

	// Modify original data - should not affect SecureBytes
	data[0] = 'X'

	if sb.String() != "original" {
		t.Errorf("NewSecureBytes did not copy data; got %q, want %q", sb.String(), "original")
	}
}

func TestNewSecureBytes_EmptyData(t *testing.T) {
	data := []byte{}
	sb := NewSecureBytes(data)

	if sb == nil {
		t.Fatal("NewSecureBytes returned nil for empty data")
	}
	if sb.Len() != 0 {
		t.Errorf("Len() = %d, want 0", sb.Len())
	}
	if sb.String() != "" {
		t.Errorf("String() = %q, want %q", sb.String(), "")
	}
	if len(sb.Data()) != 0 {
		t.Errorf("Data() length = %d, want 0", len(sb.Data()))
	}
}

func TestSecureBytes_Wipe(t *testing.T) {
	sb := NewSecureBytes([]byte("secret-value"))

	// Verify data is accessible before wipe
	if sb.String() != "secret-value" {
		t.Fatalf("String() before wipe = %q, want %q", sb.String(), "secret-value")
	}

	sb.Wipe()

	// After wipe, data should be nil
	if sb.Data() != nil {
		t.Error("Data() should be nil after Wipe()")
	}

	// Len should be 0 after wipe
	if sb.Len() != 0 {
		t.Errorf("Len() after Wipe() = %d, want 0", sb.Len())
	}

	// String should be empty after wipe
	if sb.String() != "" {
		t.Errorf("String() after Wipe() = %q, want %q", sb.String(), "")
	}
}

func TestSecureBytes_WipeTwice(t *testing.T) {
	sb := NewSecureBytes([]byte("double-wipe"))

	sb.Wipe()
	// Second wipe should not panic
	sb.Wipe()

	if sb.Data() != nil {
		t.Error("Data() should be nil after double Wipe()")
	}
}

func TestSecureBytes_WipeEmptyData(t *testing.T) {
	sb := NewSecureBytes([]byte{})

	// Should not panic
	sb.Wipe()

	if sb.Len() != 0 {
		t.Errorf("Len() after Wipe() of empty data = %d, want 0", sb.Len())
	}
}

func TestSecureBytes_DataReturnsSameSlice(t *testing.T) {
	sb := NewSecureBytes([]byte("test"))

	d1 := sb.Data()
	d2 := sb.Data()

	// Data() should return the same underlying slice (not a copy)
	if &d1[0] != &d2[0] {
		t.Error("Data() should return the same underlying slice on repeated calls")
	}
}
