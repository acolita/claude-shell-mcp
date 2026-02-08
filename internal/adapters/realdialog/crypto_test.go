package realdialog

import (
	"encoding/hex"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	key, err := generateKey()
	if err != nil {
		t.Fatalf("generateKey() error: %v", err)
	}

	// Should be 64 hex chars (32 bytes)
	if len(key) != 64 {
		t.Errorf("key length = %d, want 64", len(key))
	}

	// Should be valid hex
	if _, err := hex.DecodeString(key); err != nil {
		t.Errorf("key is not valid hex: %v", err)
	}

	// Two keys should be different
	key2, _ := generateKey()
	if key == key2 {
		t.Error("two generated keys are identical")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := generateKey()
	if err != nil {
		t.Fatalf("generateKey() error: %v", err)
	}

	plaintext := []byte("hello, world! this is sensitive data")

	ciphertext, err := encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt() error: %v", err)
	}

	// Ciphertext should be different from plaintext
	if string(ciphertext) == string(plaintext) {
		t.Error("ciphertext equals plaintext")
	}

	decrypted, err := decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("decrypt() error: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDecryptEmptyData(t *testing.T) {
	key, _ := generateKey()

	ciphertext, err := encrypt([]byte{}, key)
	if err != nil {
		t.Fatalf("encrypt(empty) error: %v", err)
	}

	decrypted, err := decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("decrypt(empty) error: %v", err)
	}

	if len(decrypted) != 0 {
		t.Errorf("decrypted length = %d, want 0", len(decrypted))
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1, _ := generateKey()
	key2, _ := generateKey()

	ciphertext, err := encrypt([]byte("secret"), key1)
	if err != nil {
		t.Fatalf("encrypt() error: %v", err)
	}

	_, err = decrypt(ciphertext, key2)
	if err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestDecryptTooShort(t *testing.T) {
	key, _ := generateKey()

	_, err := decrypt([]byte("short"), key)
	if err == nil {
		t.Fatal("decrypt(short) should fail")
	}
}

func TestEncryptInvalidKey(t *testing.T) {
	_, err := encrypt([]byte("data"), "not-hex")
	if err == nil {
		t.Fatal("encrypt with invalid hex key should fail")
	}
}

func TestDecryptInvalidKey(t *testing.T) {
	_, err := decrypt([]byte("data"), "not-hex")
	if err == nil {
		t.Fatal("decrypt with invalid hex key should fail")
	}
}

func TestEncryptDifferentCiphertexts(t *testing.T) {
	key, _ := generateKey()
	plaintext := []byte("same input")

	c1, _ := encrypt(plaintext, key)
	c2, _ := encrypt(plaintext, key)

	// Same plaintext should produce different ciphertext (random nonce)
	if string(c1) == string(c2) {
		t.Error("two encryptions of the same plaintext produced identical ciphertext")
	}

	// But both should decrypt to the same thing
	d1, _ := decrypt(c1, key)
	d2, _ := decrypt(c2, key)
	if string(d1) != string(d2) {
		t.Error("decrypted values differ")
	}
}

func TestWipe(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}
	wipe(data)

	for i, b := range data {
		if b != 0 {
			t.Errorf("data[%d] = %d, want 0", i, b)
		}
	}
}

func TestWipeEmpty(t *testing.T) {
	// Should not panic
	wipe(nil)
	wipe([]byte{})
}
