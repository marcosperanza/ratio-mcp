//go:build linux || darwin

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package vault

import (
	"errors"
	"os"
	"syscall"
)

func verifyOwnedVault(_ string, info os.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("vault must be a private 0600 file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("vault must be owned by the current user")
	}
	return nil
}
