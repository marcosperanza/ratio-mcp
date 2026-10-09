// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/marcosperanza/ratio-mcp/internal/domain"
	"github.com/marcosperanza/ratio-mcp/internal/policy"
)

func queryAnalytics(t *testing.T) *Analytics {
	t.Helper()
	key := make([]byte, 32)
	tx := func(id, date, cat, merchant, amount, compFrom, compTo string) domain.EncryptedTransaction {
		e := encrypted(t, key, `{"description":"`+merchant+` desc","merchantName":"`+merchant+`","category":"`+cat+
			`","amount":`+amount+`,"currency":"EUR"}`, "", "")
		e.ID, e.BookingDate, e.CompetenzaFrom, e.CompetenzaTo = id, date, compFrom, compTo
		return e
	}
	return &Analytics{repository: snapshotRepo{domain.Snapshot{WorkspaceID: "ws",
		Coverage: domain.Coverage{CoverageFrom: "2026-01-01", CoverageTo: "2026-12-31", IsComplete: true},
		Transactions: []domain.EncryptedTransaction{
			tx("1", "2026-09-03", "Shopping", "Zara", "-80", "", ""),
			tx("2", "2026-09-20", "Shopping", "Amazon", "-45.50", "", ""),
			tx("3", "2026-08-11", "Shopping", "Zara", "-30", "", ""),
			tx("4", "2026-09-15", "Investment Purchase", "Fineco", "-1000", "", ""),
			tx("5", "2026-09-28", "Dividend Income", "Fineco", "12.34", "", ""),
			tx("6", "2026-09-05", "Giroconto", "", "-500", "", ""),
			tx("7", "2026-09-01", "Salary", "Azienda", "2000", "", ""),
			tx("8", "2026-09-10", "Utilities", "Enel", "-90", "2026-07-01", "2026-09-30"),
		}}}, keys: fixedKey(key), policy: policy.FixtureDevelopment()}
}

func TestQueryShoppingSpending(t *testing.T) {
	r, err := queryAnalytics(t).Query(context.Background(), domain.Query{Mode: "aggregate", FromMonth: "2026-09",
		Categories: []string{"shopping"}, Sign: "expense"})
	if err != nil || r.Totals.ExpensesMinor != 12550 || r.Totals.Count != 2 || r.Matched != 2 {
		t.Fatalf("r=%+v err=%v", r.Totals, err)
	}
}

func TestQueryListsInvestmentsOfTheMonth(t *testing.T) {
	r, err := queryAnalytics(t).Query(context.Background(), domain.Query{Mode: "transactions", FromMonth: "2026-09",
		Groups: []string{"investments"}})
	if err != nil || len(r.Transactions) != 2 || r.Transactions[0].ID != "5" || r.Totals.NetMinor != -98766 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
}

func TestQueryRatioRulesExcludeTransfersAndSpreadCompetenza(t *testing.T) {
	a := queryAnalytics(t)
	r, err := a.Query(context.Background(), domain.Query{Mode: "aggregate", FromMonth: "2026-07", ToMonth: "2026-09", GroupBy: "month"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, row := range r.Rows {
		got[row.Key] = row.NetMinor
	}
	// July and August get a third of the Enel bill each; September has salary,
	// shopping and the last third; transfers and investments are out.
	if got["2026-07"] != -3000 || got["2026-08"] != -6000 || got["2026-09"] != 200000-12550-3000 {
		t.Fatalf("%v", got)
	}
	raw, _ := a.Query(context.Background(), domain.Query{Mode: "aggregate", FromMonth: "2026-09", Rules: "raw"})
	if raw.Totals.NetMinor != 200000-12550-100000+1234-50000-9000 {
		t.Fatalf("raw %+v", raw.Totals)
	}
}

func TestQueryCategoriesAndGroupBy(t *testing.T) {
	a := queryAnalytics(t)
	c, err := a.Query(context.Background(), domain.Query{Mode: "categories", FromMonth: "2026-09", Rules: "raw"})
	if err != nil || len(c.Categories) != 6 || c.Categories[0].Category != "Shopping" || c.Categories[0].Count != 2 {
		t.Fatalf("%+v %v", c.Categories, err)
	}
	m, _ := a.Query(context.Background(), domain.Query{Mode: "aggregate", FromMonth: "2026-08", ToMonth: "2026-09",
		Categories: []string{"Shopping"}, GroupBy: "merchant"})
	if len(m.Rows) != 2 || m.Rows[0].Key != "Zara" || m.Rows[0].ExpensesMinor != 11000 || m.Rows[0].Count != 2 {
		t.Fatalf("%+v", m.Rows)
	}
}

func TestQueryValidation(t *testing.T) {
	a := queryAnalytics(t)
	for _, q := range []domain.Query{
		{Mode: "delete", FromMonth: "2026-09"}, {Mode: "aggregate", FromMonth: "settembre"},
		{Mode: "aggregate", FromMonth: "2026-09", ToMonth: "2026-08"}, {Mode: "aggregate", FromMonth: "2020-01", ToMonth: "2026-09"},
		{Mode: "aggregate", FromMonth: "2026-09", Groups: []string{"casino"}},
	} {
		if _, err := a.Query(context.Background(), q); err == nil {
			t.Errorf("accettata: %+v", q)
		}
	}
	_, err := a.Query(context.Background(), domain.Query{Mode: "transactions", FromMonth: "2026-09", Text: "di"})
	if err == nil || !strings.Contains(err.Error(), "searchable") {
		t.Fatalf("%v", err)
	}
}

func TestMerchantKeyFallsBackToDescription(t *testing.T) {
	if got := merchantKey(domain.Transaction{CleanDescription: "Trattoria Da Mario"}); got != "Trattoria Da Mario" {
		t.Fatal(got)
	}
	if got := merchantKey(domain.Transaction{MerchantName: "Zara", CleanDescription: "ZARA 123"}); got != "Zara" {
		t.Fatal(got)
	}
	if got := merchantKey(domain.Transaction{}); got != "(senza esercente)" {
		t.Fatal(got)
	}
}
