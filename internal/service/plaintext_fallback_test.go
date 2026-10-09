// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/marcosperanza/ratio-mcp/internal/domain"
	"github.com/marcosperanza/ratio-mcp/internal/policy"
)

func sealBlob(t *testing.T, key []byte, plaintext string) (string, string) {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	iv := make([]byte, gcm.NonceSize())
	for i := range iv {
		iv[i] = byte(i + 1)
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nil, iv, []byte(plaintext), nil)),
		base64.StdEncoding.EncodeToString(iv)
}

func encrypted(t *testing.T, key []byte, blob, plainAmount, plainCurrency string) domain.EncryptedTransaction {
	t.Helper()
	ct, iv := sealBlob(t, key, blob)
	return domain.EncryptedTransaction{ID: "tx", WorkspaceID: "ws", BookingDate: "2026-09-08",
		EncryptedBlob: ct, EncryptionIV: iv, PlainAmount: plainAmount, PlainCurrency: plainCurrency}
}

// Ratio keeps amount and currency in plaintext columns until a transaction is
// migrated into the blob; the app falls back to them and so must the MCP.
func TestNotYetMigratedTransactionUsesPlaintextColumns(t *testing.T) {
	key := make([]byte, 32)
	tx, err := decryptTransaction(encrypted(t, key, `{"description":"Spesa"}`, "-85.40", "EUR"), key)
	if err != nil || tx.AmountMinor != -8540 || tx.Currency != "EUR" {
		t.Fatalf("tx=%+v err=%v", tx, err)
	}
}

func TestBlobWinsOverPlaintextColumns(t *testing.T) {
	key := make([]byte, 32)
	tx, err := decryptTransaction(encrypted(t, key, `{"description":"x","amount":-12.5,"currency":"usd"}`, "-99.99", "EUR"), key)
	if err != nil || tx.AmountMinor != -1250 || tx.Currency != "USD" {
		t.Fatalf("tx=%+v err=%v", tx, err)
	}
}

func TestMissingAmountEverywhereStillFails(t *testing.T) {
	key := make([]byte, 32)
	_, err := decryptTransaction(encrypted(t, key, `{"description":"x","currency":"EUR"}`, "", ""), key)
	if err == nil || !strings.Contains(err.Error(), "no amount") {
		t.Fatalf("importo inventato o errore diverso: %v", err)
	}
	// A single transaction without currency is flagged (load decides).
	_, err = decryptTransaction(encrypted(t, key, `{"description":"x"}`, "-1.00", ""), key)
	if !errors.Is(err, errMissingCurrency) {
		t.Fatalf("valuta mancante non segnalata: %v", err)
	}
}

type snapshotRepo struct{ snapshot domain.Snapshot }

func (r snapshotRepo) Snapshot(context.Context) (domain.Snapshot, error) { return r.snapshot, nil }

type fixedKey []byte

func (k fixedKey) MasterKey(context.Context, string) ([]byte, error) {
	return append([]byte(nil), k...), nil
}

func loadWith(t *testing.T, txs ...domain.EncryptedTransaction) (domain.Snapshot, []domain.Transaction, error) {
	t.Helper()
	key := make([]byte, 32)
	a := &Analytics{repository: snapshotRepo{domain.Snapshot{WorkspaceID: "ws", Transactions: txs}}, keys: fixedKey(key),
		policy: policy.FixtureDevelopment()}
	return a.load(context.Background())
}

// Seen in production (2026-10-03): 4 of 3240 rows carry the amount but no
// currency. A workspace with one single currency gives it to them, and the
// coverage says how many were inferred.
func TestMissingCurrencyInheritsTheWorkspaceCurrency(t *testing.T) {
	key := make([]byte, 32)
	snap, txs, err := loadWith(t,
		encrypted(t, key, `{"description":"a","amount":-10,"currency":"EUR"}`, "", ""),
		encrypted(t, key, `{"description":"b","amount":-2.5}`, "", ""),
		encrypted(t, key, `{"description":"c","amount":3,"currency":"eur"}`, "", ""))
	if err != nil || len(txs) != 3 || txs[1].Currency != "EUR" || txs[1].AmountMinor != -250 || snap.Coverage.InferredCurrency != 1 {
		t.Fatalf("snap=%+v txs=%+v err=%v", snap.Coverage, txs, err)
	}
}

func TestMissingCurrencyWithMixedCurrenciesFails(t *testing.T) {
	key := make([]byte, 32)
	_, _, err := loadWith(t,
		encrypted(t, key, `{"description":"a","amount":-10,"currency":"EUR"}`, "", ""),
		encrypted(t, key, `{"description":"b","amount":-1,"currency":"USD"}`, "", ""),
		encrypted(t, key, `{"description":"c","amount":-2.5}`, "", ""))
	if err == nil || !strings.Contains(err.Error(), "2 currencies") {
		t.Fatalf("valuta indovinata tra valute diverse: %v", err)
	}
}

// The month summary follows the Ratio app's statistics: transfers and
// investments are neither income nor spending.
func TestSummaryExcludesTransfersAndInvestmentsLikeTheApp(t *testing.T) {
	key := make([]byte, 32)
	tx := func(cat string, amount string) domain.EncryptedTransaction {
		return encrypted(t, key, `{"description":"x","category":"`+cat+`","amount":`+amount+`,"currency":"EUR"}`, "", "")
	}
	a := &Analytics{repository: snapshotRepo{domain.Snapshot{WorkspaceID: "ws",
		Coverage: domain.Coverage{CoverageFrom: "2026-01-01", CoverageTo: "2026-12-31", IsComplete: true},
		Transactions: []domain.EncryptedTransaction{
			tx("Salary", "2000"), tx("Groceries", "-150.50"),
			tx("Giroconto", "-500"), tx("Credit Card Payment", "-300"), tx("Internal Transfer", "500"),
			tx("Investment Purchase", "-1000"), tx("Dividend Income", "12.34"),
		}}}, keys: fixedKey(key), policy: policy.FixtureDevelopment()}
	s, err := a.MonthSummary(context.Background(), "2026-09")
	if err != nil {
		t.Fatal(err)
	}
	if s.IncomeMinor != 200000 || s.ExpensesMinor != 15050 || s.BalanceMinor != 184950 || s.TransactionCount != 2 ||
		s.ExcludedTransferCount != 3 || s.ExcludedInvestmentCount != 2 || s.RulesVersion != domain.RulesVersion {
		t.Fatalf("%+v", s)
	}
	if !strings.Contains(s.AuthoritativeText, "3 giroconti/trasferimenti e 2 movimenti di investimento") {
		t.Fatal(s.AuthoritativeText)
	}
}
