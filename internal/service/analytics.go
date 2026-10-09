// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/marcosperanza/ratio-mcp/internal/core"
	"github.com/marcosperanza/ratio-mcp/internal/cryptoutil"
	"github.com/marcosperanza/ratio-mcp/internal/domain"
)

type Analytics struct {
	repository core.TransactionRepository
	keys       core.KeyProvider
	policy     core.Policy
}

func NewAnalytics(repository core.TransactionRepository, keys core.KeyProvider, policy core.Policy) (*Analytics, error) {
	if repository == nil || keys == nil || policy == nil {
		return nil, errors.New("repository, key provider and policy are required")
	}
	return &Analytics{repository: repository, keys: keys, policy: policy}, nil
}

type transactionBlob struct {
	Description      string       `json:"description"`
	CleanDescription string       `json:"cleanDescription"`
	MerchantName     string       `json:"merchantName"`
	Category         string       `json:"category"`
	Amount           *json.Number `json:"amount"`
	Currency         string       `json:"currency"`
}

func (a *Analytics) MonthSummary(ctx context.Context, month string) (domain.MonthSummary, error) {
	if err := a.policy.Authorize("ratio_get_month_summary"); err != nil {
		return domain.MonthSummary{}, err
	}
	start, end, err := monthBounds(month)
	if err != nil {
		return domain.MonthSummary{}, err
	}
	snapshot, transactions, err := a.load(ctx)
	if err != nil {
		return domain.MonthSummary{}, err
	}

	type categoryTotals struct {
		amount int64
		count  int
	}
	categories := make(map[string]categoryTotals)
	var income, expenses int64
	count := 0
	excluded, investments := 0, 0
	currency := ""
	for _, transaction := range transactions {
		// Accrual (competenza) as in the app: the amount is spread evenly over
		// the months of its period; without one, the booking month.
		share, ok := competenzaShare(transaction, start)
		if !ok {
			continue
		}
		transaction.AmountMinor = share
		if currency == "" {
			currency = transaction.Currency
		} else if transaction.Currency != currency {
			return domain.MonthSummary{}, errors.New("multiple currencies require conversion rules; none are configured")
		}
		if isExcludedTransfer(transaction.Category) {
			excluded++
			continue
		}
		if isExcludedInvestment(transaction.Category) {
			investments++
			continue
		}
		count++
		if transaction.AmountMinor >= 0 {
			income += transaction.AmountMinor
			continue
		}
		amount := -transaction.AmountMinor
		expenses += amount
		category := strings.TrimSpace(transaction.Category)
		if category == "" {
			category = "Uncategorized"
		}
		total := categories[category]
		total.amount += amount
		total.count++
		categories[category] = total
	}
	if currency == "" {
		currency = "EUR"
	}

	names := categoryNames(transactions)
	categoryList := make([]domain.CategorySummary, 0, len(categories))
	for category, total := range categories {
		categoryList = append(categoryList, domain.CategorySummary{
			Category: category, CategoryName: names[category], ExpensesMinor: total.amount, TransactionCount: total.count,
		})
	}
	sort.Slice(categoryList, func(i, j int) bool {
		if categoryList[i].ExpensesMinor == categoryList[j].ExpensesMinor {
			return categoryList[i].Category < categoryList[j].Category
		}
		return categoryList[i].ExpensesMinor > categoryList[j].ExpensesMinor
	})

	complete := coverageIncludes(snapshot.Coverage, start, end)
	status := "ok"
	prefix := ""
	if !complete {
		status = "incomplete_data"
		prefix = "Dati parziali. "
	}
	balance := income - expenses
	return domain.MonthSummary{
		Status:                  status,
		WorkspaceID:             snapshot.WorkspaceID,
		Month:                   month,
		Currency:                currency,
		IncomeMinor:             income,
		ExpensesMinor:           expenses,
		BalanceMinor:            balance,
		TransactionCount:        count,
		ExcludedTransferCount:   excluded,
		ExcludedInvestmentCount: investments,
		Categories:              categoryList,
		Coverage:                snapshot.Coverage,
		RulesVersion:            domain.RulesVersion,
		AuthoritativeText: fmt.Sprintf(
			"%s%s: entrate %s %s, uscite %s %s, saldo del periodo %s %s; %d movimenti conteggiati; esclusi come nelle statistiche di Ratio %d giroconti/trasferimenti e %d movimenti di investimento.",
			prefix, month, currency, formatMinor(income), currency, formatMinor(expenses), currency, formatMinor(balance), count, excluded, investments,
		),
	}, nil
}

func (a *Analytics) Search(ctx context.Context, month, query string) (domain.SearchResult, error) {
	if err := a.policy.Authorize("ratio_search_transactions"); err != nil {
		return domain.SearchResult{}, err
	}
	start, end, err := monthBounds(month)
	if err != nil {
		return domain.SearchResult{}, err
	}
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 || len([]rune(query)) > 80 {
		return domain.SearchResult{}, errors.New("query must contain between 2 and 80 characters")
	}
	normalizedQuery := normalizeSearchText(query)
	queryTerms := meaningfulSearchTerms(normalizedQuery)
	if len(queryTerms) == 0 {
		return domain.SearchResult{}, errors.New("query has no searchable terms")
	}
	snapshot, transactions, err := a.load(ctx)
	if err != nil {
		return domain.SearchResult{}, err
	}
	type scoredTransaction struct {
		transaction domain.Transaction
		score       int
	}
	matches := make([]scoredTransaction, 0)
	for _, transaction := range transactions {
		if transaction.BookingDate.Before(start) || !transaction.BookingDate.Before(end) {
			continue
		}
		haystack := normalizeSearchText(strings.Join([]string{
			transaction.Description,
			transaction.CleanDescription,
			transaction.MerchantName,
			transaction.CategoryName,
			transaction.Category,
		}, " "))
		if score := searchScore(haystack, normalizedQuery, queryTerms); score > 0 {
			matches = append(matches, scoredTransaction{transaction: transaction, score: score})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		if matches[i].transaction.BookingDate.Equal(matches[j].transaction.BookingDate) {
			return matches[i].transaction.ID < matches[j].transaction.ID
		}
		return matches[i].transaction.BookingDate.After(matches[j].transaction.BookingDate)
	})
	total := len(matches)
	limit := a.policy.SearchLimit()
	if len(matches) > limit {
		matches = matches[:limit]
	}
	views := make([]domain.TransactionView, 0, len(matches))
	for _, match := range matches {
		views = append(views, a.policy.View(match.transaction))
	}
	complete := coverageIncludes(snapshot.Coverage, start, end)
	status := "ok"
	prefix := ""
	if !complete {
		status = "incomplete_data"
		prefix = "Dati parziali. "
	}
	return domain.SearchResult{
		Status:            status,
		WorkspaceID:       snapshot.WorkspaceID,
		Month:             month,
		Query:             query,
		Matches:           views,
		ReturnedCount:     len(views),
		TotalMatches:      total,
		Truncated:         total > len(views),
		Coverage:          snapshot.Coverage,
		RulesVersion:      domain.RulesVersion,
		AuthoritativeText: searchAuthoritativeText(prefix, query, month, views, total),
	}, nil
}

var searchStopWords = map[string]struct{}{
	"a": {}, "al": {}, "alla": {}, "alle": {}, "allo": {}, "ai": {}, "agli": {},
	"con": {}, "da": {}, "dal": {}, "dalla": {}, "dalle": {}, "dallo": {},
	"del": {}, "della": {}, "delle": {}, "dello": {}, "dei": {}, "degli": {},
	"di": {}, "e": {}, "il": {}, "in": {}, "la": {}, "le": {}, "lo": {},
	"nel": {}, "nella": {}, "nelle": {}, "nello": {}, "per": {}, "su": {},
	"tra": {}, "un": {}, "una": {}, "uno": {},
	"cerca": {}, "cercare": {}, "mostra": {}, "mostrami": {}, "trova": {}, "trovami": {},
	"movimento": {}, "movimenti": {}, "transazione": {}, "transazioni": {},
	"gennaio": {}, "febbraio": {}, "marzo": {}, "aprile": {}, "maggio": {}, "giugno": {},
	"luglio": {}, "agosto": {}, "settembre": {}, "ottobre": {}, "novembre": {}, "dicembre": {},
	"an": {}, "and": {}, "during": {}, "find": {}, "for": {}, "from": {},
	"of": {}, "on": {}, "search": {}, "show": {}, "the": {}, "transaction": {}, "transactions": {},
}

func normalizeSearchText(value string) string {
	var normalized strings.Builder
	lastWasSpace := true
	for _, character := range strings.ToLower(value) {
		character = foldSearchRune(character)
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			normalized.WriteRune(character)
			lastWasSpace = false
			continue
		}
		if !lastWasSpace {
			normalized.WriteByte(' ')
			lastWasSpace = true
		}
	}
	return strings.TrimSpace(normalized.String())
}

func foldSearchRune(character rune) rune {
	switch character {
	case 'à', 'á', 'â', 'ä', 'ã', 'å':
		return 'a'
	case 'è', 'é', 'ê', 'ë':
		return 'e'
	case 'ì', 'í', 'î', 'ï':
		return 'i'
	case 'ò', 'ó', 'ô', 'ö', 'õ':
		return 'o'
	case 'ù', 'ú', 'û', 'ü':
		return 'u'
	default:
		return character
	}
}

func meaningfulSearchTerms(normalizedQuery string) []string {
	terms := make([]string, 0)
	seen := make(map[string]struct{})
	for _, term := range strings.Fields(normalizedQuery) {
		if _, ignored := searchStopWords[term]; ignored || isSearchYear(term) {
			continue
		}
		if _, duplicate := seen[term]; duplicate {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	return terms
}

func isSearchYear(term string) bool {
	if len(term) != 4 || !allDigits(term) {
		return false
	}
	year, err := strconv.Atoi(term)
	return err == nil && year >= 1900 && year <= 2200
}

func searchScore(haystack, normalizedQuery string, queryTerms []string) int {
	if normalizedQuery != "" && strings.Contains(haystack, normalizedQuery) {
		return 100 + len(queryTerms)
	}
	haystackTerms := make(map[string]struct{})
	for _, term := range strings.Fields(haystack) {
		haystackTerms[term] = struct{}{}
	}
	score := 0
	for _, term := range queryTerms {
		if _, found := haystackTerms[term]; found {
			score++
		}
	}
	return score
}

func searchAuthoritativeText(prefix, query, month string, views []domain.TransactionView, total int) string {
	if total == 0 {
		return fmt.Sprintf("%sRicerca %q in %s: nessun risultato.", prefix, query, month)
	}
	resultWord := "risultati"
	returnedWord := "restituiti"
	if total == 1 {
		resultWord = "risultato"
	}
	if len(views) == 1 {
		returnedWord = "restituito"
	}
	parts := make([]string, 0, len(views))
	for _, view := range views {
		label := view.Merchant
		if label == "" {
			label = view.Category
		}
		parts = append(parts, fmt.Sprintf("%s | %s | %s %s | %s", view.BookingDate, label, view.Currency, formatMinor(view.AmountMinor), view.Category))
	}
	return fmt.Sprintf(
		"%sRicerca %q in %s: %d %s, %d %s. Dettagli: %s.",
		prefix, query, month, total, resultWord, len(views), returnedWord, strings.Join(parts, "; "),
	)
}

func (a *Analytics) Validate(ctx context.Context) (int, domain.Coverage, error) {
	snapshot, transactions, err := a.load(ctx)
	if err != nil {
		return 0, domain.Coverage{}, err
	}
	return len(transactions), snapshot.Coverage, nil
}

func (a *Analytics) load(ctx context.Context) (domain.Snapshot, []domain.Transaction, error) {
	snapshot, err := a.repository.Snapshot(ctx)
	if err != nil {
		return domain.Snapshot{}, nil, err
	}
	key, err := a.keys.MasterKey(ctx, snapshot.WorkspaceID)
	if err != nil {
		return domain.Snapshot{}, nil, err
	}
	defer clear(key)
	transactions := make([]domain.Transaction, 0, len(snapshot.Transactions))
	var missingCurrency []int
	currencies := map[string]int{}
	for _, encrypted := range snapshot.Transactions {
		transaction, err := decryptTransaction(encrypted, key)
		if errors.Is(err, errMissingCurrency) {
			missingCurrency = append(missingCurrency, len(transactions))
			transactions = append(transactions, transaction)
			continue
		}
		if err != nil {
			return domain.Snapshot{}, nil, fmt.Errorf("decrypt transaction %q: %w", encrypted.ID, err)
		}
		currencies[transaction.Currency]++
		transactions = append(transactions, transaction)
	}
	if len(missingCurrency) > 0 {
		// A few rows of a workspace may carry the amount without the currency
		// (seen in production: 4 of 3240). When every other transaction of
		// the workspace uses one single currency, that is the currency; the
		// coverage reports how many were inferred. With mixed currencies,
		// fail: never guess between them.
		if len(currencies) != 1 {
			first := transactions[missingCurrency[0]]
			return domain.Snapshot{}, nil, fmt.Errorf("decrypt transaction %q: %w and the workspace has %d currencies",
				first.ID, errMissingCurrency, len(currencies))
		}
		var only string
		for c := range currencies {
			only = c
		}
		for _, i := range missingCurrency {
			transactions[i].Currency = only
		}
		snapshot.Coverage.InferredCurrency = len(missingCurrency)
	}
	if err := a.resolveCategoryNames(ctx, transactions, key); err != nil {
		return domain.Snapshot{}, nil, err
	}
	return snapshot, transactions, nil
}

// competenzaShare returns the part of the amount that belongs to the month
// starting at monthStart, mirroring the app's getCompetenzaSlices
// (lib/competenza-utils.ts): equal parts over every month from the
// competenza start to its end, the booking month when there is none. Minor
// units are integers: the remainder cents go to the first months, so the
// parts always add up to the amount. ok=false when the month gets nothing.
func competenzaShare(t domain.Transaction, monthStart time.Time) (int64, bool) {
	from := t.BookingDate
	if !t.CompetenzaFrom.IsZero() {
		from = t.CompetenzaFrom
	}
	to := from
	if !t.CompetenzaTo.IsZero() {
		to = t.CompetenzaTo
	}
	first := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC)
	target := time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, time.UTC)
	if last.Before(first) {
		last = first
	}
	if target.Before(first) || target.After(last) {
		return 0, false
	}
	months := int64((last.Year()-first.Year())*12 + int(last.Month()-first.Month()) + 1)
	index := int64((target.Year()-first.Year())*12 + int(target.Month()-first.Month()))
	amount := t.AmountMinor
	sign := int64(1)
	if amount < 0 {
		sign, amount = -1, -amount
	}
	part := amount / months
	if index < amount%months {
		part++
	}
	return sign * part, true
}

// errMissingCurrency marks a transaction whose amount is known but whose
// currency is in neither the blob nor the plaintext column.
var errMissingCurrency = errors.New("transaction has no currency")

func decryptTransaction(encrypted domain.EncryptedTransaction, key []byte) (domain.Transaction, error) {
	plaintext, err := cryptoutil.DecryptBase64(encrypted.EncryptedBlob, encrypted.EncryptionIV, key)
	if err != nil {
		return domain.Transaction{}, err
	}
	var blob transactionBlob
	decoder := json.NewDecoder(strings.NewReader(string(plaintext)))
	decoder.UseNumber()
	if err := decoder.Decode(&blob); err != nil {
		return domain.Transaction{}, fmt.Errorf("decode transaction blob: %w", err)
	}
	// Like the Ratio app, the blob wins and the plaintext columns of a not yet
	// migrated transaction are the fallback. With neither, fail: never guess.
	amountText := encrypted.PlainAmount
	if blob.Amount != nil {
		amountText = blob.Amount.String()
	}
	if amountText == "" {
		return domain.Transaction{}, errors.New("transaction has no amount in blob or plaintext columns")
	}
	amountMinor, err := decimalToMinor(amountText)
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("invalid amount: %w", err)
	}
	currencyText := blob.Currency
	if strings.TrimSpace(currencyText) == "" {
		currencyText = encrypted.PlainCurrency
	}
	currency := strings.ToUpper(strings.TrimSpace(currencyText))
	missing := currency == ""
	if !missing && (len(currency) != 3 || !allLetters(currency)) {
		return domain.Transaction{}, fmt.Errorf("invalid currency %q", blob.Currency)
	}
	bookingDate, err := time.Parse("2006-01-02", encrypted.BookingDate)
	if err != nil {
		parsed, rfcErr := time.Parse(time.RFC3339, encrypted.BookingDate)
		if rfcErr != nil {
			return domain.Transaction{}, fmt.Errorf("invalid booking date %q", encrypted.BookingDate)
		}
		bookingDate = parsed
	}
	var competenzaFrom, competenzaTo time.Time
	if encrypted.CompetenzaFrom != "" {
		if competenzaFrom, err = time.Parse("2006-01-02", encrypted.CompetenzaFrom); err != nil {
			return domain.Transaction{}, fmt.Errorf("invalid competenza date %q", encrypted.CompetenzaFrom)
		}
	}
	if encrypted.CompetenzaTo != "" {
		if competenzaTo, err = time.Parse("2006-01-02", encrypted.CompetenzaTo); err != nil {
			return domain.Transaction{}, fmt.Errorf("invalid competenza date %q", encrypted.CompetenzaTo)
		}
	}
	transaction := domain.Transaction{
		CompetenzaFrom:   competenzaFrom,
		CompetenzaTo:     competenzaTo,
		ID:               encrypted.ID,
		WorkspaceID:      encrypted.WorkspaceID,
		BookingDate:      bookingDate,
		AmountMinor:      amountMinor,
		Currency:         currency,
		Description:      blob.Description,
		CleanDescription: blob.CleanDescription,
		MerchantName:     blob.MerchantName,
		Category:         blob.Category,
		ServerVersion:    encrypted.ServerVersion,
	}
	if missing {
		return transaction, errMissingCurrency
	}
	return transaction, nil
}

func decimalToMinor(value string) (int64, error) {
	if strings.ContainsAny(value, "eE") {
		return 0, errors.New("exponent notation is not supported")
	}
	sign := int64(1)
	if strings.HasPrefix(value, "-") {
		sign = -1
		value = strings.TrimPrefix(value, "-")
	} else {
		value = strings.TrimPrefix(value, "+")
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("invalid decimal %q", value)
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 2 {
		return 0, fmt.Errorf("more than two fractional digits in %q", value)
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	if !allDigits(parts[0]) || !allDigits(fraction) {
		return 0, fmt.Errorf("invalid decimal %q", value)
	}
	major, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse major units: %w", err)
	}
	minor, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse minor units: %w", err)
	}
	if major > (int64(^uint64(0)>>1)-minor)/100 {
		return 0, errors.New("amount is outside int64 range")
	}
	return sign * (major*100 + minor), nil
}

func monthBounds(month string) (time.Time, time.Time, error) {
	if len(month) != 7 {
		return time.Time{}, time.Time{}, errors.New("month must use YYYY-MM")
	}
	start, err := time.Parse("2006-01", month)
	if err != nil || start.Format("2006-01") != month {
		return time.Time{}, time.Time{}, errors.New("month must use YYYY-MM")
	}
	return start, start.AddDate(0, 1, 0), nil
}

func coverageIncludes(coverage domain.Coverage, start, endExclusive time.Time) bool {
	if !coverage.IsComplete {
		return false
	}
	from, fromErr := time.Parse("2006-01-02", coverage.CoverageFrom)
	to, toErr := time.Parse("2006-01-02", coverage.CoverageTo)
	if fromErr != nil || toErr != nil {
		return false
	}
	return !from.After(start) && !to.AddDate(0, 0, 1).Before(endExclusive)
}

// The Ratio app's own statistics rule (condo-family-manager
// lib/categories.ts, isExcludedFromSpending, commit 83b2601): transfers move
// money between Marco's accounts and investments buy or sell assets, so
// neither is income or spending. Kept identical so Jarvis and the app agree.
var (
	transferCategories   = []string{"Internal Transfer", "Credit Card Payment", "Giroconto"}
	investmentCategories = []string{"Investment Purchase", "Investment Sale", "Dividend Income", "Capital Gain/Loss"}
)

func inCategories(category string, list []string) bool {
	c := strings.TrimSpace(category)
	for _, l := range list {
		if strings.EqualFold(c, l) {
			return true
		}
	}
	return false
}

func isExcludedTransfer(category string) bool { return inCategories(category, transferCategories) }

func isExcludedInvestment(category string) bool { return inCategories(category, investmentCategories) }

func formatMinor(value int64) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	return fmt.Sprintf("%s%d.%02d", sign, value/100, value%100)
}

func allDigits(value string) bool {
	if value == "" {
		return true
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func allLetters(value string) bool {
	for _, character := range value {
		if !unicode.IsLetter(character) {
			return false
		}
	}
	return true
}
