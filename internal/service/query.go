// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/marcosperanza/ratio-mcp/internal/domain"
)

const (
	maxQueryMonths = 36
	maxListLimit   = 100
)

// categoryGroup classifies a category the way the app's statistics do.
func categoryGroup(category string) string {
	switch {
	case isExcludedTransfer(category):
		return "transfers"
	case isExcludedInvestment(category):
		return "investments"
	}
	return "spending"
}

func validateQuery(q *domain.Query) (time.Time, time.Time, error) {
	switch q.Mode {
	case "categories", "transactions", "aggregate":
	default:
		return time.Time{}, time.Time{}, errors.New("mode must be categories, transactions or aggregate")
	}
	if q.ToMonth == "" {
		q.ToMonth = q.FromMonth
	}
	start, _, err := monthBounds(q.FromMonth)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("fromMonth: %w", err)
	}
	last, end, err := monthBounds(q.ToMonth)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("toMonth: %w", err)
	}
	if last.Before(start) {
		return time.Time{}, time.Time{}, errors.New("toMonth is before fromMonth")
	}
	if months := (last.Year()-start.Year())*12 + int(last.Month()-start.Month()) + 1; months > maxQueryMonths {
		return time.Time{}, time.Time{}, fmt.Errorf("period longer than %d months", maxQueryMonths)
	}
	switch q.Sign {
	case "":
		q.Sign = "all"
	case "all", "income", "expense":
	default:
		return time.Time{}, time.Time{}, errors.New("sign must be all, income or expense")
	}
	switch q.Rules {
	case "":
		q.Rules = "ratio"
		if q.Mode == "transactions" {
			q.Rules = "raw" // a list shows every movement unless asked otherwise
		}
	case "ratio", "raw":
	default:
		return time.Time{}, time.Time{}, errors.New("rules must be ratio or raw")
	}
	switch q.GroupBy {
	case "":
		q.GroupBy = "none"
	case "none", "category", "month", "merchant":
	default:
		return time.Time{}, time.Time{}, errors.New("groupBy must be none, category, month or merchant")
	}
	for _, g := range q.Groups {
		if g != "transfers" && g != "investments" && g != "spending" {
			return time.Time{}, time.Time{}, fmt.Errorf("unknown group %q", g)
		}
	}
	if q.Limit <= 0 || q.Limit > maxListLimit {
		q.Limit = 50
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	if len(q.Text) > 80 {
		return time.Time{}, time.Time{}, errors.New("text longer than 80 characters")
	}
	return start, end, nil
}

// slice is the part of a transaction that falls in one month.
type slice struct {
	month  time.Time
	amount int64
}

// slices returns the monthly parts of a transaction inside [start, end):
// with rules "ratio" the competenza spreading, with "raw" its booking month.
func slices(t domain.Transaction, rules string, start, end time.Time) []slice {
	if rules == "raw" {
		if t.BookingDate.Before(start) || !t.BookingDate.Before(end) {
			return nil
		}
		m := time.Date(t.BookingDate.Year(), t.BookingDate.Month(), 1, 0, 0, 0, 0, time.UTC)
		return []slice{{m, t.AmountMinor}}
	}
	var out []slice
	for m := start; m.Before(end); m = m.AddDate(0, 1, 0) {
		if share, ok := competenzaShare(t, m); ok {
			out = append(out, slice{m, share})
		}
	}
	return out
}

func queryMatches(q domain.Query, t domain.Transaction, terms []string, normalized string) bool {
	group := categoryGroup(t.Category)
	if q.Rules == "ratio" && group != "spending" && len(q.Groups) == 0 && len(q.Categories) == 0 {
		return false // the app's statistics leave transfers and investments out
	}
	if len(q.Groups) > 0 {
		found := false
		for _, g := range q.Groups {
			found = found || g == group
		}
		if !found {
			return false
		}
	}
	if len(q.Categories) > 0 && !inCategories(t.Category, q.Categories) && !inCategories(t.CategoryName, q.Categories) {
		return false
	}
	if q.Text != "" {
		haystack := normalizeSearchText(strings.Join([]string{t.Description, t.CleanDescription, t.MerchantName, t.Category, t.CategoryName}, " "))
		if searchScore(haystack, normalized, terms) == 0 {
			return false
		}
	}
	return true
}

func signAllows(sign string, amount int64) bool {
	switch sign {
	case "income":
		return amount > 0
	case "expense":
		return amount < 0
	}
	return true
}

func absMinor(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// Query answers ratio_categories, ratio_transactions and ratio_aggregate.
// Figures are exact integer minor units computed here; the model only
// chooses the filters.
func (a *Analytics) Query(ctx context.Context, q domain.Query) (domain.QueryResult, error) {
	if err := a.policy.Authorize("ratio_query"); err != nil {
		return domain.QueryResult{}, err
	}
	start, end, err := validateQuery(&q)
	if err != nil {
		return domain.QueryResult{}, err
	}
	var terms []string
	normalized := ""
	if q.Text = strings.TrimSpace(q.Text); q.Text != "" {
		normalized = normalizeSearchText(q.Text)
		if terms = meaningfulSearchTerms(normalized); len(terms) == 0 {
			return domain.QueryResult{}, errors.New("text has no searchable terms")
		}
	}
	snapshot, transactions, err := a.load(ctx)
	if err != nil {
		return domain.QueryResult{}, err
	}
	out := domain.QueryResult{Status: "ok", WorkspaceID: snapshot.WorkspaceID, Query: q, Coverage: snapshot.Coverage,
		RulesVersion: domain.RulesVersion}
	if !coverageIncludes(snapshot.Coverage, start, end) {
		out.Status = "incomplete_data"
	}
	rows := map[string]*domain.AggregateRow{}
	counts := map[string]int{}
	var listed []domain.Transaction
	for _, t := range transactions {
		parts := slices(t, q.Rules, start, end)
		if len(parts) == 0 {
			continue
		}
		if q.Mode == "categories" {
			counts[t.Category]++
			continue
		}
		if !queryMatches(q, t, terms, normalized) {
			continue
		}
		if !signAllows(q.Sign, t.AmountMinor) {
			continue
		}
		if (q.MinMinor > 0 && absMinor(t.AmountMinor) < q.MinMinor) || (q.MaxMinor > 0 && absMinor(t.AmountMinor) > q.MaxMinor) {
			continue
		}
		if out.Currency == "" {
			out.Currency = t.Currency
		} else if out.Currency != t.Currency {
			return domain.QueryResult{}, errors.New("multiple currencies require conversion rules; none are configured")
		}
		out.Matched++
		if q.Mode == "transactions" {
			listed = append(listed, t)
			continue
		}
		for _, p := range parts {
			key := "totale"
			switch q.GroupBy {
			case "category":
				key = strings.TrimSpace(t.Category)
				if key == "" {
					key = "Uncategorized"
				}
			case "month":
				key = p.month.Format("2006-01")
			case "merchant":
				key = merchantKey(t)
			}
			for _, r := range []*domain.AggregateRow{rowFor(rows, key), &out.Totals} {
				if p.amount >= 0 {
					r.IncomeMinor += p.amount
				} else {
					r.ExpensesMinor += -p.amount
				}
				r.NetMinor += p.amount
			}
		}
		rowFor(rows, groupKeyForCount(q.GroupBy, t, parts)).Count++
		out.Totals.Count++
	}
	if out.Currency == "" {
		out.Currency = "EUR"
	}
	switch q.Mode {
	case "categories":
		names := categoryNames(transactions)
		for c, n := range counts {
			out.Categories = append(out.Categories, domain.CategoryCount{Category: c, CategoryName: names[c], Group: categoryGroup(c), Count: n})
		}
		sort.Slice(out.Categories, func(i, j int) bool {
			if out.Categories[i].Count != out.Categories[j].Count {
				return out.Categories[i].Count > out.Categories[j].Count
			}
			return out.Categories[i].Category < out.Categories[j].Category
		})
		out.Matched = len(out.Categories)
	case "transactions":
		sort.Slice(listed, func(i, j int) bool {
			if !listed[i].BookingDate.Equal(listed[j].BookingDate) {
				return listed[i].BookingDate.After(listed[j].BookingDate)
			}
			return listed[i].ID < listed[j].ID
		})
		for i := q.Offset; i < len(listed) && i < q.Offset+q.Limit; i++ {
			out.Transactions = append(out.Transactions, a.policy.View(listed[i]))
		}
		// Totals of the whole matched set (not only the returned page).
		for _, t := range listed {
			if t.AmountMinor >= 0 {
				out.Totals.IncomeMinor += t.AmountMinor
			} else {
				out.Totals.ExpensesMinor += -t.AmountMinor
			}
			out.Totals.NetMinor += t.AmountMinor
			out.Totals.Count++
		}
	case "aggregate":
		if q.GroupBy != "none" {
			for _, r := range rows {
				out.Rows = append(out.Rows, *r)
			}
			sort.Slice(out.Rows, func(i, j int) bool {
				if q.GroupBy == "month" {
					return out.Rows[i].Key < out.Rows[j].Key
				}
				if absMinor(out.Rows[i].NetMinor) != absMinor(out.Rows[j].NetMinor) {
					return absMinor(out.Rows[i].NetMinor) > absMinor(out.Rows[j].NetMinor)
				}
				return out.Rows[i].Key < out.Rows[j].Key
			})
			if len(out.Rows) > q.Limit {
				out.Rows = out.Rows[:q.Limit]
			}
		}
	}
	return out, nil
}

func rowFor(rows map[string]*domain.AggregateRow, key string) *domain.AggregateRow {
	r, ok := rows[key]
	if !ok {
		r = &domain.AggregateRow{Key: key}
		rows[key] = r
	}
	return r
}

// groupKeyForCount is the row a transaction is counted in: its first slice
// for month grouping, its category or merchant otherwise.
func groupKeyForCount(groupBy string, t domain.Transaction, parts []slice) string {
	switch groupBy {
	case "category":
		if k := strings.TrimSpace(t.Category); k != "" {
			return k
		}
		return "Uncategorized"
	case "month":
		return parts[0].month.Format("2006-01")
	case "merchant":
		return merchantKey(t)
	}
	return "totale"
}

// merchantKey names who was paid: the merchant when the app extracted it,
// otherwise the clean description (the bank text is the last resort).
func merchantKey(t domain.Transaction) string {
	for _, k := range []string{t.MerchantName, t.CleanDescription, t.Description} {
		if k = strings.TrimSpace(k); k != "" {
			return k
		}
	}
	return "(senza esercente)"
}
