//go:build windows

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
	fixtureDir := filepath.Join("..", "..", "testdata", "fixtures")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"doctor", "--fixture-dir", fixtureDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"status": "ok"`) {
		t.Fatalf("unexpected doctor output: %s", stdout.String())
	}
}

func TestWindowsVaultMustStayInProfileConfig(t *testing.T) {
	t.Setenv("APPDATA", filepath.Join(t.TempDir(), "appdata"))
	inside := filepath.Join(os.Getenv("APPDATA"), "ratio-mcp", "vault.json")
	if err := validateVaultLocation(inside); err != nil {
		t.Fatalf("expected user profile vault path to be allowed: %v", err)
	}
	if err := validateVaultLocation(filepath.Join(t.TempDir(), "vault.json")); err == nil {
		t.Fatal("expected vault path outside user profile config to be rejected")
	}
}
