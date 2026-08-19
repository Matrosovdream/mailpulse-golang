package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Cipher encrypts the values that must never be readable from a database dump:
// mailbox passwords, OAuth refresh tokens, and notifier API keys.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher takes a base64 encoded 32 byte key. There is deliberately no
// fallback key — a hard-coded default would be worse than no encryption,
// because it reads as protection while providing none.
func NewCipher(base64Key string) (*Cipher, error) {
	if base64Key == "" {
		return nil, errors.New("security.encryption_key is empty: set SECURITY_ENCRYPTION_KEY (openssl rand -base64 32)")
	}

	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("security.encryption_key is not valid base64: %w", err)
	}

	if len(key) != 32 {
		return nil, fmt.Errorf("security.encryption_key must decode to 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &Cipher{aead: aead}, nil
}

// Encrypt returns base64(nonce || ciphertext).
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (c *Cipher) Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("ciphertext is not valid base64: %w", err)
	}

	nonceSize := c.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("ciphertext is shorter than the nonce")
	}

	plaintext, err := c.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}
