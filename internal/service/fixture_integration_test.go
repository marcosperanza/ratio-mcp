// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/marcosperanza/ratio-mcp/internal/fixture"
	"github.com/marcosperanza/ratio-mcp/internal/policy"
)

func TestFixtureMonthSummaryUsesExactMinorUnits(t *testing.T) {
	t.Parallel()
	analytics := fixtureAnalytics(t)
	summary, err := analytics.MonthSummary(context.Background(), "2026-09")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "ok" || summary.IncomeMinor != 320000 || summary.ExpensesMinor != 116315 || summary.BalanceMinor != 203685 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.TransactionCount != 5 || summary.ExcludedTransferCount != 1 {
		t.Fatalf("unexpected counts: %+v", summary)
	}
	if len(summary.Categories) != 4 || summary.Categories[0].Category != "Rent/Mortgage" {
		t.Fatalf("unexpected categories: %+v", summary.Categories)
	}
}

func TestFixtureSearch(t *testing.T) {
	t.Parallel()
	analytics := fixtureAnalytics(t)
	result, err := analytics.Search(context.Background(), "2026-09", "supermercato")
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalMatches != 1 || result.Matches[0].AmountMinor != -8540 || result.Matches[0].Merchant != "Mercato Verde" {
		t.Fatalf("unexpected search result: %+v", result)
	}
}

func TestFixtureSearchAcceptsNaturalLanguage(t *testing.T) {
	t.Parallel()
	analytics := fixtureAnalytics(t)
	queries := []string{
		"cerca la spesa del supermercato a settembre 2026",
		"\"SUPERMERCATO\"",
		"mostrami la transazione del supermercato",
	}
	for _, query := range queries {
		result, err := analytics.Search(context.Background(), "2026-09", query)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		if result.TotalMatches != 1 || result.Matches[0].Merchant != "Mercato Verde" {
			t.Fatalf("search %q returned unexpected result: %+v", query, result)
		}
	}
}

func TestFixtureSearchRanksMatchingTerms(t *testing.T) {
	t.Parallel()
	analytics := fixtureAnalytics(t)
	result, err := analytics.Search(context.Background(), "2026-09", "trova il pagamento al bar centrale")
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalMatches < 1 || result.Matches[0].Merchant != "Bar Centrale" {
		t.Fatalf("unexpected ranked result: %+v", result)
	}
}

func fixtureAnalytics(t *testing.T) *Analytics {
	t.Helper()
	directory := filepath.Join("..", "..", "testdata", "fixtures")
	repository, err := fixture.LoadRepository(directory)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := fixture.LoadKeyProvider(directory)
	if err != nil {
		t.Fatal(err)
	}
	analytics, err := NewAnalytics(repository, keys, policy.FixtureDevelopment())
	if err != nil {
		t.Fatal(err)
	}
	return analytics
}
