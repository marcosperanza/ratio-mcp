//go:build !darwin && !windows

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package keystore

func StoreDesktopCredential(string, []byte) error { return nil }

func LoadDesktopCredential(string) ([]byte, error) {
	return nil, ErrDesktopCredentialUnavailable
}
