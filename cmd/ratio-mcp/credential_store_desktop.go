//go:build darwin || windows

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/marcosperanza/ratio-mcp/internal/keystore"
)

func runCredentialStore(args []string, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "install" {
		return errors.New("usage: ratio-mcp credential-store install [--vault PATH]")
	}
	flags := flag.NewFlagSet("credential-store install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	vaultPath := flags.String("vault", defaultVaultPath(), "encrypted Ratio vault")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected credential-store arguments: %v", flags.Args())
	}
	if err := validateVaultLocation(*vaultPath); err != nil {
		return err
	}
	passphrase, err := readPassphrase("Vault passphrase to save in the operating system credential store: ", stderr)
	if err != nil {
		return err
	}
	defer wipe(passphrase)
	if _, err := buildRealAnalyticsFromPassphrase(*vaultPath, passphrase); err != nil {
		return errors.New("the supplied passphrase does not unlock the selected vault")
	}
	if err := keystore.StoreDesktopCredential(*vaultPath, passphrase); err != nil {
		return err
	}
	fmt.Fprintln(stderr, "Vault passphrase saved in the operating system credential store.")
	return nil
}
