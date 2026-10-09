//go:build darwin || windows

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package keystore

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/99designs/keyring"
)

const desktopService = "Ratio Local MCP"

func desktopKey(vaultPath string) string {
	absolute, err := filepath.Abs(vaultPath)
	if err != nil {
		absolute = filepath.Clean(vaultPath)
	}
	return absolute
}

func desktopKeyring() (keyring.Keyring, error) {
	backend := keyring.KeychainBackend
	config := keyring.Config{ServiceName: desktopService, AllowedBackends: []keyring.BackendType{backend}, KeychainAccessibleWhenUnlocked: true}
	if runtimeOS() == "windows" {
		config.AllowedBackends = []keyring.BackendType{keyring.WinCredBackend}
	}
	return keyring.Open(config)
}

func StoreDesktopCredential(vaultPath string, passphrase []byte) error {
	store, err := desktopKeyring()
	if err != nil {
		return errors.New("open the operating system credential store and try again")
	}
	secret := append([]byte(nil), passphrase...)
	defer wipe(secret)
	if err := store.Set(keyring.Item{Key: desktopKey(vaultPath), Data: secret, Label: "Ratio MCP vault passphrase"}); err != nil {
		return errors.New("could not store the vault passphrase in the operating system credential store")
	}
	return nil
}

func LoadDesktopCredential(vaultPath string) ([]byte, error) {
	store, err := desktopKeyring()
	if err != nil {
		return nil, errors.New("open the operating system credential store and try again")
	}
	item, err := store.Get(desktopKey(vaultPath))
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return nil, fmt.Errorf("no passphrase is saved for this vault; run `ratio-mcp credential-store install --vault %q`", vaultPath)
		}
		return nil, errors.New("could not read the vault passphrase from the operating system credential store")
	}
	if len(item.Data) < 12 || len(item.Data) > 4096 {
		wipe(item.Data)
		return nil, errors.New("stored vault passphrase has an invalid length")
	}
	return item.Data, nil
}

func runtimeOS() string { return runtimeGOOS }
