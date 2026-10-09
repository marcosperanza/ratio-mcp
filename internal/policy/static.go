// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package policy

import (
	"errors"
	"fmt"

	"github.com/marcosperanza/ratio-mcp/internal/domain"
)

const (
	MonthSummaryTool = "ratio_get_month_summary"
	SearchTool       = "ratio_search_transactions"
	QueryTool        = "ratio_query"
)

type Static struct {
	allowed     map[string]struct{}
	searchLimit int
	showDetails bool
}

func FixtureDevelopment() *Static {
	return &Static{
		allowed: map[string]struct{}{
			MonthSummaryTool: {},
			SearchTool:       {},
			QueryTool:        {},
		},
		searchLimit: 5,
		showDetails: true,
	}
}

func (p *Static) Authorize(toolName string) error {
	if _, ok := p.allowed[toolName]; !ok {
		return fmt.Errorf("tool %q is not allowed by local policy", toolName)
	}
	return nil
}

func (p *Static) SearchLimit() int {
	if p.searchLimit < 1 {
		return 1
	}
	return p.searchLimit
}

func (p *Static) View(transaction domain.Transaction) domain.TransactionView {
	view := domain.TransactionView{
		ID:           transaction.ID,
		BookingDate:  transaction.BookingDate.Format("2006-01-02"),
		AmountMinor:  transaction.AmountMinor,
		Currency:     transaction.Currency,
		Category:     transaction.Category,
		CategoryName: transaction.CategoryName,
	}
	if p.showDetails {
		view.Merchant = transaction.MerchantName
		view.Description = transaction.Description
		if transaction.CleanDescription != "" {
			view.Description = transaction.CleanDescription
		}
	}
	if !transaction.CompetenzaFrom.IsZero() {
		view.CompetenzaFrom = transaction.CompetenzaFrom.Format("2006-01-02")
	}
	if !transaction.CompetenzaTo.IsZero() {
		view.CompetenzaTo = transaction.CompetenzaTo.Format("2006-01-02")
	}
	return view
}

func Validate(policy *Static) error {
	if policy == nil {
		return errors.New("policy is required")
	}
	return nil
}
