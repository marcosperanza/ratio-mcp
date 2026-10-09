// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVaultRoundTripAndWrongPassphrase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	want := Data{ServerURL: "https://ratio.example", WorkspaceID: "workspace-1", APIKey: "secret", MasterKey: "master"}
	if err := Save(path, want, []byte("correct horse battery staple")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows does not expose POSIX permission bits through FileMode; the
	// vault is stored beneath the current user's application-data directory.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("vault permissions = %o", info.Mode().Perm())
	}
	got, err := Load(path, []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != want.APIKey || got.MasterKey != want.MasterKey || got.WorkspaceID != want.WorkspaceID {
		t.Fatalf("unexpected vault contents: %#v", got)
	}
	if _, err := Load(path, []byte("this passphrase is wrong")); err == nil {
		t.Fatal("wrong passphrase must fail")
	}
}

func TestVaultRejectsWeakParametersAndTrailingJSON(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"kdf":"argon2id","memoryKiB":1,"iterations":0,"parallelism":0,"salt":"AA","nonce":"AA","ciphertext":"AA"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, []byte("correct horse battery staple")); err == nil {
		t.Fatal("weak parameters must be rejected")
	}
	validPath := filepath.Join(t.TempDir(), "valid.json")
	if err := Save(validPath, Data{WorkspaceID: "workspace"}, []byte("correct horse battery staple")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(validPath)
	if err != nil {
		t.Fatal(err)
	}
	var valid any
	if err := json.Unmarshal(raw, &valid); err != nil {
		t.Fatal(err)
	}
	raw = append(raw, []byte("\n{}")...)
	if err := os.WriteFile(validPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(validPath, []byte("correct horse battery staple")); err == nil {
		t.Fatal("trailing JSON must be rejected")
	}
}
