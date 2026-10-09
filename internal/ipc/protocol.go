// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ipc

import "github.com/marcosperanza/ratio-mcp/internal/domain"

import "encoding/json"

const ProtocolVersion = 1

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type HelloParams struct {
	ProtocolVersions []int  `json:"protocolVersions"`
	Client           string `json:"client"`
}

type HelloResult struct {
	ProtocolVersion int      `json:"protocolVersion"`
	Capabilities    []string `json:"capabilities"`
}

type monthParams struct {
	Workspace string `json:"workspace,omitempty"`
	Month     string `json:"month"`
}

type queryParams struct {
	Workspace string       `json:"workspace,omitempty"`
	Query     domain.Query `json:"query"`
}

type searchParams struct {
	Workspace string `json:"workspace,omitempty"`
	Month     string `json:"month"`
	Query     string `json:"query"`
}

type codedError interface {
	ErrorCode() string
	Retryable() bool
}
