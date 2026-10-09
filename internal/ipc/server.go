//go:build linux

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
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/marcosperanza/ratio-mcp/internal/localcore"
	"golang.org/x/sys/unix"
)

const (
	maxRequestBytes = 64 << 10
)

type SocketKind string

const (
	ToolsSocket SocketKind = "tools"
	AdminSocket SocketKind = "admin"
)

type Server struct {
	Path       string
	Kind       SocketKind
	Manager    *localcore.Manager
	AllowedUID uint32
	listener   *net.UnixListener
	wg         sync.WaitGroup
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.Path == "" || s.Manager == nil {
		return errors.New("socket path and manager are required")
	}
	if s.AllowedUID == 0 {
		s.AllowedUID = uint32(os.Geteuid())
	}
	if err := prepareSocketPath(s.Path); err != nil {
		return err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: s.Path, Net: "unix"})
	if err != nil {
		return err
	}
	s.listener = listener
	if err := os.Chmod(s.Path, 0o600); err != nil {
		listener.Close()
		return err
	}
	defer func() {
		listener.Close()
		s.wg.Wait()
		_ = os.Remove(s.Path)
	}()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		connection, acceptErr := listener.AcceptUnix()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return acceptErr
		}
		if err := authorizePeer(connection, s.AllowedUID); err != nil {
			_ = connection.Close()
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer connection.Close()
			s.handleConnection(ctx, connection)
		}()
	}
}

func (s *Server) handleConnection(serverCtx context.Context, connection *net.UnixConn) {
	scanner := bufio.NewScanner(connection)
	scanner.Buffer(make([]byte, 4096), maxRequestBytes)
	writer := bufio.NewWriter(connection)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		response := s.handle(serverCtx, line)
		raw, err := json.Marshal(response)
		if err != nil || len(raw) > maxResponseBytes {
			response = Response{JSONRPC: "2.0", ID: response.ID, Error: &RPCError{Code: localcore.CodeUnavailable, Message: "response could not be encoded"}}
			raw, _ = json.Marshal(response)
		}
		if _, err := writer.Write(append(raw, '\n')); err != nil {
			return
		}
		if err := writer.Flush(); err != nil {
			return
		}
	}
}

func (s *Server) handle(serverCtx context.Context, raw []byte) Response {
	var request Request
	if err := decodeStrict(raw, &request); err != nil || request.JSONRPC != "2.0" || len(request.ID) == 0 || request.Method == "" {
		return Response{JSONRPC: "2.0", ID: request.ID, Error: &RPCError{Code: "RATIO_INVALID_ARGUMENT", Message: "invalid IPC request"}}
	}
	response := Response{JSONRPC: "2.0", ID: request.ID}
	ctx, cancel := context.WithTimeout(serverCtx, methodTimeout(request.Method))
	defer cancel()
	result, err := s.dispatch(ctx, request)
	if err != nil {
		response.Error = errorResponse(err)
		return response
	}
	response.Result = result
	return response
}

func (s *Server) dispatch(ctx context.Context, request Request) (any, error) {
	switch request.Method {
	case "ipc.hello":
		var params HelloParams
		if err := decodeParams(request.Params, &params); err != nil || !containsVersion(params.ProtocolVersions, ProtocolVersion) {
			return nil, &localcore.Error{Code: "RATIO_PROTOCOL_VERSION", Message: "incompatible IPC protocol"}
		}
		capabilities := []string{"status"}
		if s.Kind == ToolsSocket {
			capabilities = append(capabilities, "workspaces.list", "analytics.monthSummary", "analytics.searchTransactions", "analytics.query")
		} else {
			capabilities = append(capabilities, "admin.lock", "admin.unlock")
		}
		return HelloResult{ProtocolVersion: ProtocolVersion, Capabilities: capabilities}, nil
	case "core.status":
		return s.Manager.Status(), nil
	case "analytics.monthSummary":
		if s.Kind != ToolsSocket {
			return nil, errors.New("method is unavailable on this socket")
		}
		var params monthParams
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.Manager.MonthSummaryFor(ctx, params.Workspace, params.Month)
	case "analytics.searchTransactions":
		if s.Kind != ToolsSocket {
			return nil, errors.New("method is unavailable on this socket")
		}
		var params searchParams
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.Manager.SearchFor(ctx, params.Workspace, params.Month, params.Query)
	case "analytics.query":
		if s.Kind != ToolsSocket {
			return nil, errors.New("method is unavailable on this socket")
		}
		var params queryParams
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.Manager.QueryFor(ctx, params.Workspace, params.Query)
	case "workspaces.list":
		if s.Kind != ToolsSocket {
			return nil, errors.New("method is unavailable on this socket")
		}
		return s.Manager.ListWorkspaces(ctx)
	case "admin.status":
		if s.Kind != AdminSocket {
			return nil, errors.New("method is unavailable on this socket")
		}
		return s.Manager.Status(), nil
	case "admin.unlock":
		if s.Kind != AdminSocket {
			return nil, errors.New("method is unavailable on this socket")
		}
		if err := s.Manager.Unlock(ctx); err != nil {
			return nil, err
		}
		return s.Manager.Status(), nil
	case "admin.lock":
		if s.Kind != AdminSocket {
			return nil, errors.New("method is unavailable on this socket")
		}
		if err := s.Manager.Lock(); err != nil {
			return nil, err
		}
		return s.Manager.Status(), nil
	default:
		return nil, &localcore.Error{Code: "RATIO_INVALID_ARGUMENT", Message: "unknown IPC method"}
	}
}

func methodTimeout(method string) time.Duration {
	switch method {
	case "ipc.hello", "core.status", "admin.status":
		return time.Second
	case "admin.lock":
		return 5 * time.Second
	case "admin.unlock":
		return 20 * time.Second
	default:
		return 30 * time.Second
	}
}

func prepareSocketPath(path string) error {
	if len(path) > 100 {
		return errors.New("Unix socket path is too long")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("refusing to replace a non-socket runtime path")
	}
	probe, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
	if dialErr == nil {
		probe.Close()
		return errors.New("another daemon is already listening")
	}
	return os.Remove(path)
}

func authorizePeer(connection *net.UnixConn, allowedUID uint32) error {
	raw, err := connection.SyscallConn()
	if err != nil {
		return err
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if controlErr != nil {
		return controlErr
	}
	if credential == nil || credential.Uid != allowedUID {
		return errors.New("peer UID is not authorized")
	}
	return nil
}

func decodeStrict(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("trailing JSON is not allowed")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON is not allowed")
		}
		return err
	}
	return nil
}

func decodeParams(raw json.RawMessage, destination any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := decodeStrict(raw, destination); err != nil {
		return &localcore.Error{Code: "RATIO_INVALID_ARGUMENT", Message: "invalid IPC parameters"}
	}
	return nil
}

func containsVersion(versions []int, wanted int) bool {
	for _, version := range versions {
		if version == wanted {
			return true
		}
	}
	return false
}

func errorResponse(err error) *RPCError {
	var coded codedError
	if errors.As(err, &coded) {
		return &RPCError{Code: coded.ErrorCode(), Message: err.Error(), Retryable: coded.Retryable()}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &RPCError{Code: "RATIO_TIMEOUT", Message: "operation timed out", Retryable: true}
	}
	return &RPCError{Code: localcore.CodeUnavailable, Message: fmt.Sprintf("operation failed: %v", err)}
}
