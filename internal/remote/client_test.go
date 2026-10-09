// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClientRequiresHTTPSOutsideLoopback(t *testing.T) {
	if _, err := NewClient("http://ratio.example", ""); err == nil {
		t.Fatal("plain HTTP must be rejected")
	}
	if _, err := NewClient("http://127.0.0.1:3000", ""); err != nil {
		t.Fatalf("loopback development URL rejected: %v", err)
	}
	if _, err := NewClient("https://app.ratioapp.it", ""); err != nil {
		t.Fatalf("HTTPS URL rejected: %v", err)
	}
}

func TestRequestPreservesQueryParameters(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/transactions/sync" {
			t.Errorf("unexpected path: %s", request.URL.Path)
		}
		if request.URL.Query().Get("full") != "true" || request.URL.Query().Get("workspaceId") != "workspace-1" {
			t.Errorf("unexpected query: %s", request.URL.RawQuery)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		OK bool `json:"ok"`
	}
	_, err = client.request(context.Background(), http.MethodGet, "/api/transactions/sync?full=true&workspaceId=workspace-1", nil, nil, &response)
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK {
		t.Fatal("response was not decoded")
	}
}

func TestListWorkspacesMarksDefault(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/local-ai/workspaces" {
			http.NotFound(response, request)
			return
		}
		_, _ = response.Write([]byte(`{"defaultWorkspaceId":"ws-1","workspaces":[{"id":"ws-1","name":"Famiglia","type":"FAMILY","role":"OWNER"},{"id":"ws-2","name":"Casa","type":"CONDO","role":"ADMIN"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "ratio_lai_test.secret")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.ListWorkspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Workspaces) != 2 || !result.Workspaces[0].IsDefault || result.Workspaces[1].IsDefault {
		t.Fatalf("unexpected workspace list: %#v", result)
	}
}

// A competenza starting at local midnight on 1 January is 31 December in
// UTC: dates are read in the app's zone, as the app does in the browser.
func TestLocalDateUsesTheAppZone(t *testing.T) {
	utc := time.Date(2025, 12, 31, 23, 0, 0, 0, time.UTC) // 2026-01-01 00:00 in Rome
	if got := localDate(utc); got != "2026-01-01" {
		t.Fatalf("%s", got)
	}
	if got := localDate(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)); got != "2026-09-30" {
		t.Fatalf("%s", got)
	}
}
