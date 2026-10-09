//go:build linux || darwin

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"os"
	"syscall"
)

func verifyOwnedFile(_ string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("file is not owned by the current user")
	}
	return nil
}

func validatePassphraseFilePermissions(_ string, info os.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("permissions are too broad")
	}
	return nil
}

func validateVaultLocation(string) error { return nil }
