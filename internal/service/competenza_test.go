// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"testing"
	"time"

	"github.com/marcosperanza/ratio-mcp/internal/domain"
)

func day(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

// Mirrors the app's getCompetenzaSlices: equal parts over the months of the
// accrual period; integer cents with the remainder on the first months.
func TestCompetenzaShare(t *testing.T) {
	bill := domain.Transaction{BookingDate: day("2026-03-10"), AmountMinor: -10000, // 100.00 over 3 months
		CompetenzaFrom: day("2026-01-01"), CompetenzaTo: day("2026-03-31")}
	var sum int64
	for _, tc := range []struct {
		month string
		want  int64
		ok    bool
	}{{"2025-12", 0, false}, {"2026-01", -3334, true}, {"2026-02", -3333, true}, {"2026-03", -3333, true}, {"2026-04", 0, false}} {
		got, ok := competenzaShare(bill, day(tc.month+"-01"))
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: %d %v, atteso %d %v", tc.month, got, ok, tc.want, tc.ok)
		}
		sum += got
	}
	if sum != -10000 {
		t.Fatalf("somma %d", sum)
	}
	plain := domain.Transaction{BookingDate: day("2026-09-30"), AmountMinor: 500}
	if got, ok := competenzaShare(plain, day("2026-09-01")); got != 500 || !ok {
		t.Fatalf("senza competenza: %d %v", got, ok)
	}
	onlyFrom := domain.Transaction{BookingDate: day("2026-10-02"), AmountMinor: -700, CompetenzaFrom: day("2026-09-01")}
	if got, ok := competenzaShare(onlyFrom, day("2026-09-01")); got != -700 || !ok {
		t.Fatalf("solo competenzaFrom: %d %v", got, ok)
	}
	if _, ok := competenzaShare(onlyFrom, day("2026-10-01")); ok {
		t.Fatal("contato anche nel mese di contabilizzazione")
	}
}
