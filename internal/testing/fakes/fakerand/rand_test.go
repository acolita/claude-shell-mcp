package fakerand

import (
	"testing"
)

func TestRandom_Sequential(t *testing.T) {
	r := NewSequential()

	buf := make([]byte, 5)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 5 {
		t.Errorf("Read() n = %d, want %d", n, 5)
	}

	expected := []byte{0, 1, 2, 3, 4}
	for i, b := range buf {
		if b != expected[i] {
			t.Errorf("buf[%d] = %d, want %d", i, b, expected[i])
		}
	}
}

func TestRandom_Fixed(t *testing.T) {
	r := NewFixed([]byte{0xAB, 0xCD})

	buf := make([]byte, 4)
	r.Read(buf)

	// Should cycle through fixed bytes
	expected := []byte{0xAB, 0xCD, 0xAB, 0xCD}
	for i, b := range buf {
		if b != expected[i] {
			t.Errorf("buf[%d] = %02x, want %02x", i, b, expected[i])
		}
	}
}

func TestRandom_Reset(t *testing.T) {
	r := NewSequential()

	buf1 := make([]byte, 3)
	r.Read(buf1) // reads 0, 1, 2

	r.Reset()

	buf2 := make([]byte, 3)
	r.Read(buf2) // should read 0, 1, 2 again

	for i := range buf1 {
		if buf1[i] != buf2[i] {
			t.Errorf("after Reset, buf[%d] = %d, want %d", i, buf2[i], buf1[i])
		}
	}
}
