// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package fixture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcosperanza/ratio-mcp/internal/cryptoutil"
	"github.com/marcosperanza/ratio-mcp/internal/domain"
)

type Repository struct {
	snapshot domain.Snapshot
}

type repositoryFile struct {
	WorkspaceID  string                        `json:"workspaceId"`
	Coverage     domain.Coverage               `json:"coverage"`
	Transactions []domain.EncryptedTransaction `json:"transactions"`
}

func LoadRepository(directory string) (*Repository, error) {
	path := filepath.Join(directory, "repository.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fixture repository %q: %w", path, err)
	}
	var file repositoryFile
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("decode fixture repository %q: %w", path, err)
	}
	if strings.TrimSpace(file.WorkspaceID) == "" {
		return nil, errors.New("fixture repository has no workspaceId")
	}
	if len(file.Transactions) == 0 {
		return nil, errors.New("fixture repository has no transactions")
	}
	for i := range file.Transactions {
		transaction := &file.Transactions[i]
		if transaction.WorkspaceID == "" {
			transaction.WorkspaceID = file.WorkspaceID
		}
		if transaction.WorkspaceID != file.WorkspaceID {
			return nil, fmt.Errorf("transaction %q belongs to unexpected workspace", transaction.ID)
		}
		if transaction.ID == "" || transaction.BookingDate == "" || transaction.EncryptedBlob == "" || transaction.EncryptionIV == "" {
			return nil, fmt.Errorf("transaction at index %d is incomplete", i)
		}
	}
	return &Repository{snapshot: domain.Snapshot{
		WorkspaceID:  file.WorkspaceID,
		Transactions: file.Transactions,
		Coverage:     file.Coverage,
	}}, nil
}

func (r *Repository) Snapshot(context.Context) (domain.Snapshot, error) {
	transactions := make([]domain.EncryptedTransaction, len(r.snapshot.Transactions))
	copy(transactions, r.snapshot.Transactions)
	snapshot := r.snapshot
	snapshot.Transactions = transactions
	return snapshot, nil
}

type KeyProvider struct {
	workspaceID string
	masterKey   []byte
}

type keyFile struct {
	WorkspaceID string `json:"workspaceId"`
	MasterKey   string `json:"masterKey"`
}

func LoadKeyProvider(directory string) (*KeyProvider, error) {
	path := filepath.Join(directory, "master_key.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fixture key %q: %w", path, err)
	}
	var file keyFile
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("decode fixture key %q: %w", path, err)
	}
	if file.WorkspaceID == "" {
		return nil, errors.New("fixture key has no workspaceId")
	}
	key, err := cryptoutil.DecodeKey(file.MasterKey)
	if err != nil {
		return nil, err
	}
	return &KeyProvider{workspaceID: file.WorkspaceID, masterKey: key}, nil
}

func (p *KeyProvider) MasterKey(_ context.Context, workspaceID string) ([]byte, error) {
	if workspaceID != p.workspaceID {
		return nil, fmt.Errorf("no key for workspace %q", workspaceID)
	}
	key := make([]byte, len(p.masterKey))
	copy(key, p.masterKey)
	return key, nil
}
