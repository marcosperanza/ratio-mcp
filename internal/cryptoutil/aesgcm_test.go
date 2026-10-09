// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package cryptoutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDecryptBrowserCompatibleVector(t *testing.T) {
	t.Parallel()
	type vector struct {
		MasterKeyB64  string `json:"masterKey"`
		IVB64         string `json:"iv"`
		CiphertextB64 string `json:"ciphertext"`
		Plaintext     string `json:"plaintext"`
	}
	path := filepath.Join("..", "..", "testdata", "vectors", "aes_gcm_browser_compatible.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v vector
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	key, err := DecodeKey(v.MasterKeyB64)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := DecryptBase64(v.CiphertextB64, v.IVB64, key)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != v.Plaintext {
		t.Fatalf("plaintext mismatch: got %q want %q", plaintext, v.Plaintext)
	}
}

func TestDecryptRejectsWrongKey(t *testing.T) {
	t.Parallel()
	key := make([]byte, 32)
	_, err := DecryptBase64("AAAAAAAAAAAAAAAAAAAAAA==", "AAAAAAAAAAAAAAAA", key)
	if err == nil {
		t.Fatal("expected authentication error")
	}
}
