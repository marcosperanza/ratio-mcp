// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package localcore

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcosperanza/ratio-mcp/internal/domain"
	"github.com/marcosperanza/ratio-mcp/internal/keystore"
	"github.com/marcosperanza/ratio-mcp/internal/policy"
	"github.com/marcosperanza/ratio-mcp/internal/remote"
	"github.com/marcosperanza/ratio-mcp/internal/service"
	"github.com/marcosperanza/ratio-mcp/internal/vault"
)

const (
	CodeLocked          = "RATIO_LOCKED"
	CodeUnavailable     = "RATIO_UNAVAILABLE"
	CodeAuthRequired    = "RATIO_AUTH_REQUIRED"
	CodeAccessDenied    = "RATIO_ACCESS_DENIED"
	CodeInvalidArgument = "RATIO_INVALID_ARGUMENT"
	CodeBusy            = "RATIO_BUSY"
)

type Error struct {
	Code           string
	Message        string
	RetryableValue bool
}

func (e *Error) Error() string     { return e.Message }
func (e *Error) ErrorCode() string { return e.Code }
func (e *Error) Retryable() bool   { return e.RetryableValue }

type Status struct {
	Vault               string `json:"vault"`
	Ready               bool   `json:"ready"`
	AutounlockPaused    bool   `json:"autounlockPaused"`
	Generation          uint64 `json:"generation"`
	CredentialExpiresAt string `json:"credentialExpiresAt,omitempty"`
	LastErrorCode       string `json:"lastErrorCode,omitempty"`
	LastTransitionAt    string `json:"lastTransitionAt"`
}

type Manager struct {
	mu            sync.RWMutex
	vaultPath     string
	pausePath     string
	provider      keystore.Provider
	unlocked      *unlockedSession
	sessionCtx    context.Context
	cancelSession context.CancelFunc
	generation    uint64
	unlocking     bool
	paused        bool
	expiresAt     string
	lastErrorCode string
	transitionAt  time.Time
	verifyDevice  DeviceVerifier
}

type unlockedSession struct {
	client             *remote.Client
	masterKey          []byte
	defaultWorkspaceID string
}

type operationSession struct {
	client             *remote.Client
	masterKey          []byte
	defaultWorkspaceID string
}

type DeviceVerifier func(context.Context, *remote.Client, vault.Data) (int, error)

type Option func(*Manager)

func WithDeviceVerifier(verifier DeviceVerifier) Option {
	return func(manager *Manager) { manager.verifyDevice = verifier }
}

func New(vaultPath, pausePath string, provider keystore.Provider, options ...Option) (*Manager, error) {
	if vaultPath == "" || pausePath == "" || provider == nil {
		return nil, errors.New("vault path, pause path and credential provider are required")
	}
	manager := &Manager{
		vaultPath: vaultPath, pausePath: pausePath, provider: provider, transitionAt: time.Now().UTC(),
		verifyDevice: func(ctx context.Context, client *remote.Client, data vault.Data) (int, error) {
			return client.ValidateDevice(ctx, data.DeviceID, data.WorkspaceID)
		},
	}
	for _, option := range options {
		option(manager)
	}
	if manager.verifyDevice == nil {
		return nil, errors.New("device verifier is required")
	}
	if _, err := os.Stat(pausePath); err == nil {
		manager.paused = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect autounlock state: %w", err)
	}
	return manager, nil
}

func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	vaultState := "locked"
	if m.unlocked != nil {
		vaultState = "unlocked"
	} else if m.unlocking {
		vaultState = "unlocking"
	}
	return Status{
		Vault: vaultState, Ready: m.unlocked != nil, AutounlockPaused: m.paused,
		Generation: m.generation, CredentialExpiresAt: m.expiresAt,
		LastErrorCode: m.lastErrorCode, LastTransitionAt: m.transitionAt.Format(time.RFC3339),
	}
}

func (m *Manager) AutoUnlock(ctx context.Context) error {
	m.mu.RLock()
	paused := m.paused
	m.mu.RUnlock()
	if paused {
		return &Error{Code: CodeLocked, Message: "autounlock is paused by an administrator"}
	}
	return m.Unlock(ctx)
}

func (m *Manager) Unlock(ctx context.Context) error {
	m.mu.Lock()
	if m.unlocked != nil {
		m.mu.Unlock()
		return nil
	}
	if m.unlocking {
		m.mu.Unlock()
		return &Error{Code: CodeBusy, Message: "vault unlock is already in progress", RetryableValue: true}
	}
	m.unlocking = true
	m.transitionAt = time.Now().UTC()
	m.mu.Unlock()

	passphrase, err := m.provider.Load(ctx)
	if err != nil {
		m.finishUnlock(nil, "", CodeUnavailable)
		return &Error{Code: CodeUnavailable, Message: err.Error(), RetryableValue: true}
	}
	defer wipe(passphrase)
	data, err := vault.Load(m.vaultPath, passphrase)
	if err != nil {
		m.finishUnlock(nil, "", CodeLocked)
		return &Error{Code: CodeLocked, Message: "unable to unlock the encrypted Ratio vault"}
	}
	if data.ActivationPending {
		m.finishUnlock(nil, data.CredentialExpiresAt, CodeAuthRequired)
		return &Error{Code: CodeAuthRequired, Message: "device activation is incomplete"}
	}
	if expiry, parseErr := time.Parse(time.RFC3339, data.CredentialExpiresAt); parseErr != nil || !expiry.After(time.Now()) {
		m.finishUnlock(nil, data.CredentialExpiresAt, CodeAuthRequired)
		return &Error{Code: CodeAuthRequired, Message: "Local AI credential is expired or invalid"}
	}
	masterKey, err := base64.RawURLEncoding.DecodeString(data.MasterKey)
	if err != nil || len(masterKey) != 32 {
		wipe(masterKey)
		m.finishUnlock(nil, data.CredentialExpiresAt, CodeLocked)
		return &Error{Code: CodeLocked, Message: "vault contains an invalid master key"}
	}
	client, err := remote.NewClient(data.ServerURL, data.APIKey)
	if err != nil {
		wipe(masterKey)
		m.finishUnlock(nil, data.CredentialExpiresAt, CodeUnavailable)
		return &Error{Code: CodeUnavailable, Message: "vault contains an invalid Ratio server configuration"}
	}
	validationCtx, cancelValidation := context.WithTimeout(ctx, 15*time.Second)
	statusCode, err := m.verifyDevice(validationCtx, client, data)
	cancelValidation()
	if err != nil {
		wipe(masterKey)
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
			m.finishUnlock(nil, data.CredentialExpiresAt, CodeAuthRequired)
			return &Error{Code: CodeAuthRequired, Message: "Local AI credential is invalid, expired or revoked"}
		}
		m.finishUnlock(nil, data.CredentialExpiresAt, CodeUnavailable)
		return &Error{Code: CodeUnavailable, Message: "Ratio server is unavailable; autounlock will retry", RetryableValue: true}
	}
	m.finishUnlock(&unlockedSession{
		client: client, masterKey: masterKey, defaultWorkspaceID: data.WorkspaceID,
	}, data.CredentialExpiresAt, "")
	if err := os.Remove(m.pausePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = m.Lock()
		return errors.New("vault opened but autounlock pause state could not be cleared")
	}
	m.mu.Lock()
	m.paused = false
	m.mu.Unlock()
	return nil
}

func (m *Manager) finishUnlock(unlocked *unlockedSession, expiry, errorCode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unlocking = false
	m.expiresAt = expiry
	m.lastErrorCode = errorCode
	m.transitionAt = time.Now().UTC()
	if unlocked != nil {
		if m.cancelSession != nil {
			m.cancelSession()
		}
		if m.unlocked != nil {
			wipe(m.unlocked.masterKey)
		}
		m.sessionCtx, m.cancelSession = context.WithCancel(context.Background())
		m.unlocked = unlocked
		m.generation++
	}
}

func (m *Manager) Lock() error {
	if err := writePauseFile(m.pausePath); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancelSession != nil {
		m.cancelSession()
	}
	if m.unlocked != nil {
		wipe(m.unlocked.masterKey)
	}
	m.unlocked = nil
	m.sessionCtx = nil
	m.cancelSession = nil
	m.generation++
	m.paused = true
	m.lastErrorCode = CodeLocked
	m.transitionAt = time.Now().UTC()
	return nil
}

func (m *Manager) MonthSummary(ctx context.Context, month string) (domain.MonthSummary, error) {
	return m.MonthSummaryFor(ctx, "", month)
}

func (m *Manager) MonthSummaryFor(ctx context.Context, workspace, month string) (domain.MonthSummary, error) {
	operation, session, generation, err := m.session()
	if err != nil {
		return domain.MonthSummary{}, err
	}
	defer wipe(operation.masterKey)
	opCtx, cancel := joinContexts(ctx, session)
	defer cancel()
	selected, err := operation.resolveWorkspace(opCtx, workspace)
	if err != nil {
		return domain.MonthSummary{}, err
	}
	analytics, err := operation.analytics(selected.ID)
	if err != nil {
		return domain.MonthSummary{}, err
	}
	result, err := analytics.MonthSummary(opCtx, month)
	if err != nil {
		return domain.MonthSummary{}, err
	}
	if !m.generationCurrent(generation) {
		return domain.MonthSummary{}, &Error{Code: CodeLocked, Message: "Ratio was locked while the request was running", RetryableValue: true}
	}
	result.WorkspaceName = selected.Name
	return result, nil
}

func (m *Manager) Search(ctx context.Context, month, query string) (domain.SearchResult, error) {
	return m.SearchFor(ctx, "", month, query)
}

func (m *Manager) SearchFor(ctx context.Context, workspace, month, query string) (domain.SearchResult, error) {
	operation, session, generation, err := m.session()
	if err != nil {
		return domain.SearchResult{}, err
	}
	defer wipe(operation.masterKey)
	opCtx, cancel := joinContexts(ctx, session)
	defer cancel()
	selected, err := operation.resolveWorkspace(opCtx, workspace)
	if err != nil {
		return domain.SearchResult{}, err
	}
	analytics, err := operation.analytics(selected.ID)
	if err != nil {
		return domain.SearchResult{}, err
	}
	result, err := analytics.Search(opCtx, month, query)
	if err != nil {
		return domain.SearchResult{}, err
	}
	if !m.generationCurrent(generation) {
		return domain.SearchResult{}, &Error{Code: CodeLocked, Message: "Ratio was locked while the request was running", RetryableValue: true}
	}
	result.WorkspaceName = selected.Name
	return result, nil
}

func (m *Manager) QueryFor(ctx context.Context, workspace string, q domain.Query) (domain.QueryResult, error) {
	operation, session, generation, err := m.session()
	if err != nil {
		return domain.QueryResult{}, err
	}
	defer wipe(operation.masterKey)
	opCtx, cancel := joinContexts(ctx, session)
	defer cancel()
	selected, err := operation.resolveWorkspace(opCtx, workspace)
	if err != nil {
		return domain.QueryResult{}, err
	}
	analytics, err := operation.analytics(selected.ID)
	if err != nil {
		return domain.QueryResult{}, err
	}
	result, err := analytics.Query(opCtx, q)
	if err != nil {
		return domain.QueryResult{}, err
	}
	if !m.generationCurrent(generation) {
		return domain.QueryResult{}, &Error{Code: CodeLocked, Message: "Ratio was locked while the request was running", RetryableValue: true}
	}
	result.WorkspaceName = selected.Name
	return result, nil
}

func (m *Manager) ListWorkspaces(ctx context.Context) (domain.WorkspaceList, error) {
	operation, session, generation, err := m.session()
	if err != nil {
		return domain.WorkspaceList{}, err
	}
	defer wipe(operation.masterKey)
	opCtx, cancel := joinContexts(ctx, session)
	defer cancel()
	result, err := operation.client.ListWorkspaces(opCtx)
	if err != nil {
		return domain.WorkspaceList{}, remoteAccessError(err)
	}
	if !m.generationCurrent(generation) {
		return domain.WorkspaceList{}, &Error{Code: CodeLocked, Message: "Ratio was locked while the request was running", RetryableValue: true}
	}
	return result, nil
}

func (m *Manager) session() (operationSession, context.Context, uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.unlocked == nil || m.sessionCtx == nil {
		return operationSession{}, nil, 0, &Error{Code: CodeLocked, Message: "Ratio is locked; unlock it with the local administrative command", RetryableValue: true}
	}
	return operationSession{
		client:             m.unlocked.client,
		masterKey:          append([]byte(nil), m.unlocked.masterKey...),
		defaultWorkspaceID: m.unlocked.defaultWorkspaceID,
	}, m.sessionCtx, m.generation, nil
}

func (m *Manager) generationCurrent(generation uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.unlocked != nil && m.generation == generation
}

func (s operationSession) resolveWorkspace(ctx context.Context, selector string) (domain.Workspace, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return domain.Workspace{ID: s.defaultWorkspaceID, IsDefault: true}, nil
	}
	list, err := s.client.ListWorkspaces(ctx)
	if err != nil {
		return domain.Workspace{}, remoteAccessError(err)
	}
	var match *domain.Workspace
	for index := range list.Workspaces {
		workspace := &list.Workspaces[index]
		if workspace.ID == selector || strings.EqualFold(workspace.Name, selector) {
			if match != nil && match.ID != workspace.ID {
				return domain.Workspace{}, &Error{Code: CodeInvalidArgument, Message: "workspace name is ambiguous"}
			}
			match = workspace
		}
	}
	if match == nil {
		return domain.Workspace{}, &Error{Code: CodeInvalidArgument, Message: "workspace is not available to this Local AI device"}
	}
	return *match, nil
}

func (s operationSession) analytics(workspaceID string) (*service.Analytics, error) {
	return service.NewAnalytics(
		remote.Repository{Client: s.client, WorkspaceID: workspaceID},
		remote.StaticKeyProvider{WorkspaceID: workspaceID, Key: s.masterKey},
		policy.FixtureDevelopment(),
	)
}

func remoteAccessError(err error) error {
	switch remote.StatusCode(err) {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &Error{Code: CodeAccessDenied, Message: "Local AI device is not allowed to list or access this workspace"}
	default:
		return &Error{Code: CodeUnavailable, Message: "Ratio server is unavailable", RetryableValue: true}
	}
}

func joinContexts(parent, session context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-session.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func writePauseFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".autounlock-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.WriteString("paused\n"); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
