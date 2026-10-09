// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"
)

const (
	memoryKiB   = 64 * 1024
	iterations  = 3
	parallelism = 1
)

type Data struct {
	ServerURL           string   `json:"serverUrl"`
	PairingID           string   `json:"pairingId"`
	DeviceID            string   `json:"deviceId"`
	WorkspaceID         string   `json:"workspaceId"`
	PrivateKeyPEM       string   `json:"privateKeyPem"`
	MasterKey           string   `json:"masterKey"`
	APIKey              string   `json:"apiKey"`
	Scopes              []string `json:"scopes"`
	CredentialExpiresAt string   `json:"credentialExpiresAt"`
	ActivationPending   bool     `json:"activationPending"`
}

type file struct {
	Version     int    `json:"version"`
	KDF         string `json:"kdf"`
	MemoryKiB   uint32 `json:"memoryKiB"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
	Salt        string `json:"salt"`
	Nonce       string `json:"nonce"`
	Ciphertext  string `json:"ciphertext"`
}

func Save(path string, data Data, passphrase []byte) error {
	if len(passphrase) < 12 {
		return errors.New("vault passphrase must contain at least 12 characters")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	key := argon2.IDKey(passphrase, salt, iterations, memoryKiB, parallelism, 32)
	defer wipe(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	plaintext, err := json.Marshal(data)
	if err != nil {
		return err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte("ratio-local-ai-vault-v1"))
	wipe(plaintext)
	encoded, err := json.MarshalIndent(file{Version: 1, KDF: "argon2id", MemoryKiB: memoryKiB, Iterations: iterations, Parallelism: parallelism, Salt: base64.RawURLEncoding.EncodeToString(salt), Nonce: base64.RawURLEncoding.EncodeToString(nonce), Ciphertext: base64.RawURLEncoding.EncodeToString(ciphertext)}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".vault-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func Load(path string, passphrase []byte) (Data, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Data{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Data{}, errors.New("vault must be a regular 0600 file")
	}
	if err := verifyOwnedVault(path, info); err != nil {
		return Data{}, err
	}
	if info.Size() <= 0 || info.Size() > 4<<20 {
		return Data{}, errors.New("vault has an invalid size")
	}
	fileHandle, err := os.Open(path)
	if err != nil {
		return Data{}, err
	}
	defer fileHandle.Close()
	raw, err := io.ReadAll(io.LimitReader(fileHandle, (4<<20)+1))
	if err != nil {
		return Data{}, err
	}
	var stored file
	decoder := json.NewDecoder(bytesReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return Data{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Data{}, errors.New("vault contains trailing JSON")
	}
	if stored.Version != 1 || stored.KDF != "argon2id" || stored.MemoryKiB < 8*1024 || stored.MemoryKiB > 128*1024 || stored.Iterations < 1 || stored.Iterations > 10 || stored.Parallelism < 1 || stored.Parallelism > 4 {
		return Data{}, errors.New("unsupported vault parameters")
	}
	salt, err := base64.RawURLEncoding.DecodeString(stored.Salt)
	if err != nil {
		return Data{}, err
	}
	if len(salt) != 16 {
		return Data{}, errors.New("invalid vault salt")
	}
	nonce, err := base64.RawURLEncoding.DecodeString(stored.Nonce)
	if err != nil {
		return Data{}, err
	}
	if len(nonce) != 12 {
		return Data{}, errors.New("invalid vault nonce")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(stored.Ciphertext)
	if err != nil {
		return Data{}, err
	}
	if len(ciphertext) < 16 {
		return Data{}, errors.New("invalid vault ciphertext")
	}
	key := argon2.IDKey(passphrase, salt, stored.Iterations, stored.MemoryKiB, stored.Parallelism, 32)
	defer wipe(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return Data{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Data{}, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte("ratio-local-ai-vault-v1"))
	if err != nil {
		return Data{}, errors.New("unable to unlock vault")
	}
	defer wipe(plaintext)
	var data Data
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return Data{}, fmt.Errorf("decode vault: %w", err)
	}
	return data, nil
}

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
