// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package vault

import "bytes"

func bytesReader(value []byte) *bytes.Reader { return bytes.NewReader(value) }
