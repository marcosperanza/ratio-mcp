// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"context"
	"github.com/marcosperanza/ratio-mcp/internal/domain"
	"github.com/marcosperanza/ratio-mcp/internal/policy"
	"testing"
)

type categoryRepo struct {
	snapshotRepo
	catalog *domain.EncryptedCategoryCatalog
}

func (r categoryRepo) CategoryCatalog(context.Context) (*domain.EncryptedCategoryCatalog, error) {
	return r.catalog, nil
}
func TestCustomCategoryQueryByNameKeepsStableIDAndAccounting(t *testing.T) {
	key := make([]byte, 32)
	id := "custom:11111111-1111-4111-8111-111111111111"
	row := encrypted(t, key, `{"description":"Club","merchantName":"Club","category":"`+id+`","amount":-40,"currency":"EUR"}`, "", "")
	row.ID, row.BookingDate = "tx", "2026-09-03"
	record := encrypted(t, key, `{"metadata":{"categories":[{"id":"`+id+`","name":"Padel"}]}}`, "", "")
	repository := categoryRepo{snapshotRepo: snapshotRepo{domain.Snapshot{WorkspaceID: "ws", Transactions: []domain.EncryptedTransaction{row}, Coverage: domain.Coverage{IsComplete: true, CoverageFrom: "2026-01-01", CoverageTo: "2026-12-31"}}}, catalog: &domain.EncryptedCategoryCatalog{EncryptedBlob: record.EncryptedBlob, EncryptionIV: record.EncryptionIV}}
	a := &Analytics{repository: repository, keys: fixedKey(key), policy: policy.FixtureDevelopment()}
	result, err := a.Query(context.Background(), domain.Query{Mode: "transactions", FromMonth: "2026-09", Categories: []string{"padel"}})
	if err != nil || len(result.Transactions) != 1 || result.Transactions[0].Category != id || result.Transactions[0].CategoryName != "Padel" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	summary, err := a.MonthSummary(context.Background(), "2026-09")
	if err != nil || summary.ExpensesMinor != 4000 || summary.Categories[0].CategoryName != "Padel" {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	repository.catalog.EncryptedBlob = "tampered"
	if _, err := a.Query(context.Background(), domain.Query{Mode: "categories", FromMonth: "2026-09"}); err == nil {
		t.Fatal("must reject an unreadable category catalog")
	}
}
