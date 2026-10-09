// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package keystore

import "errors"

var ErrDesktopCredentialUnavailable = errors.New("OS credential store is available only on macOS and Windows")
