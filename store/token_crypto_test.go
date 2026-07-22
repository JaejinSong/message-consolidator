package store

import (
	"bytes"
	"strings"
	"testing"
)

func withKey(t *testing.T) func() {
	t.Helper()
	prev := tokenEncKey
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	tokenEncKey = key
	return func() { tokenEncKey = prev }
}

func TestEncryptDecryptString_RoundTrip(t *testing.T) {
	defer withKey(t)()

	plaintext := `{"access_token":"secret","refresh_token":"r"}`
	enc := encryptString(plaintext)
	if enc == plaintext {
		t.Fatal("ciphertext equals plaintext")
	}
	if !strings.HasPrefix(enc, encMagic) {
		t.Fatalf("missing enc marker: %q", enc)
	}
	if got := decryptString(enc); got != plaintext {
		t.Errorf("decrypt = %q, want %q", got, plaintext)
	}
}

func TestDecryptString_LegacyPlaintextPassthrough(t *testing.T) {
	defer withKey(t)()

	legacy := `{"access_token":"plain"}`
	if got := decryptString(legacy); got != legacy {
		t.Errorf("legacy plaintext should pass through, got %q", got)
	}
}

func TestEncryptString_NoKeyPassthrough(t *testing.T) {
	prev := tokenEncKey
	tokenEncKey = nil
	defer func() { tokenEncKey = prev }()

	plaintext := "no-key-here"
	if got := encryptString(plaintext); got != plaintext {
		t.Errorf("without key, encrypt should passthrough, got %q", got)
	}
}

func TestEncryptDecryptBytes_RoundTrip(t *testing.T) {
	defer withKey(t)()

	blob := []byte{0x00, 0x01, 0x02, 0xff, 0xfe}
	enc := encryptBytes(blob)
	if bytes.Equal(enc, blob) {
		t.Fatal("ciphertext equals plaintext blob")
	}
	if !bytes.HasPrefix(enc, encMagicBytes) {
		t.Fatal("missing enc marker on blob")
	}
	if got := decryptBytes(enc); !bytes.Equal(got, blob) {
		t.Errorf("decrypt blob = %v, want %v", got, blob)
	}
}

func TestDecryptBytes_LegacyPlaintextPassthrough(t *testing.T) {
	defer withKey(t)()

	legacy := []byte("legacy session bytes")
	if got := decryptBytes(legacy); !bytes.Equal(got, legacy) {
		t.Errorf("legacy blob should pass through, got %v", got)
	}
}
