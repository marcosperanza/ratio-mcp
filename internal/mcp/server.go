// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/marcosperanza/ratio-mcp/internal/domain"
)

const ProtocolVersion = "2025-06-18"

type Analytics interface {
	MonthSummary(ctx context.Context, month string) (domain.MonthSummary, error)
	Search(ctx context.Context, month, query string) (domain.SearchResult, error)
}

type WorkspaceAnalytics interface {
	Analytics
	ListWorkspaces(ctx context.Context) (domain.WorkspaceList, error)
	MonthSummaryFor(ctx context.Context, workspace, month string) (domain.MonthSummary, error)
	SearchFor(ctx context.Context, workspace, month, query string) (domain.SearchResult, error)
	QueryFor(ctx context.Context, workspace string, q domain.Query) (domain.QueryResult, error)
}

// querier is the single-workspace form of the query primitive (fixtures).
type querier interface {
	Query(ctx context.Context, q domain.Query) (domain.QueryResult, error)
}

type Server struct {
	name      string
	version   string
	analytics Analytics
}

func NewServer(name, version string, analytics Analytics) (*Server, error) {
	if name == "" || version == "" || analytics == nil {
		return nil, errors.New("server name, version and analytics are required")
	}
	return &Server{name: name, version: version, analytics: analytics}, nil
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Meta      json.RawMessage `json:"_meta,omitempty"`
}

type summaryArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Month     string `json:"month"`
}

// queryArgs are the arguments of ratio_categories, ratio_transactions and
// ratio_aggregate (each tool fixes the mode and accepts its own subset).
type queryArgs struct {
	Workspace  string   `json:"workspace,omitempty"`
	FromMonth  string   `json:"from_month"`
	ToMonth    string   `json:"to_month,omitempty"`
	Categories []string `json:"categories,omitempty"`
	Groups     []string `json:"groups,omitempty"`
	Text       string   `json:"text,omitempty"`
	Sign       string   `json:"sign,omitempty"`
	MinCents   int64    `json:"min_cents,omitempty"`
	MaxCents   int64    `json:"max_cents,omitempty"`
	Rules      string   `json:"rules,omitempty"`
	GroupBy    string   `json:"group_by,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Offset     int      `json:"offset,omitempty"`
}

type searchArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Month     string `json:"month"`
	Query     string `json:"query"`
}

type toolResult struct {
	Content           []content `json:"content"`
	StructuredContent any       `json:"structuredContent,omitempty"`
	IsError           bool      `json:"isError,omitempty"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (s *Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			if err := encoder.Encode(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}}); err != nil {
				return fmt.Errorf("write parse error: %w", err)
			}
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		res := s.handle(ctx, req)
		if err := encoder.Encode(res); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read MCP request: %w", err)
	}
	return nil
}

func (s *Server) handle(ctx context.Context, req request) response {
	base := response{JSONRPC: "2.0", ID: req.ID}
	if req.JSONRPC != "2.0" {
		base.Error = &rpcError{Code: -32600, Message: "invalid JSON-RPC request"}
		return base
	}
	switch req.Method {
	case "initialize":
		protocolVersion := ProtocolVersion
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(req.Params) > 0 && json.Unmarshal(req.Params, &params) == nil && params.ProtocolVersion != "" {
			protocolVersion = params.ProtocolVersion
		}
		base.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo":   map[string]string{"name": s.name, "version": s.version},
			"instructions": "Ratio financial tools return authoritative integer minor-unit amounts. Do not recalculate or alter those values.",
		}
	case "ping":
		base.Result = map[string]any{}
	case "tools/list":
		base.Result = map[string]any{"tools": definitions()}
	case "tools/call":
		result, err := s.callTool(ctx, req.Params)
		if err != nil {
			base.Result = errorToolResult(err)
		} else {
			base.Result = result
		}
	default:
		base.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	return base
}

func (s *Server) callTool(ctx context.Context, raw json.RawMessage) (toolResult, error) {
	var params callParams
	if err := decodeStrict(raw, &params); err != nil {
		return toolResult{}, fmt.Errorf("invalid tools/call parameters: %w", err)
	}
	if len(params.Arguments) == 0 {
		params.Arguments = json.RawMessage("{}")
	}
	switch params.Name {
	case "ratio_list_workspaces":
		if err := decodeStrict(params.Arguments, &struct{}{}); err != nil {
			return toolResult{}, fmt.Errorf("invalid ratio_list_workspaces arguments: %w", err)
		}
		analytics, ok := s.analytics.(WorkspaceAnalytics)
		if !ok {
			return successToolResult(domain.WorkspaceList{Workspaces: []domain.Workspace{{ID: "default", Name: "Default", Type: "FIXTURE", Role: "OWNER", IsDefault: true}}})
		}
		result, err := analytics.ListWorkspaces(ctx)
		if err != nil {
			return toolResult{}, err
		}
		return successToolResult(result)
	case "ratio_get_month_summary":
		var args summaryArgs
		if err := decodeStrict(params.Arguments, &args); err != nil {
			return toolResult{}, fmt.Errorf("invalid ratio_get_month_summary arguments: %w", err)
		}
		var result domain.MonthSummary
		var err error
		if analytics, ok := s.analytics.(WorkspaceAnalytics); ok {
			result, err = analytics.MonthSummaryFor(ctx, args.Workspace, args.Month)
		} else if args.Workspace == "" || strings.EqualFold(args.Workspace, "default") {
			result, err = s.analytics.MonthSummary(ctx, args.Month)
		} else {
			err = errors.New("this data source does not provide multiple workspaces")
		}
		if err != nil {
			return toolResult{}, err
		}
		return successToolResult(result)
	case "ratio_search_transactions":
		var args searchArgs
		if err := decodeStrict(params.Arguments, &args); err != nil {
			return toolResult{}, fmt.Errorf("invalid ratio_search_transactions arguments: %w", err)
		}
		var result domain.SearchResult
		var err error
		if analytics, ok := s.analytics.(WorkspaceAnalytics); ok {
			result, err = analytics.SearchFor(ctx, args.Workspace, args.Month, args.Query)
		} else if args.Workspace == "" || strings.EqualFold(args.Workspace, "default") {
			result, err = s.analytics.Search(ctx, args.Month, args.Query)
		} else {
			err = errors.New("this data source does not provide multiple workspaces")
		}
		if err != nil {
			return toolResult{}, err
		}
		return successToolResult(result)
	case "ratio_categories", "ratio_transactions", "ratio_aggregate":
		var args queryArgs
		if err := decodeStrict(params.Arguments, &args); err != nil {
			return toolResult{}, fmt.Errorf("invalid %s arguments: %w", params.Name, err)
		}
		q := domain.Query{Mode: strings.TrimPrefix(params.Name, "ratio_"), FromMonth: args.FromMonth, ToMonth: args.ToMonth,
			Categories: args.Categories, Groups: args.Groups, Text: args.Text, Sign: args.Sign, MinMinor: args.MinCents,
			MaxMinor: args.MaxCents, Rules: args.Rules, GroupBy: args.GroupBy, Limit: args.Limit, Offset: args.Offset}
		var result domain.QueryResult
		var err error
		if analytics, ok := s.analytics.(WorkspaceAnalytics); ok {
			result, err = analytics.QueryFor(ctx, args.Workspace, q)
		} else if single, ok := s.analytics.(querier); ok && (args.Workspace == "" || strings.EqualFold(args.Workspace, "default")) {
			result, err = single.Query(ctx, q)
		} else {
			err = errors.New("this data source does not provide queries")
		}
		if err != nil {
			return toolResult{}, err
		}
		return successToolResult(result)
	default:
		return toolResult{}, fmt.Errorf("unknown tool %q", params.Name)
	}
}

func definitions() []toolDefinition {
	stringProperty := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	return []toolDefinition{
		{
			Name:        "ratio_list_workspaces",
			Description: "Elenca i workspace Ratio che questo dispositivo Local AI è autorizzato a leggere. Usalo prima di un'analisi quando l'utente nomina un workspace.",
			InputSchema: map[string]any{
				"type": "object", "properties": map[string]any{}, "additionalProperties": false,
			},
		},
		{
			Name:        "ratio_get_month_summary",
			Description: "Calcola il riepilogo finanziario autorevole di un mese. Gli importi sono interi in centesimi e sono già calcolati dal server locale.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"month":     stringProperty("Mese nel formato YYYY-MM, per esempio 2026-09."),
					"workspace": stringProperty("Nome esatto o ID del workspace restituito da ratio_list_workspaces. Omettere per il workspace predefinito."),
				},
				"required":             []string{"month"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "ratio_search_transactions",
			Description: "Cerca nelle transazioni locali decifrate di un mese. La query può essere una parola o una breve frase naturale; mesi, anni e parole di contorno vengono ignorati. Restituisce al massimo cinque risultati secondo la policy locale.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"month":     stringProperty("Mese nel formato YYYY-MM, per esempio 2026-09."),
					"query":     stringProperty("Parola o breve frase da cercare in descrizione, esercente o categoria; non è necessario ripetere il mese."),
					"workspace": stringProperty("Nome esatto o ID del workspace restituito da ratio_list_workspaces. Omettere per il workspace predefinito."),
				},
				"required":             []string{"month", "query"},
				"additionalProperties": false,
			},
		},
		queryTool("ratio_categories",
			"Elenca le categorie presenti nelle transazioni del periodo, con il numero di movimenti e il gruppo "+
				"(spending, transfers, investments). Usalo per sapere quali nomi di categoria esistono prima di filtrare.",
			[]string{"workspace", "from_month", "to_month", "rules"}),
		queryTool("ratio_transactions",
			"Restituisce le transazioni grezze del periodo che soddisfano i filtri (categorie, gruppi, testo, segno, "+
				"importo), dalla più recente, a pagine (limit massimo 100, offset). Include i totali esatti di tutto "+
				"l'insieme filtrato. Per elenchi e per analizzare movimenti specifici.",
			[]string{"workspace", "from_month", "to_month", "categories", "groups", "text", "sign", "min_cents",
				"max_cents", "rules", "limit", "offset"}),
		queryTool("ratio_aggregate",
			"Somma e conta in modo esatto le transazioni filtrate, in totale o raggruppate per categoria, mese o "+
				"esercente. Usalo per qualsiasi cifra: non sommare importi a mano. Con rules=ratio (predefinito) segue "+
				"le statistiche dell'app: giroconti e investimenti esclusi salvo filtro esplicito, competenza ripartita.",
			[]string{"workspace", "from_month", "to_month", "categories", "groups", "text", "sign", "min_cents",
				"max_cents", "rules", "group_by", "limit"}),
	}
}

func queryTool(name, description string, fields []string) toolDefinition {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	all := map[string]any{
		"workspace":  str("Nome esatto o ID del workspace (ratio_list_workspaces). Omettere per il predefinito."),
		"from_month": str("Primo mese del periodo, YYYY-MM."),
		"to_month":   str("Ultimo mese incluso, YYYY-MM (omesso = from_month). Massimo 36 mesi."),
		"categories": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
			"description": "Nomi esatti di categoria (vedi ratio_categories), per esempio Shopping."},
		"groups": map[string]any{"type": "array", "items": map[string]any{"type": "string",
			"enum": []string{"spending", "transfers", "investments"}},
			"description": "Gruppi: spending (spese ed entrate correnti), transfers (giroconti), investments."},
		"text":      str("Parole da cercare in descrizione, esercente o categoria."),
		"sign":      map[string]any{"type": "string", "enum": []string{"all", "income", "expense"}},
		"min_cents": map[string]any{"type": "integer", "minimum": 0, "description": "Importo minimo in valore assoluto, in centesimi."},
		"max_cents": map[string]any{"type": "integer", "minimum": 0, "description": "Importo massimo in valore assoluto, in centesimi."},
		"rules": map[string]any{"type": "string", "enum": []string{"ratio", "raw"},
			"description": "ratio = come le statistiche dell'app; raw = ogni movimento alla data contabile."},
		"group_by": map[string]any{"type": "string", "enum": []string{"none", "category", "month", "merchant"}},
		"limit":    map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		"offset":   map[string]any{"type": "integer", "minimum": 0},
	}
	props := map[string]any{}
	for _, f := range fields {
		props[f] = all[f]
	}
	return toolDefinition{Name: name, Description: description, InputSchema: map[string]any{
		"type": "object", "properties": props, "required": []string{"from_month"}, "additionalProperties": false}}
}

func decodeStrict(raw []byte, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func successToolResult(value any) (toolResult, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return toolResult{}, fmt.Errorf("encode tool result: %w", err)
	}
	return toolResult{
		Content:           []content{{Type: "text", Text: string(raw)}},
		StructuredContent: value,
	}, nil
}

func errorToolResult(err error) toolResult {
	payload := map[string]any{"status": "error", "message": err.Error()}
	var coded interface {
		ErrorCode() string
		Retryable() bool
	}
	if errors.As(err, &coded) {
		payload["code"] = coded.ErrorCode()
		payload["retryable"] = coded.Retryable()
	}
	raw, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		raw = []byte(`{"status":"error","message":"internal error"}`)
	}
	return toolResult{Content: []content{{Type: "text", Text: string(raw)}}, IsError: true}
}
