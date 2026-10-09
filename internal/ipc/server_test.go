//go:build linux

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ipc

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcosperanza/ratio-mcp/internal/localcore"
	"github.com/marcosperanza/ratio-mcp/internal/remote"
	"github.com/marcosperanza/ratio-mcp/internal/vault"
)

type testProvider struct{ value []byte }

func (p testProvider) Load(context.Context) ([]byte, error) {
	return append([]byte(nil), p.value...), nil
}
func (testProvider) Description() string { return "test" }

func TestAdminAndToolsSocketsShareDaemonState(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	vaultPath := filepath.Join(directory, "vault.json")
	passphrase := []byte("correct horse battery staple")
	data := vault.Data{
		ServerURL: "http://127.0.0.1:1", WorkspaceID: "workspace-1", DeviceID: "device-1",
		APIKey: "ratio_lai_test.secret", MasterKey: base64.RawURLEncoding.EncodeToString(make([]byte, 32)),
		CredentialExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	if err := vault.Save(vaultPath, data, passphrase); err != nil {
		t.Fatal(err)
	}
	manager, err := localcore.New(
		vaultPath, filepath.Join(directory, "pause"), testProvider{value: passphrase},
		localcore.WithDeviceVerifier(func(context.Context, *remote.Client, vault.Data) (int, error) { return 200, nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adminPath := filepath.Join(directory, "admin.sock")
	toolsPath := filepath.Join(directory, "tools.sock")
	for path, kind := range map[string]SocketKind{adminPath: AdminSocket, toolsPath: ToolsSocket} {
		server := &Server{Path: path, Kind: kind, Manager: manager, AllowedUID: uint32(os.Geteuid())}
		go func() { _ = server.ListenAndServe(ctx) }()
		waitForSocket(t, path)
	}
	admin := &Client{Path: adminPath}
	var status localcore.Status
	if err := admin.Call(context.Background(), "admin.status", nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.Ready {
		t.Fatal("new daemon must start locked before autounlock")
	}
	if err := admin.Call(context.Background(), "admin.unlock", nil, &status); err != nil {
		t.Fatal(err)
	}
	if !status.Ready {
		t.Fatal("admin unlock did not update shared state")
	}
	var toolsStatus localcore.Status
	if err := (&Client{Path: toolsPath}).Call(context.Background(), "core.status", nil, &toolsStatus); err != nil {
		t.Fatal(err)
	}
	if !toolsStatus.Ready {
		t.Fatal("tools socket did not observe unlocked state")
	}
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s was not created", path)
}
