// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package keystore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const DefaultCredentialName = "ratio-mcp-vault-passphrase"

// Provider releases a vault passphrase to the daemon. Implementations must
// never log or return the secret as part of an error.
type Provider interface {
	Load(context.Context) ([]byte, error)
	Description() string
}

type SystemdCredential struct {
	Name string
	Path string
}

func (p SystemdCredential) Description() string { return "systemd credential" }

func (p SystemdCredential) resolvePath() (string, bool, error) {
	if p.Path != "" {
		return filepath.Clean(p.Path), false, nil
	}
	directory := os.Getenv("CREDENTIALS_DIRECTORY")
	if directory == "" {
		return "", false, errors.New("systemd credentials are unavailable")
	}
	name := p.Name
	if name == "" {
		name = DefaultCredentialName
	}
	if filepath.Base(name) != name || name == "." {
		return "", false, errors.New("invalid credential name")
	}
	return filepath.Join(directory, name), true, nil
}

func (p SystemdCredential) Load(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	path, systemdRuntime, err := p.resolvePath()
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("vault credential is not installed")
		}
		return nil, errors.New("unable to open vault credential")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("vault credential is not a regular file")
	}
	if (!systemdRuntime && info.Mode().Perm()&0o077 != 0) || (systemdRuntime && info.Mode().Perm()&0o007 != 0) {
		return nil, errors.New("vault credential permissions are too broad")
	}
	if info.Size() < 12 || info.Size() > 4096 {
		return nil, errors.New("vault credential has an invalid size")
	}
	value, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return nil, errors.New("unable to read vault credential")
	}
	value = bytes.TrimSuffix(value, []byte("\n"))
	value = bytes.TrimSuffix(value, []byte("\r"))
	if len(value) < 12 || len(value) > 4096 {
		wipe(value)
		return nil, fmt.Errorf("vault credential has an invalid passphrase length")
	}
	return value, nil
}

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
