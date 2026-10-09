// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"

	"github.com/marcosperanza/ratio-mcp/internal/domain"
)

type TransactionRepository interface {
	Snapshot(ctx context.Context) (domain.Snapshot, error)
}

type KeyProvider interface {
	MasterKey(ctx context.Context, workspaceID string) ([]byte, error)
}

type Policy interface {
	Authorize(toolName string) error
	SearchLimit() int
	View(transaction domain.Transaction) domain.TransactionView
}

// Optional repository capability for locally resolving encrypted category names.
type CategoryRepository interface {
	CategoryCatalog(ctx context.Context) (*domain.EncryptedCategoryCatalog, error)
}
