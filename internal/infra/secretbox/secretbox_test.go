package secretbox

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func testKey(fill byte) []byte {
	return bytes.Repeat([]byte{fill}, KeySize)
}

func TestBoxRoundTrip(t *testing.T) {
	box, err := New(testKey(1))
	if err != nil {
		t.Fatalf("new box: %v", err)
	}

	for _, plaintext := range []string{"glpat-secret-token", "中文密钥", strings.Repeat("x", 4096)} {
		ciphertext, err := box.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		if ciphertext == plaintext || strings.Contains(ciphertext, plaintext) {
			t.Fatalf("ciphertext leaks plaintext: %q", ciphertext)
		}
		if !strings.HasPrefix(ciphertext, cipherVersion) {
			t.Fatalf("expected version prefix, got %q", ciphertext)
		}
		decrypted, err := box.Decrypt(ciphertext)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if decrypted != plaintext {
			t.Fatalf("round trip mismatch: got %q", decrypted)
		}
	}
}

func TestBoxUsesRandomNonce(t *testing.T) {
	box, _ := New(testKey(1))
	first, _ := box.Encrypt("same")
	second, _ := box.Encrypt("same")
	if first == second {
		t.Fatal("expected different ciphertexts for the same plaintext")
	}
}

func TestBoxKeepsEmptyValue(t *testing.T) {
	box, _ := New(testKey(1))
	ciphertext, err := box.Encrypt("")
	if err != nil || ciphertext != "" {
		t.Fatalf("expected empty ciphertext, got %q, %v", ciphertext, err)
	}
	plaintext, err := box.Decrypt("")
	if err != nil || plaintext != "" {
		t.Fatalf("expected empty plaintext, got %q, %v", plaintext, err)
	}
}

func TestBoxRejectsWrongKeyAndTampering(t *testing.T) {
	box, _ := New(testKey(1))
	other, _ := New(testKey(2))
	ciphertext, _ := box.Encrypt("secret")

	if _, err := other.Decrypt(ciphertext); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("expected invalid ciphertext with wrong key, got %v", err)
	}

	tampered := ciphertext[:len(ciphertext)-2] + "AA"
	if tampered == ciphertext {
		tampered = ciphertext[:len(ciphertext)-2] + "BB"
	}
	if _, err := box.Decrypt(tampered); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("expected invalid ciphertext after tampering, got %v", err)
	}
	for _, invalid := range []string{"plain", "v1:not-base64!", "v1:AAAA"} {
		if _, err := box.Decrypt(invalid); !errors.Is(err, ErrInvalidCiphertext) {
			t.Fatalf("expected invalid ciphertext for %q, got %v", invalid, err)
		}
	}
}

func TestNewRejectsInvalidKeySize(t *testing.T) {
	for _, size := range []int{0, 16, 31, 33} {
		if _, err := New(make([]byte, size)); err == nil {
			t.Fatalf("expected error for key size %d", size)
		}
	}
}
