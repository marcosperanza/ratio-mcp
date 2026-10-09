//go:build linux

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ipc

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadLineLimitedRejectsOversizedResponse(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", 65)+"\n"), 8)
	if _, err := readLineLimited(reader, 64); err == nil {
		t.Fatal("expected oversized response to be rejected")
	}
}

func TestReadLineLimitedAcceptsFragmentedResponse(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader("{\"ok\":true}\n"), 4)
	value, err := readLineLimited(reader, 64)
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "{\"ok\":true}\n" {
		t.Fatalf("unexpected value %q", value)
	}
}
