//go:build windows

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package vault

import (
	"errors"
	"os"
)

func verifyOwnedVault(_ string, info os.FileInfo) error {
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("vault must be a regular file, not a reparse point")
	}
	return nil
}
