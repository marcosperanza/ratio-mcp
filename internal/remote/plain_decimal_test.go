// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package remote

import (
	"encoding/json"
	"testing"
)

func TestPlainDecimal(t *testing.T) {
	for raw, want := range map[string]string{``: "", `null`: "", `"-85.40"`: "-85.40", `12.5`: "12.5", `" 3.00 "`: "3.00"} {
		got, err := plainDecimal(json.RawMessage(raw))
		if err != nil || got != want {
			t.Errorf("%q=%q err=%v want %q", raw, got, err, want)
		}
	}
	if _, err := plainDecimal(json.RawMessage(`{"x":1}`)); err == nil {
		t.Fatal("oggetto accettato come importo")
	}
}
