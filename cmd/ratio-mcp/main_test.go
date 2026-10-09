//go:build !windows

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctor(t *testing.T) {
	t.Parallel()
	fixtureDir := filepath.Join("..", "..", "testdata", "fixtures")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"doctor", "--fixture-dir", fixtureDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"status": "ok"`) || !strings.Contains(stdout.String(), `"encryptedRecords": 8`) {
		t.Fatalf("unexpected doctor output: %s", stdout.String())
	}
}

func TestReadPassphraseFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(path, []byte("secret phrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := readPassphraseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer wipe(value)
	if string(value) != "secret phrase" {
		t.Fatalf("unexpected passphrase value: %q", value)
	}
}

func TestReadPassphraseFileRejectsBroadPermissions(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(path, []byte("secret phrase"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readPassphraseFile(path)
	if err == nil || !strings.Contains(err.Error(), "group or others") {
		t.Fatalf("expected permissions error, got %v", err)
	}
}
