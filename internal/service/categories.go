// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/marcosperanza/ratio-mcp/internal/core"
	"github.com/marcosperanza/ratio-mcp/internal/cryptoutil"
	"github.com/marcosperanza/ratio-mcp/internal/domain"
	"strings"
)

func categoryNames(transactions []domain.Transaction) map[string]string {
	names := make(map[string]string)
	for _, tx := range transactions {
		if tx.CategoryName != "" {
			names[tx.Category] = tx.CategoryName
		}
	}
	return names
}
func (a *Analytics) resolveCategoryNames(ctx context.Context, transactions []domain.Transaction, key []byte) error {
	needed := false
	for _, tx := range transactions {
		if strings.HasPrefix(tx.Category, "custom:") {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	repository, ok := a.repository.(core.CategoryRepository)
	if !ok {
		return errors.New("custom categories require an encrypted category repository")
	}
	record, err := repository.CategoryCatalog(ctx)
	if err != nil {
		return errors.New("could not fetch encrypted category catalog")
	}
	if record == nil {
		return errors.New("custom category catalog is missing")
	}
	plaintext, err := cryptoutil.DecryptBase64(record.EncryptedBlob, record.EncryptionIV, key)
	if err != nil {
		return errors.New("could not decrypt category catalog")
	}
	defer clear(plaintext)
	var blob struct {
		Metadata struct {
			Categories []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"categories"`
		} `json:"metadata"`
	}
	if json.Unmarshal(plaintext, &blob) != nil {
		return errors.New("invalid category catalog")
	}
	names := map[string]string{}
	for _, category := range blob.Metadata.Categories {
		if !strings.HasPrefix(category.ID, "custom:") || strings.TrimSpace(category.Name) == "" || len(category.Name) > 240 {
			return errors.New("invalid category definition")
		}
		if _, exists := names[category.ID]; exists {
			return errors.New("duplicate category ID")
		}
		names[category.ID] = category.Name
	}
	for i := range transactions {
		if !strings.HasPrefix(transactions[i].Category, "custom:") {
			continue
		}
		name, exists := names[transactions[i].Category]
		if !exists {
			return errors.New("transaction category missing from catalog")
		}
		transactions[i].CategoryName = name
	}
	return nil
}
