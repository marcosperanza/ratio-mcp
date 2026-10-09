// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package domain

import "time"

// RulesVersion names the accounting rule of every figure: the Ratio app's
// statistics exclusions (transfers and investments) and accrual spreading
// (competenza), app commit 83b2601.
const RulesVersion = "ratio-stats-83b2601+competenza"

type EncryptedTransaction struct {
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspaceId"`
	BookingDate   string `json:"bookingDate"`
	EncryptedBlob string `json:"encryptedBlob"`
	EncryptionIV  string `json:"encryptionIv"`
	ServerVersion int64  `json:"serverVersion"`
	// PlainAmount/PlainCurrency are the server's plaintext columns. Ratio keeps
	// them for transactions not yet migrated into the encrypted blob and sets
	// them to NULL afterwards; they are only a fallback for a blob without amount.
	PlainAmount   string `json:"plainAmount,omitempty"`
	PlainCurrency string `json:"plainCurrency,omitempty"`
	// CompetenzaFrom/To (YYYY-MM-DD, app time zone) is the accrual period.
	CompetenzaFrom string `json:"competenzaFrom,omitempty"`
	CompetenzaTo   string `json:"competenzaTo,omitempty"`
}

type Transaction struct {
	CategoryName     string
	ID               string
	WorkspaceID      string
	BookingDate      time.Time
	AmountMinor      int64
	Currency         string
	Description      string
	CleanDescription string
	MerchantName     string
	Category         string
	ServerVersion    int64
	// CompetenzaFrom/To are zero when the transaction has no accrual period.
	CompetenzaFrom time.Time
	CompetenzaTo   time.Time
}

type Coverage struct {
	CoverageFrom   string `json:"coverageFrom"`
	CoverageTo     string `json:"coverageTo"`
	IsComplete     bool   `json:"isComplete"`
	LastSyncedAt   string `json:"lastSyncedAt"`
	SourceRevision string `json:"sourceRevision"`
	// InferredCurrency counts transactions whose missing currency was taken
	// from the workspace's single currency.
	InferredCurrency int `json:"inferredCurrency,omitempty"`
}

type Snapshot struct {
	WorkspaceID  string
	Transactions []EncryptedTransaction
	Coverage     Coverage
}

type Workspace struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Role      string `json:"role"`
	IsDefault bool   `json:"isDefault"`
}

type WorkspaceList struct {
	Workspaces []Workspace `json:"workspaces"`
}

type CategorySummary struct {
	CategoryName     string `json:"categoryName,omitempty"`
	Category         string `json:"category"`
	ExpensesMinor    int64  `json:"expensesMinor"`
	TransactionCount int    `json:"transactionCount"`
}

type MonthSummary struct {
	Status                string `json:"status"`
	WorkspaceID           string `json:"workspaceId"`
	WorkspaceName         string `json:"workspaceName,omitempty"`
	Month                 string `json:"month"`
	Currency              string `json:"currency"`
	IncomeMinor           int64  `json:"incomeMinor"`
	ExpensesMinor         int64  `json:"expensesMinor"`
	BalanceMinor          int64  `json:"balanceMinor"`
	TransactionCount      int    `json:"transactionCount"`
	ExcludedTransferCount int    `json:"excludedTransferCount"`
	// ExcludedInvestmentCount: investment purchases, sales, dividends and
	// capital gains, left out of income and spending as in the app.
	ExcludedInvestmentCount int               `json:"excludedInvestmentCount"`
	Categories              []CategorySummary `json:"categories"`
	Coverage                Coverage          `json:"coverage"`
	RulesVersion            string            `json:"rulesVersion"`
	AuthoritativeText       string            `json:"authoritativeText"`
}

type TransactionView struct {
	CategoryName string `json:"categoryName,omitempty"`
	ID           string `json:"id"`
	BookingDate  string `json:"bookingDate"`
	AmountMinor  int64  `json:"amountMinor"`
	Currency     string `json:"currency"`
	Category     string `json:"category"`
	Merchant     string `json:"merchant,omitempty"`
	Description  string `json:"description,omitempty"`
	// Accrual period when set (YYYY-MM-DD).
	CompetenzaFrom string `json:"competenzaFrom,omitempty"`
	CompetenzaTo   string `json:"competenzaTo,omitempty"`
}

type SearchResult struct {
	Status            string            `json:"status"`
	WorkspaceID       string            `json:"workspaceId"`
	WorkspaceName     string            `json:"workspaceName,omitempty"`
	Month             string            `json:"month"`
	Query             string            `json:"query"`
	Matches           []TransactionView `json:"matches"`
	ReturnedCount     int               `json:"returnedCount"`
	TotalMatches      int               `json:"totalMatches"`
	Truncated         bool              `json:"truncated"`
	Coverage          Coverage          `json:"coverage"`
	RulesVersion      string            `json:"rulesVersion"`
	AuthoritativeText string            `json:"authoritativeText"`
}

// Query is the generic read primitive behind ratio_categories,
// ratio_transactions and ratio_aggregate: the model decides what to ask,
// the service computes exact figures. Periods are whole months.
type Query struct {
	Mode       string   `json:"mode"` // categories | transactions | aggregate
	FromMonth  string   `json:"fromMonth"`
	ToMonth    string   `json:"toMonth"`
	Categories []string `json:"categories,omitempty"`
	// Groups: "transfers", "investments", "spending" (everything else).
	Groups   []string `json:"groups,omitempty"`
	Text     string   `json:"text,omitempty"`
	Sign     string   `json:"sign,omitempty"` // all | income | expense
	MinMinor int64    `json:"minMinor,omitempty"`
	MaxMinor int64    `json:"maxMinor,omitempty"`
	// Rules: "ratio" (the app's statistics: transfers and investments out,
	// competenza spread) or "raw" (every movement on its booking date).
	Rules   string `json:"rules,omitempty"`
	GroupBy string `json:"groupBy,omitempty"` // none | category | month | merchant
	Limit   int    `json:"limit,omitempty"`
	Offset  int    `json:"offset,omitempty"`
}

type CategoryCount struct {
	CategoryName string `json:"categoryName,omitempty"`
	Category     string `json:"category"`
	Group        string `json:"group"`
	Count        int    `json:"count"`
}

type AggregateRow struct {
	Key           string `json:"key"`
	IncomeMinor   int64  `json:"incomeMinor"`
	ExpensesMinor int64  `json:"expensesMinor"`
	NetMinor      int64  `json:"netMinor"`
	Count         int    `json:"count"`
}

type QueryResult struct {
	Status        string            `json:"status"`
	WorkspaceID   string            `json:"workspaceId"`
	WorkspaceName string            `json:"workspaceName,omitempty"`
	Query         Query             `json:"query"`
	Currency      string            `json:"currency"`
	Categories    []CategoryCount   `json:"categories,omitempty"`
	Transactions  []TransactionView `json:"transactions,omitempty"`
	Matched       int               `json:"matched"`
	Totals        AggregateRow      `json:"totals"`
	Rows          []AggregateRow    `json:"rows,omitempty"`
	Coverage      Coverage          `json:"coverage"`
	RulesVersion  string            `json:"rulesVersion"`
}

type EncryptedCategoryCatalog struct {
	EncryptedBlob string `json:"encryptedBlob"`
	EncryptionIV  string `json:"encryptionIv"`
	Version       int    `json:"version"`
}
