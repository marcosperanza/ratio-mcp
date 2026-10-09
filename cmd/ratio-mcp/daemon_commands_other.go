//go:build !linux

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"io"
)

func runDaemon([]string, io.Writer, io.Writer) error {
	return errors.New("the persistent Unix-socket daemon is available on Linux; run the local MCP directly with --vault")
}

func runAdminCommand(string, []string, io.Writer, io.Writer) error {
	return errors.New("administrative socket commands are available on Linux daemon installations")
}
