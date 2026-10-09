// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcosperanza/ratio-mcp/internal/domain"
)

type fakeAnalytics struct{}

func (fakeAnalytics) MonthSummary(_ context.Context, month string) (domain.MonthSummary, error) {
	return domain.MonthSummary{Status: "ok", Month: month, IncomeMinor: 12345}, nil
}

func (fakeAnalytics) Search(_ context.Context, month, query string) (domain.SearchResult, error) {
	return domain.SearchResult{Status: "ok", Month: month, Query: query}, nil
}

type fakeWorkspaceAnalytics struct{ fakeAnalytics }

func (fakeWorkspaceAnalytics) ListWorkspaces(context.Context) (domain.WorkspaceList, error) {
	return domain.WorkspaceList{Workspaces: []domain.Workspace{{ID: "ws-family", Name: "Famiglia", IsDefault: true}}}, nil
}

func (fakeWorkspaceAnalytics) MonthSummaryFor(_ context.Context, workspace, month string) (domain.MonthSummary, error) {
	return domain.MonthSummary{Status: "ok", WorkspaceID: workspace, WorkspaceName: workspace, Month: month}, nil
}

func (fakeWorkspaceAnalytics) SearchFor(_ context.Context, workspace, month, query string) (domain.SearchResult, error) {
	return domain.SearchResult{Status: "ok", WorkspaceID: workspace, WorkspaceName: workspace, Month: month, Query: query}, nil
}

func (fakeWorkspaceAnalytics) QueryFor(_ context.Context, workspace string, q domain.Query) (domain.QueryResult, error) {
	return domain.QueryResult{Status: "ok", WorkspaceID: workspace, Query: q}, nil
}

// The three query tools fix the mode and forward the filters unchanged.
func TestQueryToolsForwardModeAndFilters(t *testing.T) {
	server, err := NewServer("ratio-mcp", "test", fakeWorkspaceAnalytics{})
	if err != nil {
		t.Fatal(err)
	}
	for tool, mode := range map[string]string{"ratio_categories": "categories", "ratio_transactions": "transactions",
		"ratio_aggregate": "aggregate"} {
		args := `{"workspace":"Casa","from_month":"2026-09","rules":"raw"}`
		if tool != "ratio_categories" {
			args = `{"workspace":"Casa","from_month":"2026-09","groups":["investments"],"sign":"expense"}`
		}
		raw, _ := json.Marshal(map[string]any{"name": tool, "arguments": json.RawMessage(args)})
		res, err := server.callTool(context.Background(), raw)
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		got := res.StructuredContent.(domain.QueryResult)
		if got.WorkspaceID != "Casa" || got.Query.Mode != mode || got.Query.FromMonth != "2026-09" {
			t.Fatalf("%s: %+v", tool, got)
		}
	}
	raw, _ := json.Marshal(map[string]any{"name": "ratio_categories", "arguments": json.RawMessage(`{"from_month":"2026-09","delete":true}`)})
	if _, err := server.callTool(context.Background(), raw); err == nil {
		t.Fatal("argomento sconosciuto accettato")
	}
}

func TestServerLifecycleAndToolCall(t *testing.T) {
	t.Parallel()
	server, err := NewServer("ratio-mcp", "test", fakeAnalytics{})
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ratio_get_month_summary","arguments":{"month":"2026-09"},"_meta":{"progressToken":"claude-test"}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d responses, want 3: %s", len(lines), output.String())
	}
	if !strings.Contains(lines[0], `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("unexpected initialize response: %s", lines[0])
	}
	var callResponse struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &callResponse); err != nil {
		t.Fatal(err)
	}
	if callResponse.Result.IsError || !strings.Contains(callResponse.Result.Content[0].Text, `"incomeMinor":12345`) {
		t.Fatalf("unexpected tool result: %s", lines[2])
	}
}

func TestServerRejectsUnknownArguments(t *testing.T) {
	t.Parallel()
	server, err := NewServer("ratio-mcp", "test", fakeAnalytics{})
	if err != nil {
		t.Fatal(err)
	}
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ratio_get_month_summary","arguments":{"month":"2026-09","escape":"value"}}}` + "\n"
	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"isError":true`) || !strings.Contains(output.String(), "unknown field") {
		t.Fatalf("expected safe tool error, got %s", output.String())
	}
}

func TestWorkspaceToolsPassExplicitWorkspace(t *testing.T) {
	t.Parallel()
	server, err := NewServer("ratio-mcp", "test", fakeWorkspaceAnalytics{})
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ratio_list_workspaces","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ratio_get_month_summary","arguments":{"workspace":"Famiglia","month":"2026-08"}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"name":"Famiglia"`) || !strings.Contains(output.String(), `"workspaceName":"Famiglia"`) {
		t.Fatalf("workspace was not listed and forwarded: %s", output.String())
	}
}
