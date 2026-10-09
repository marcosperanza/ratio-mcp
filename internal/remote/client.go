// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/marcosperanza/ratio-mcp/internal/domain"
)

const envelopeAlgorithm = "RSA-OAEP-4096-SHA256+A256GCM"

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	apiKey     string
}

type HTTPError struct {
	Status int
}

func (e *HTTPError) Error() string { return fmt.Sprintf("Ratio API returned HTTP %d", e.Status) }

func StatusCode(err error) int {
	var responseError *HTTPError
	if errors.As(err, &responseError) {
		return responseError.Status
	}
	return 0
}

type DeviceStatus struct {
	Device struct {
		ID          string     `json:"id"`
		WorkspaceID string     `json:"workspaceId"`
		RevokedAt   *time.Time `json:"revokedAt"`
		ExpiresAt   time.Time  `json:"expiresAt"`
	} `json:"device"`
}

type workspaceListResponse struct {
	DefaultWorkspaceID string `json:"defaultWorkspaceId"`
	Workspaces         []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
		Role string `json:"role"`
	} `json:"workspaces"`
}

func (c *Client) ListWorkspaces(ctx context.Context) (domain.WorkspaceList, error) {
	var response workspaceListResponse
	if _, err := c.request(ctx, http.MethodGet, "/api/local-ai/workspaces", nil, nil, &response); err != nil {
		return domain.WorkspaceList{}, err
	}
	result := domain.WorkspaceList{Workspaces: make([]domain.Workspace, 0, len(response.Workspaces))}
	for _, workspace := range response.Workspaces {
		result.Workspaces = append(result.Workspaces, domain.Workspace{
			ID: workspace.ID, Name: workspace.Name, Type: workspace.Type, Role: workspace.Role,
			IsDefault: workspace.ID == response.DefaultWorkspaceID,
		})
	}
	return result, nil
}

func (c *Client) ValidateDevice(ctx context.Context, deviceID, workspaceID string) (int, error) {
	var status DeviceStatus
	httpStatus, err := c.request(ctx, http.MethodGet, "/api/local-ai/device?workspaceId="+url.QueryEscape(workspaceID), nil, nil, &status)
	if err != nil {
		return httpStatus, err
	}
	if status.Device.ID != deviceID || status.Device.WorkspaceID != workspaceID || status.Device.RevokedAt != nil || !status.Device.ExpiresAt.After(time.Now()) {
		return http.StatusUnauthorized, errors.New("Local AI device is invalid, expired or revoked")
	}
	return httpStatus, nil
}

func NewClient(rawURL, apiKey string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil || parsed.Host == "" {
		return nil, errors.New("invalid Ratio server URL")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1")) {
		return nil, errors.New("Ratio server must use HTTPS (HTTP is allowed only on loopback)")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			return errors.New("cross-origin redirect refused")
		}
		return nil
	}
	return &Client{baseURL: parsed, httpClient: client, apiKey: apiKey}, nil
}

func (c *Client) request(ctx context.Context, method, path string, body any, headers map[string]string, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(encoded)
	}
	relative, err := url.Parse(path)
	if err != nil || relative.IsAbs() || relative.Host != "" || !strings.HasPrefix(relative.Path, "/") {
		return 0, errors.New("invalid Ratio API path")
	}
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(c.baseURL.Path, "/") + relative.Path
	endpoint.RawQuery = relative.RawQuery
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, &HTTPError{Status: resp.StatusCode}
	}
	if out != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

type CreatePairingRequest struct {
	PublicKeyJWK      string `json:"publicKeyJwk"`
	PairingNonce      string `json:"pairingNonce"`
	PollingSecretHash string `json:"pollingSecretHash"`
}

type CreatePairingResponse struct {
	ID                   string    `json:"id"`
	DeviceID             string    `json:"deviceId"`
	UserCode             string    `json:"userCode"`
	ExpiresAt            time.Time `json:"expiresAt"`
	PublicKeyFingerprint string    `json:"publicKeyFingerprint"`
}

type PollResponse struct {
	State               string    `json:"state"`
	DeviceID            string    `json:"deviceId"`
	WorkspaceID         string    `json:"workspaceId"`
	Scopes              []string  `json:"scopes"`
	Envelope            string    `json:"envelope"`
	EnvelopeDigest      string    `json:"envelopeDigest"`
	TranscriptDigest    string    `json:"transcriptDigest"`
	CredentialExpiresAt time.Time `json:"credentialExpiresAt"`
}

func (c *Client) CreatePairing(ctx context.Context, request CreatePairingRequest) (CreatePairingResponse, error) {
	var response CreatePairingResponse
	_, err := c.request(ctx, http.MethodPost, "/api/local-ai/pairings", request, nil, &response)
	return response, err
}

func (c *Client) Poll(ctx context.Context, id, secret string) (PollResponse, bool, error) {
	var response PollResponse
	status, err := c.request(ctx, http.MethodPost, "/api/local-ai/pairings/"+url.PathEscape(id)+"/poll", nil, map[string]string{"X-Ratio-Pairing-Secret": secret}, &response)
	if status == http.StatusAccepted {
		return response, false, nil
	}
	return response, err == nil, err
}

func (c *Client) Complete(ctx context.Context, id, pollingSecret, bootstrapSecret, credentialID, credentialHash string) error {
	body := map[string]string{"bootstrapSecret": bootstrapSecret, "credentialId": credentialID, "credentialSecretHash": credentialHash}
	_, err := c.request(ctx, http.MethodPost, "/api/local-ai/pairings/"+url.PathEscape(id)+"/complete", body, map[string]string{"X-Ratio-Pairing-Secret": pollingSecret}, nil)
	return err
}

func (c *Client) Acknowledge(ctx context.Context, workspaceID string) error {
	_, err := c.request(ctx, http.MethodPost, "/api/local-ai/device", map[string]string{"workspaceId": workspaceID}, nil, nil)
	return err
}

type DeviceEnvelopeHeader struct {
	Origin               string   `json:"origin"`
	PairingID            string   `json:"pairingId"`
	DeviceID             string   `json:"deviceId"`
	UserID               string   `json:"userId"`
	WorkspaceID          string   `json:"workspaceId"`
	PublicKeyFingerprint string   `json:"publicKeyFingerprint"`
	PairingNonce         string   `json:"pairingNonce"`
	Scopes               []string `json:"scopes"`
	IssuedAt             string   `json:"issuedAt"`
	ExpiresAt            string   `json:"expiresAt"`
	CredentialExpiresAt  string   `json:"credentialExpiresAt"`
	KeyVersion           int      `json:"keyVersion"`
	EnvelopeVersion      int      `json:"envelopeVersion"`
}

type DeviceEnvelope struct {
	Version    int                  `json:"version"`
	Algorithm  string               `json:"alg"`
	Header     DeviceEnvelopeHeader `json:"header"`
	WrappedKey string               `json:"wrappedKey"`
	IV         string               `json:"iv"`
	Ciphertext string               `json:"ciphertext"`
}

type EnvelopePayload struct {
	DeviceEnvelopeHeader
	BootstrapSecret string `json:"bootstrapSecret"`
	Keys            []struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		Version int    `json:"version"`
		Value   string `json:"value"`
	} `json:"keys"`
	Canary string `json:"canary"`
}

func GenerateDeviceIdentity() (*rsa.PrivateKey, string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, "", "", err
	}
	n := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes())
	jwk := fmt.Sprintf(`{"kty":"RSA","n":"%s","e":"AQAB"}`, n)
	digest := sha256.Sum256([]byte(jwk))
	return privateKey, jwk, hex.EncodeToString(digest[:]), nil
}

func MarshalPrivateKey(privateKey *rsa.PrivateKey) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}))
}

func ParsePrivateKey(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("invalid private key")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func OpenEnvelope(raw string, privateKey *rsa.PrivateKey) (EnvelopePayload, error) {
	var envelope DeviceEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return EnvelopePayload{}, err
	}
	if envelope.Version != 1 || envelope.Algorithm != envelopeAlgorithm {
		return EnvelopePayload{}, errors.New("unsupported envelope suite")
	}
	wrapped, err := base64.RawURLEncoding.DecodeString(envelope.WrappedKey)
	if err != nil {
		return EnvelopePayload{}, err
	}
	key, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, wrapped, nil)
	if err != nil {
		return EnvelopePayload{}, fmt.Errorf("unwrap envelope key: %w", err)
	}
	if len(key) != 32 {
		return EnvelopePayload{}, errors.New("invalid envelope key length")
	}
	iv, err := base64.RawURLEncoding.DecodeString(envelope.IV)
	if err != nil {
		return EnvelopePayload{}, err
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return EnvelopePayload{}, err
	}
	aad, _ := json.Marshal(envelope.Header)
	plaintext, err := openAESGCM(key, iv, ciphertext, aad)
	if err != nil {
		return EnvelopePayload{}, fmt.Errorf("open envelope: %w", err)
	}
	var payload EnvelopePayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return EnvelopePayload{}, err
	}
	if payload.Canary != "ratio-local-ai-v1" || payload.DeviceID != envelope.Header.DeviceID || payload.WorkspaceID != envelope.Header.WorkspaceID {
		return EnvelopePayload{}, errors.New("envelope context mismatch")
	}
	return payload, nil
}

func openAESGCM(key, iv, ciphertext, aad []byte) ([]byte, error) {
	block, err := newAESBlock(key)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(block)
	if err != nil {
		return nil, err
	}
	if len(iv) != gcm.NonceSize() {
		return nil, errors.New("invalid GCM nonce")
	}
	return gcm.Open(nil, iv, ciphertext, aad)
}

// Wrappers keep the crypto imports near the only code that handles plaintext keys.
func newAESBlock(key []byte) (cipherBlock, error)  { return aesNewCipher(key) }
func newGCM(block cipherBlock) (cipherAEAD, error) { return cipherNewGCM(block) }

type snapshotResponse struct {
	Transactions []struct {
		ID            string    `json:"id"`
		WorkspaceID   string    `json:"workspaceId"`
		BookingDate   time.Time `json:"bookingDate"`
		EncryptedBlob *string   `json:"encryptedBlob"`
		EncryptionIV  *string   `json:"encryptionIv"`
		Version       int64     `json:"version"`
		// Accrual period: the app spreads the amount evenly over its months.
		CompetenzaFrom *time.Time `json:"competenzaFrom"`
		CompetenzaTo   *time.Time `json:"competenzaTo"`
		// Prisma serialises Decimal as a JSON string; a number is accepted too.
		Amount   json.RawMessage `json:"amount"`
		Currency *string         `json:"currency"`
	} `json:"transactions"`
	HasMore        bool    `json:"hasMore"`
	NextCursor     *string `json:"nextCursor"`
	SnapshotCursor string  `json:"snapshotCursor"`
}

func (c *Client) fetchSnapshot(ctx context.Context, workspaceID string) (domain.Snapshot, error) {
	var all []domain.EncryptedTransaction
	cursor := ""
	sourceRevision := ""
	for {
		path := "/api/transactions/sync?full=true&limit=500&workspaceId=" + url.QueryEscape(workspaceID)
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor) + "&snapshotCursor=" + url.QueryEscape(sourceRevision)
		}
		var page snapshotResponse
		if _, err := c.request(ctx, http.MethodGet, path, nil, nil, &page); err != nil {
			return domain.Snapshot{}, err
		}
		if sourceRevision == "" {
			sourceRevision = page.SnapshotCursor
		}
		for _, row := range page.Transactions {
			if row.EncryptedBlob == nil || row.EncryptionIV == nil {
				continue
			}
			plainAmount, err := plainDecimal(row.Amount)
			if err != nil {
				return domain.Snapshot{}, fmt.Errorf("transaction %q: %w", row.ID, err)
			}
			plainCurrency := ""
			if row.Currency != nil {
				plainCurrency = *row.Currency
			}
			// Dates are read in the app's time zone: a period starting at
			// local midnight on the 1st is the previous day in UTC.
			tx := domain.EncryptedTransaction{ID: row.ID, WorkspaceID: row.WorkspaceID,
				BookingDate: localDate(row.BookingDate), EncryptedBlob: *row.EncryptedBlob, EncryptionIV: *row.EncryptionIV,
				ServerVersion: row.Version, PlainAmount: plainAmount, PlainCurrency: plainCurrency}
			if row.CompetenzaFrom != nil {
				tx.CompetenzaFrom = localDate(*row.CompetenzaFrom)
			}
			if row.CompetenzaTo != nil {
				tx.CompetenzaTo = localDate(*row.CompetenzaTo)
			}
			all = append(all, tx)
		}
		if !page.HasMore || page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	coverage := domain.Coverage{IsComplete: true, LastSyncedAt: time.Now().UTC().Format(time.RFC3339), SourceRevision: sourceRevision}
	if len(all) > 0 {
		coverage.CoverageFrom = all[len(all)-1].BookingDate
		coverage.CoverageTo = all[0].BookingDate
	}
	return domain.Snapshot{WorkspaceID: workspaceID, Transactions: all, Coverage: coverage}, nil
}

// appZone is the time zone the Ratio app reads dates in (Italian users'
// browsers); UTC is the fallback when tzdata is missing.
var appZone = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/Rome"); err == nil {
		return loc
	}
	return time.UTC
}()

func localDate(t time.Time) string { return t.In(appZone).Format("2006-01-02") }

type Repository struct {
	Client      *Client
	WorkspaceID string
}

func (r Repository) Snapshot(ctx context.Context) (domain.Snapshot, error) {
	if r.Client == nil || r.WorkspaceID == "" {
		return domain.Snapshot{}, errors.New("remote repository is not configured")
	}
	return r.Client.fetchSnapshot(ctx, r.WorkspaceID)
}

type StaticKeyProvider struct {
	WorkspaceID string
	Key         []byte
}

func (p StaticKeyProvider) MasterKey(_ context.Context, workspaceID string) ([]byte, error) {
	if workspaceID != p.WorkspaceID {
		return nil, errors.New("workspace key mismatch")
	}
	return append([]byte(nil), p.Key...), nil
}

func RandomSecret(size int) (string, error) {
	value := make([]byte, size)
	_, err := rand.Read(value)
	return base64.RawURLEncoding.EncodeToString(value), err
}
func SHA256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
func RandomCredentialID() (string, error) {
	value, err := RandomSecret(12)
	if err != nil {
		return "", err
	}
	return value, nil
}

// plainDecimal reads the optional plaintext amount column: null or absent
// gives "", a JSON string or number gives its decimal text.
func plainDecimal(raw json.RawMessage) (string, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return "", nil
	}
	if strings.HasPrefix(text, "\"") {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", fmt.Errorf("plaintext amount: %w", err)
		}
		return strings.TrimSpace(s), nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return "", fmt.Errorf("plaintext amount: %w", err)
	}
	return n.String(), nil
}

func (r Repository) CategoryCatalog(ctx context.Context) (*domain.EncryptedCategoryCatalog, error) {
	var response struct {
		Catalog *domain.EncryptedCategoryCatalog `json:"catalog"`
	}
	_, err := r.Client.request(ctx, http.MethodGet, "/api/local-ai/categories?workspaceId="+url.QueryEscape(r.WorkspaceID), nil, nil, &response)
	if err != nil {
		return nil, err
	}
	return response.Catalog, nil
}
