// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package cryptoutil

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
)

func DecryptBase64(ciphertextB64, ivB64 string, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must contain 32 bytes, got %d", len(key))
	}
	ciphertext, err := decodeBase64(ciphertextB64)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	iv, err := decodeBase64(ivB64)
	if err != nil {
		return nil, fmt.Errorf("decode IV: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	if len(iv) != gcm.NonceSize() {
		return nil, fmt.Errorf("IV must contain %d bytes, got %d", gcm.NonceSize(), len(iv))
	}
	plaintext, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return nil, errors.New("decrypt AES-GCM payload: authentication failed")
	}
	return plaintext, nil
}

func DecodeKey(value string) ([]byte, error) {
	key, err := decodeBase64(value)
	if err != nil {
		return nil, fmt.Errorf("decode master key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must contain 32 bytes, got %d", len(key))
	}
	return key, nil
}

func decodeBase64(value string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	var lastErr error
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(value)
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
