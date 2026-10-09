//go:build !linux && !darwin && !windows

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"io"
)

func runCredentialStore([]string, io.Writer) error {
	return errors.New("credential-store is available on Linux systemd, macOS Keychain and Windows Credential Manager")
}
