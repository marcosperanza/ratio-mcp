// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package service

import "testing"

func TestDecimalToMinor(t *testing.T) {
	t.Parallel()
	tests := map[string]int64{
		"0":      0,
		"12":     1200,
		"12.3":   1230,
		"12.34":  1234,
		"-85.40": -8540,
	}
	for input, expected := range tests {
		actual, err := decimalToMinor(input)
		if err != nil {
			t.Fatalf("decimalToMinor(%q): %v", input, err)
		}
		if actual != expected {
			t.Fatalf("decimalToMinor(%q) = %d, want %d", input, actual, expected)
		}
	}
}

func TestDecimalToMinorRejectsFractionsBelowCent(t *testing.T) {
	t.Parallel()
	if _, err := decimalToMinor("1.001"); err == nil {
		t.Fatal("expected precision error")
	}
}
