// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ipc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/marcosperanza/ratio-mcp/internal/domain"
)

const maxResponseBytes = 1 << 20

type Client struct {
	Path string
	seq  atomic.Uint64
}

type RemoteError struct {
	Code           string
	Message        string
	RetryableValue bool
}

func (e *RemoteError) Error() string     { return e.Message }
func (e *RemoteError) ErrorCode() string { return e.Code }
func (e *RemoteError) Retryable() bool   { return e.RetryableValue }

func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	if c.Path == "" {
		return &RemoteError{Code: "RATIO_UNAVAILABLE", Message: "Ratio local service socket is not configured", RetryableValue: true}
	}
	dialer := net.Dialer{Timeout: time.Second}
	connection, err := dialer.DialContext(ctx, "unix", c.Path)
	if err != nil {
		return &RemoteError{Code: "RATIO_UNAVAILABLE", Message: "Ratio local service is unavailable", RetryableValue: true}
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	reader := bufio.NewReaderSize(connection, maxResponseBytes)
	if err := c.exchange(connection, reader, "ipc.hello", HelloParams{ProtocolVersions: []int{ProtocolVersion}, Client: "ratio-mcp"}, &HelloResult{}); err != nil {
		return err
	}
	return c.exchange(connection, reader, method, params, result)
}

func (c *Client) exchange(connection net.Conn, reader *bufio.Reader, method string, params, result any) error {
	id, _ := json.Marshal(c.seq.Add(1))
	request := Request{JSONRPC: "2.0", ID: id, Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		request.Params = raw
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if _, err := connection.Write(append(encoded, '\n')); err != nil {
		return &RemoteError{Code: "RATIO_UNAVAILABLE", Message: "Ratio local service connection failed", RetryableValue: true}
	}
	raw, err := readLineLimited(reader, maxResponseBytes)
	if err != nil {
		return &RemoteError{Code: "RATIO_UNAVAILABLE", Message: "Ratio local service returned no valid response", RetryableValue: true}
	}
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.JSONRPC != "2.0" || string(response.ID) != string(id) {
		return errors.New("invalid response from Ratio local service")
	}
	if response.Error != nil {
		return &RemoteError{Code: response.Error.Code, Message: response.Error.Message, RetryableValue: response.Error.Retryable}
	}
	if result != nil {
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decode Ratio local service response: %w", err)
		}
	}
	return nil
}

func readLineLimited(reader *bufio.Reader, limit int) ([]byte, error) {
	var response bytes.Buffer
	for response.Len() <= limit {
		fragment, prefix, err := reader.ReadLine()
		if err != nil {
			return nil, err
		}
		if response.Len()+len(fragment)+1 > limit {
			return nil, errors.New("response exceeds size limit")
		}
		response.Write(fragment)
		if !prefix {
			response.WriteByte('\n')
			return response.Bytes(), nil
		}
	}
	return nil, errors.New("response exceeds size limit")
}

type AnalyticsClient struct{ Client *Client }

func (a AnalyticsClient) MonthSummary(ctx context.Context, month string) (domain.MonthSummary, error) {
	return a.MonthSummaryFor(ctx, "", month)
}

func (a AnalyticsClient) MonthSummaryFor(ctx context.Context, workspace, month string) (domain.MonthSummary, error) {
	var result domain.MonthSummary
	err := a.Client.Call(ctx, "analytics.monthSummary", monthParams{Workspace: workspace, Month: month}, &result)
	return result, err
}

func (a AnalyticsClient) Search(ctx context.Context, month, query string) (domain.SearchResult, error) {
	return a.SearchFor(ctx, "", month, query)
}

func (a AnalyticsClient) SearchFor(ctx context.Context, workspace, month, query string) (domain.SearchResult, error) {
	var result domain.SearchResult
	err := a.Client.Call(ctx, "analytics.searchTransactions", searchParams{Workspace: workspace, Month: month, Query: query}, &result)
	return result, err
}

func (a AnalyticsClient) QueryFor(ctx context.Context, workspace string, q domain.Query) (domain.QueryResult, error) {
	var result domain.QueryResult
	err := a.Client.Call(ctx, "analytics.query", queryParams{Workspace: workspace, Query: q}, &result)
	return result, err
}

func (a AnalyticsClient) ListWorkspaces(ctx context.Context) (domain.WorkspaceList, error) {
	var result domain.WorkspaceList
	err := a.Client.Call(ctx, "workspaces.list", nil, &result)
	return result, err
}
