//go:build windows

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func verifyOwnedFile(path string, info os.FileInfo) error {
	config, err := os.UserConfigDir()
	if err != nil {
		return errors.New("cannot determine the current user's application data directory")
	}
	root, err := filepath.Abs(config)
	if err != nil {
		return errors.New("cannot validate the file location")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return errors.New("cannot validate the file location")
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return errors.New("on Windows, store credential files under the current user's application data directory")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("credential file must be a regular file")
	}
	return nil
}

func validatePassphraseFilePermissions(path string, info os.FileInfo) error {
	return verifyOwnedFile(path, info)
}

func validateVaultLocation(path string) error {
	config, err := os.UserConfigDir()
	if err != nil {
		return errors.New("cannot determine the current user's application data directory")
	}
	root, err := filepath.Abs(filepath.Join(config, "ratio-mcp"))
	if err != nil {
		return errors.New("cannot validate the vault location")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return errors.New("cannot validate the vault location")
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return errors.New("on Windows, store the vault under the current user's application data directory")
	}
	return nil
}
