package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

var (
	ErrKeyTooShort = errors.New("encryption key must be at least 32 characters")
	ErrDecryptFail = errors.New("decryption failed")
)

// DeriveKey derives a 32-byte AES-256 key from a string key using SHA-256.
func DeriveKey(key string) ([]byte, error) {
	if len(key) == 0 {
		return nil, ErrKeyTooShort
	}
	h := sha256.Sum256([]byte(key))
	return h[:], nil
}

// GenerateKey generates a random 256-bit key encoded as base64.
func GenerateKey() string {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.StdEncoding.EncodeToString(key)
}

// Encrypt encrypts plaintext using AES-256-GCM with the given key.
// Returns the nonce prepended to the ciphertext.
func Encrypt(plaintext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// nonce + ciphertext
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext using AES-256-GCM with the given key.
// Expects nonce prepended to ciphertext.
func Decrypt(ciphertext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrDecryptFail
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptFail
	}

	return plaintext, nil
}
