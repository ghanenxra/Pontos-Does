package auth

import (
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key := []byte("12345678901234567890123456789012") // 32 bytes key
	originalText := "secret_oauth_access_token_12345!"

	encrypted, err := Encrypt(originalText, key)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	if encrypted == originalText {
		t.Fatalf("encrypted text is same as plaintext")
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if decrypted != originalText {
		t.Errorf("expected decrypted text %q, got %q", originalText, decrypted)
	}
}

func TestHashToken(t *testing.T) {
	token := "some_random_session_token_xyz"
	hash1 := HashToken(token)
	hash2 := HashToken(token)

	if hash1 != hash2 {
		t.Errorf("hashing is not deterministic")
	}

	if len(hash1) != 64 { // SHA256 hex string length is 64 characters
		t.Errorf("expected hex hash length 64, got %d", len(hash1))
	}
}
