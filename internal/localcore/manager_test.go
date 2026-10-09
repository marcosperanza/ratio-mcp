// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package localcore

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcosperanza/ratio-mcp/internal/remote"
	"github.com/marcosperanza/ratio-mcp/internal/vault"
)

type staticProvider struct{ passphrase []byte }

func (p staticProvider) Load(context.Context) ([]byte, error) {
	return append([]byte(nil), p.passphrase...), nil
}

func TestOperationSessionResolvesWorkspaceByName(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write([]byte(`{"defaultWorkspaceId":"ws-default","workspaces":[{"id":"ws-family","name":"Famiglia","type":"FAMILY","role":"OWNER"}]}`))
	}))
	defer server.Close()
	client, err := remote.NewClient(server.URL, "ratio_lai_test.secret")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := (operationSession{client: client, defaultWorkspaceID: "ws-default"}).resolveWorkspace(context.Background(), "famiglia")
	if err != nil {
		t.Fatal(err)
	}
	if workspace.ID != "ws-family" || workspace.Name != "Famiglia" {
		t.Fatalf("unexpected workspace: %#v", workspace)
	}
}
func (staticProvider) Description() string { return "test" }

func TestManagerUnlockLockAndResume(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	vaultPath := filepath.Join(directory, "vault.json")
	pausePath := filepath.Join(directory, "autounlock.paused")
	passphrase := []byte("correct horse battery staple")
	masterKey := make([]byte, 32)
	for index := range masterKey {
		masterKey[index] = byte(index + 1)
	}
	data := vault.Data{
		ServerURL:           "http://127.0.0.1:1",
		DeviceID:            "device-1",
		WorkspaceID:         "workspace-1",
		APIKey:              "ratio_lai_test.secret",
		MasterKey:           base64.RawURLEncoding.EncodeToString(masterKey),
		CredentialExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	if err := vault.Save(vaultPath, data, passphrase); err != nil {
		t.Fatal(err)
	}
	verifier := WithDeviceVerifier(func(context.Context, *remote.Client, vault.Data) (int, error) { return 200, nil })
	manager, err := New(vaultPath, pausePath, staticProvider{passphrase: passphrase}, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.AutoUnlock(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := manager.Status(); !status.Ready || status.Vault != "unlocked" {
		t.Fatalf("unexpected unlocked status: %#v", status)
	}
	if err := manager.Lock(); err != nil {
		t.Fatal(err)
	}
	if status := manager.Status(); status.Ready || !status.AutounlockPaused {
		t.Fatalf("unexpected locked status: %#v", status)
	}
	if _, err := os.Stat(pausePath); err != nil {
		t.Fatalf("pause marker missing: %v", err)
	}
	restarted, err := New(vaultPath, pausePath, staticProvider{passphrase: passphrase}, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.AutoUnlock(context.Background()); err == nil {
		t.Fatal("autounlock must respect the persistent administrative lock")
	}
	if err := restarted.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := restarted.Status(); !status.Ready || status.AutounlockPaused {
		t.Fatalf("unexpected resumed status: %#v", status)
	}
}
