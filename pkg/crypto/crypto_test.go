package crypto

import (
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key, err := DeriveKey("my-test-encryption-key-for-http-pg-project")
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}

	plaintext := []byte("SELECT * FROM users WHERE id = 1;")

	ciphertext, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Fatalf("roundtrip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	key1, _ := DeriveKey("key-one-for-http-pg-project-testing")
	key2, _ := DeriveKey("key-two-for-http-pg-project-testing")

	ciphertext, _ := Encrypt([]byte("hello"), key1)
	_, err := Decrypt(ciphertext, key2)
	if err == nil {
		t.Fatal("expected error with wrong key, got nil")
	}
}

func TestDeriveKeyTooShort(t *testing.T) {
	_, err := DeriveKey("")
	if err != ErrKeyTooShort {
		t.Fatalf("expected ErrKeyTooShort, got %v", err)
	}
}

func TestEncryptEmptyPlaintext(t *testing.T) {
	key, _ := DeriveKey("test-key-for-empty-plaintext-test")
	ciphertext, err := Encrypt([]byte{}, key)
	if err != nil {
		t.Fatalf("Encrypt empty failed: %v", err)
	}
	decrypted, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt empty failed: %v", err)
	}
	if len(decrypted) != 0 {
		t.Fatalf("expected empty decrypted, got %v", decrypted)
	}
}

func TestGenerateKey(t *testing.T) {
	k1 := GenerateKey()
	k2 := GenerateKey()
	if k1 == k2 {
		t.Fatal("generated keys should be unique")
	}
	if len(k1) < 40 {
		t.Fatalf("key too short: %d chars", len(k1))
	}
}
