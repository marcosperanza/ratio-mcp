//go:build linux

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package keystore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemdCredentialLoadsProtectedFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(path, []byte("correct horse battery staple\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := (SystemdCredential{Path: path}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer wipe(value)
	if string(value) != "correct horse battery staple" {
		t.Fatalf("unexpected credential value %q", value)
	}
}

func TestSystemdCredentialRejectsBroadPermissions(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(path, []byte("correct horse battery staple"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := (SystemdCredential{Path: path}).Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("expected permissions error, got %v", err)
	}
}

func TestSystemdCredentialAcceptsRuntimeACLMode(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, DefaultCredentialName)
	if err := os.WriteFile(path, []byte("correct horse battery staple"), 0o640); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", directory)
	value, err := (SystemdCredential{Name: DefaultCredentialName}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wipe(value)
}
