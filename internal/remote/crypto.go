// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package remote

import (
	"crypto/aes"
	"crypto/cipher"
)

type cipherBlock = cipher.Block
type cipherAEAD = cipher.AEAD

func aesNewCipher(key []byte) (cipher.Block, error)        { return aes.NewCipher(key) }
func cipherNewGCM(block cipher.Block) (cipher.AEAD, error) { return cipher.NewGCM(block) }
