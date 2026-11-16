// Package secretbox 使用 AES-256-GCM 加解密落库的敏感字段（代码源凭证、webhook secret、密钥类构建变量）。
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	KeySize       = 32
	cipherVersion = "v1:"
)

var ErrInvalidCiphertext = errors.New("secretbox: invalid ciphertext")

type Box struct {
	aead cipher.AEAD
}

func New(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("secretbox: key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretbox: create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: create gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Encrypt 返回 "v1:" + base64(nonce || ciphertext)。空字符串原样返回，对应表里的空默认值。
func (b *Box) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secretbox: generate nonce: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return cipherVersion + base64.StdEncoding.EncodeToString(sealed), nil
}

func (b *Box) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	encoded, ok := strings.CutPrefix(ciphertext, cipherVersion)
	if !ok {
		return "", ErrInvalidCiphertext
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(sealed) < b.aead.NonceSize()+b.aead.Overhead() {
		return "", ErrInvalidCiphertext
	}
	nonceSize := b.aead.NonceSize()
	plaintext, err := b.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	return string(plaintext), nil
}
