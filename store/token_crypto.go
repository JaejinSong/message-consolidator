package store

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"

	"message-consolidator/logger"
)

// encMagic marks a stored value as AES-256-GCM sealed. Legacy plaintext rows lack it,
// so reads transparently fall through and the next write re-persists them encrypted.
const encMagic = "encv1:"

var errNoEncKey = errors.New("token encryption key not configured")

var (
	tokenEncKey  []byte
	tokenEncOnce sync.Once
)

// InitTokenEncryption loads the AES-256 key from TOKEN_ENC_KEY (64 hex chars = 32 bytes).
// When unset or invalid, token columns are stored in plaintext (logged once) so local
// development keeps working; production must configure the key.
func InitTokenEncryption() {
	tokenEncOnce.Do(func() {
		raw := os.Getenv("TOKEN_ENC_KEY")
		if raw == "" {
			logger.Warnf("[SECURITY] TOKEN_ENC_KEY unset; OAuth/session tokens stored in plaintext")
			return
		}
		key, err := hex.DecodeString(strings.TrimSpace(raw))
		if err != nil || len(key) != 32 {
			logger.Errorf("[SECURITY] TOKEN_ENC_KEY invalid (need 64 hex chars); tokens will NOT be encrypted")
			return
		}
		tokenEncKey = key
	})
}

func tokenAEAD() (cipher.AEAD, error) {
	if tokenEncKey == nil {
		return nil, errNoEncKey
	}
	block, err := aes.NewCipher(tokenEncKey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(plaintext []byte) ([]byte, error) {
	aead, err := tokenAEAD()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func open(sealed []byte) ([]byte, error) {
	aead, err := tokenAEAD()
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	return aead.Open(nil, nonce, ct, nil)
}

// encryptString returns an encrypted, marker-prefixed value, or the plaintext unchanged
// when no key is configured (dev fallback).
func encryptString(plaintext string) string {
	sealed, err := seal([]byte(plaintext))
	if err != nil {
		return plaintext
	}
	return encMagic + base64.StdEncoding.EncodeToString(sealed)
}

// decryptString reverses encryptString. Values without the marker are legacy plaintext
// and returned as-is; a decrypt failure also falls back to the raw stored value.
func decryptString(stored string) string {
	if !strings.HasPrefix(stored, encMagic) {
		return stored
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encMagic))
	if err != nil {
		return stored
	}
	pt, err := open(raw)
	if err != nil {
		logger.Errorf("[SECURITY] failed to decrypt token; returning raw value")
		return stored
	}
	return string(pt)
}

var encMagicBytes = []byte(encMagic)

// encryptBytes seals a binary payload (e.g. a Telegram session blob).
func encryptBytes(plaintext []byte) []byte {
	sealed, err := seal(plaintext)
	if err != nil {
		return plaintext
	}
	return append(append([]byte{}, encMagicBytes...), sealed...)
}

// decryptBytes reverses encryptBytes with the same legacy-plaintext fallback.
func decryptBytes(stored []byte) []byte {
	if !bytes.HasPrefix(stored, encMagicBytes) {
		return stored
	}
	pt, err := open(stored[len(encMagicBytes):])
	if err != nil {
		logger.Errorf("[SECURITY] failed to decrypt session blob; returning raw value")
		return stored
	}
	return pt
}
