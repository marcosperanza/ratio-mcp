// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/marcosperanza/ratio-mcp/internal/fixture"
	"github.com/marcosperanza/ratio-mcp/internal/ipc"
	"github.com/marcosperanza/ratio-mcp/internal/keystore"
	"github.com/marcosperanza/ratio-mcp/internal/mcp"
	"github.com/marcosperanza/ratio-mcp/internal/policy"
	"github.com/marcosperanza/ratio-mcp/internal/remote"
	"github.com/marcosperanza/ratio-mcp/internal/service"
	"github.com/marcosperanza/ratio-mcp/internal/vault"
	"golang.org/x/term"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "ratio-mcp: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("a command is required")
	}
	switch args[0] {
	case "daemon":
		return runDaemon(args[1:], stdout, stderr)
	case "serve":
		return runServe(args[1:], stdin, stdout, stderr)
	case "status":
		return runAdminCommand("admin.status", args[1:], stdout, stderr)
	case "unlock":
		return runAdminCommand("admin.unlock", args[1:], stdout, stderr)
	case "lock":
		return runAdminCommand("admin.lock", args[1:], stdout, stderr)
	case "credential-store":
		return runCredentialStore(args[1:], stderr)
	case "pair":
		return runPair(args[1:], stdin, stdout, stderr)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "version":
		_, err := fmt.Fprintf(stdout, "ratio-mcp %s (commit %s, built %s)\n", version, commit, date)
		return err
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runServe(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fixtureDir := flags.String("fixture-dir", defaultFixtureDir(), "directory containing encrypted development fixtures")
	vaultPath := flags.String("vault", "", "encrypted Local AI vault (enables real Ratio mode)")
	passphraseFile := flags.String("passphrase-file", "", "root/user-only runtime file containing the vault passphrase")
	socketPath := flags.String("socket", "", "Ratio local service tools socket")
	stdio := flags.Bool("stdio", true, "serve MCP over standard input/output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected serve arguments: %v", flags.Args())
	}
	if *vaultPath != "" {
		if err := validateVaultLocation(*vaultPath); err != nil {
			return err
		}
	}
	if !*stdio {
		return errors.New("phase 0A supports only --stdio")
	}
	if *passphraseFile != "" && *vaultPath == "" {
		return errors.New("--passphrase-file requires --vault")
	}
	if *socketPath != "" && (*vaultPath != "" || *passphraseFile != "") {
		return errors.New("--socket cannot be combined with --vault or --passphrase-file")
	}
	var analytics *service.Analytics
	var err error
	mode := "fixture"
	if *socketPath != "" {
		analyticsClient := ipc.AnalyticsClient{Client: &ipc.Client{Path: *socketPath}}
		server, serverErr := mcp.NewServer("ratio-mcp", version, analyticsClient)
		if serverErr != nil {
			return serverErr
		}
		fmt.Fprintf(stderr, "ratio-mcp %s: bridging MCP to local service over stdio\n", version)
		return server.Serve(context.Background(), stdin, stdout)
	} else if *vaultPath != "" {
		analytics, err = buildRealAnalytics(*vaultPath, *passphraseFile, stderr)
		mode = "real encrypted Ratio data"
	} else {
		analytics, err = buildAnalytics(*fixtureDir)
	}
	if err != nil {
		return err
	}
	server, err := mcp.NewServer("ratio-mcp", version, analytics)
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "ratio-mcp %s: serving %s over stdio\n", version, mode)
	return server.Serve(context.Background(), stdin, stdout)
}

func runPair(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("pair", flag.ContinueOnError)
	flags.SetOutput(stderr)
	serverURL := flags.String("server", "https://www.ratio-fin.com", "Ratio server origin")
	vaultPath := flags.String("vault", defaultVaultPath(), "encrypted vault path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected pair arguments: %v", flags.Args())
	}
	if err := validateVaultLocation(*vaultPath); err != nil {
		return err
	}

	passphrase, err := readPassphrase("New vault passphrase: ", stderr)
	if err != nil {
		return err
	}
	defer wipe(passphrase)
	confirmation, err := readPassphrase("Repeat vault passphrase: ", stderr)
	if err != nil {
		return err
	}
	if string(passphrase) != string(confirmation) {
		wipe(confirmation)
		return errors.New("passphrases do not match")
	}
	wipe(confirmation)

	privateKey, publicJWK, fingerprint, err := remote.GenerateDeviceIdentity()
	if err != nil {
		return err
	}
	pollingSecret, err := remote.RandomSecret(32)
	if err != nil {
		return err
	}
	nonce, err := remote.RandomSecret(32)
	if err != nil {
		return err
	}
	client, err := remote.NewClient(*serverURL, "")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 11*time.Minute)
	defer cancel()
	pairing, err := client.CreatePairing(ctx, remote.CreatePairingRequest{PublicKeyJWK: publicJWK, PairingNonce: nonce, PollingSecretHash: remote.SHA256Hex(pollingSecret)})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Open %s/it/dashboard/connector and enter code: %s\n", strings.TrimRight(*serverURL, "/"), pairing.UserCode)
	fmt.Fprintf(stdout, "Verify this full device fingerprint in Ratio:\n%s\n", groupDigest(fingerprint))

	var approved remote.PollResponse
	for {
		if time.Now().After(pairing.ExpiresAt) {
			return errors.New("pairing expired")
		}
		response, ready, pollErr := client.Poll(ctx, pairing.ID, pollingSecret)
		if pollErr == nil && ready {
			approved = response
			break
		}
		if pollErr != nil {
			return pollErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	if remote.SHA256Hex(approved.Envelope) != approved.EnvelopeDigest {
		return errors.New("envelope digest mismatch")
	}
	payload, err := remote.OpenEnvelope(approved.Envelope, privateKey)
	if err != nil {
		return err
	}
	if payload.PairingID != pairing.ID || payload.DeviceID != pairing.DeviceID || payload.PairingNonce != nonce || payload.PublicKeyFingerprint != fingerprint || payload.WorkspaceID != approved.WorkspaceID {
		return errors.New("approved envelope does not match the local pairing")
	}
	fmt.Fprintf(stdout, "Verify this approval transcript in Ratio:\n%s\n", groupDigest(approved.TranscriptDigest))
	fmt.Fprint(stderr, "Type YES after comparing the complete transcript: ")
	var answer string
	if _, err := fmt.Fscanln(stdin, &answer); err != nil {
		return err
	}
	if answer != "YES" {
		return errors.New("pairing cancelled: transcript was not confirmed")
	}

	masterKey := ""
	for _, key := range payload.Keys {
		if key.Type == "MASTER_KEY" {
			masterKey = key.Value
			break
		}
	}
	if masterKey == "" {
		return errors.New("envelope contains no master key")
	}
	credentialID, err := remote.RandomCredentialID()
	if err != nil {
		return err
	}
	credentialSecret, err := remote.RandomSecret(32)
	if err != nil {
		return err
	}
	apiKey := "ratio_lai_" + credentialID + "." + credentialSecret
	vaultData := vault.Data{ServerURL: *serverURL, PairingID: pairing.ID, DeviceID: pairing.DeviceID, WorkspaceID: approved.WorkspaceID, PrivateKeyPEM: remote.MarshalPrivateKey(privateKey), MasterKey: masterKey, APIKey: apiKey, Scopes: approved.Scopes, CredentialExpiresAt: approved.CredentialExpiresAt.Format(time.RFC3339), ActivationPending: true}
	if err := vault.Save(*vaultPath, vaultData, passphrase); err != nil {
		return err
	}
	if err := client.Complete(ctx, pairing.ID, pollingSecret, payload.BootstrapSecret, credentialID, remote.SHA256Hex(credentialSecret)); err != nil {
		return fmt.Errorf("vault saved but activation is pending: %w", err)
	}
	vaultData.ActivationPending = false
	if err := vault.Save(*vaultPath, vaultData, passphrase); err != nil {
		return err
	}
	authenticated, err := remote.NewClient(*serverURL, apiKey)
	if err != nil {
		return err
	}
	if err := authenticated.Acknowledge(ctx, approved.WorkspaceID); err != nil {
		return fmt.Errorf("paired, but envelope acknowledgement failed: %w", err)
	}
	if err := keystore.StoreDesktopCredential(*vaultPath, passphrase); err != nil {
		return fmt.Errorf("device paired and vault saved, but the passphrase could not be stored: %w; run `ratio-mcp credential-store install --vault %q`", err, *vaultPath)
	}
	fmt.Fprintf(stdout, "Local AI paired. Encrypted vault: %s\n", *vaultPath)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		fmt.Fprintln(stdout, "Vault passphrase saved in the operating system credential store.")
	}
	return nil
}

func buildRealAnalytics(vaultPath, passphraseFile string, stderr io.Writer) (*service.Analytics, error) {
	var passphrase []byte
	var err error
	if passphraseFile != "" {
		passphrase, err = readPassphraseFile(passphraseFile)
	} else {
		passphrase, err = keystore.LoadDesktopCredential(vaultPath)
		if errors.Is(err, keystore.ErrDesktopCredentialUnavailable) {
			passphrase, err = readPassphrase("Vault passphrase: ", stderr)
		}
	}
	if err != nil {
		return nil, err
	}
	defer wipe(passphrase)
	return buildRealAnalyticsFromPassphrase(vaultPath, passphrase)
}

func buildRealAnalyticsFromPassphrase(vaultPath string, passphrase []byte) (*service.Analytics, error) {
	data, err := vault.Load(vaultPath, passphrase)
	if err != nil {
		return nil, err
	}
	if data.ActivationPending {
		return nil, errors.New("device activation is incomplete; rerun pairing recovery")
	}
	masterKey, err := decodeBase64URLKey(data.MasterKey)
	if err != nil {
		return nil, err
	}
	client, err := remote.NewClient(data.ServerURL, data.APIKey)
	if err != nil {
		return nil, err
	}
	return service.NewAnalytics(remote.Repository{Client: client, WorkspaceID: data.WorkspaceID}, remote.StaticKeyProvider{WorkspaceID: data.WorkspaceID, Key: masterKey}, policy.FixtureDevelopment())
}

func readPassphraseFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open passphrase file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat passphrase file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("passphrase file must be a regular file")
	}
	if err := validatePassphraseFilePermissions(path, info); err != nil {
		return nil, errors.New("passphrase file must not be accessible by group or others")
	}
	if err := verifyOwnedFile(path, info); err != nil {
		return nil, errors.New("passphrase file must be owned by the current user")
	}
	if info.Size() <= 0 || info.Size() > 4096 {
		return nil, errors.New("passphrase file has an invalid size")
	}
	value, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return nil, fmt.Errorf("read passphrase file: %w", err)
	}
	value = []byte(strings.TrimSuffix(strings.TrimSuffix(string(value), "\n"), "\r"))
	if len(value) == 0 {
		return nil, errors.New("passphrase file is empty")
	}
	return value, nil
}

func readPassphrase(prompt string, output io.Writer) ([]byte, error) {
	fmt.Fprint(output, prompt)
	terminal := os.Stdin
	var err error
	if runtime.GOOS != "windows" {
		terminal, err = os.OpenFile("/dev/tty", os.O_RDWR, 0)
	}
	if runtime.GOOS == "windows" && !term.IsTerminal(int(terminal.Fd())) {
		return nil, errors.New("a terminal is required to unlock the vault")
	}
	if err != nil {
		return nil, errors.New("a controlling terminal is required to unlock the vault")
	}
	if terminal != os.Stdin {
		defer terminal.Close()
	}
	if !term.IsTerminal(int(terminal.Fd())) {
		return nil, errors.New("a terminal is required to unlock the vault")
	}
	value, err := term.ReadPassword(int(terminal.Fd()))
	fmt.Fprintln(output)
	return value, err
}

func decodeBase64URLKey(value string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return nil, errors.New("invalid master key in vault")
	}
	return decoded, nil
}

func defaultVaultPath() string {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "ratio-mcp.vault"
	}
	return filepath.Join(directory, "ratio-mcp", "vault.json")
}

func groupDigest(value string) string {
	var parts []string
	for len(value) > 4 {
		parts = append(parts, value[:4])
		value = value[4:]
	}
	if value != "" {
		parts = append(parts, value)
	}
	return strings.Join(parts, " ")
}

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func runDoctor(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fixtureDir := flags.String("fixture-dir", defaultFixtureDir(), "directory containing encrypted development fixtures")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected doctor arguments: %v", flags.Args())
	}
	analytics, err := buildAnalytics(*fixtureDir)
	if err != nil {
		return err
	}
	count, coverage, err := analytics.Validate(context.Background())
	if err != nil {
		return err
	}
	report := struct {
		Status           string `json:"status"`
		Mode             string `json:"mode"`
		EncryptedRecords int    `json:"encryptedRecords"`
		CoverageFrom     string `json:"coverageFrom"`
		CoverageTo       string `json:"coverageTo"`
		IsComplete       bool   `json:"isComplete"`
		SourceRevision   string `json:"sourceRevision"`
		CheckedAt        string `json:"checkedAt"`
	}{
		Status:           "ok",
		Mode:             "encrypted-fixture",
		EncryptedRecords: count,
		CoverageFrom:     coverage.CoverageFrom,
		CoverageTo:       coverage.CoverageTo,
		IsComplete:       coverage.IsComplete,
		SourceRevision:   coverage.SourceRevision,
		CheckedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func buildAnalytics(fixtureDir string) (*service.Analytics, error) {
	directory, err := filepath.Abs(fixtureDir)
	if err != nil {
		return nil, fmt.Errorf("resolve fixture directory: %w", err)
	}
	repository, err := fixture.LoadRepository(directory)
	if err != nil {
		return nil, err
	}
	keys, err := fixture.LoadKeyProvider(directory)
	if err != nil {
		return nil, err
	}
	return service.NewAnalytics(repository, keys, policy.FixtureDevelopment())
}

func defaultFixtureDir() string {
	if value := os.Getenv("RATIO_MCP_FIXTURE_DIR"); value != "" {
		return value
	}
	return filepath.Join("testdata", "fixtures")
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, `Usage:
  ratio-mcp daemon [--vault PATH] [--tools-socket PATH] [--admin-socket PATH]
  ratio-mcp serve --stdio --socket PATH
  ratio-mcp serve [--stdio] [--fixture-dir PATH]
  ratio-mcp serve [--stdio] --vault PATH [--passphrase-file PATH]  (legacy)
  ratio-mcp status|unlock|lock [--admin-socket PATH]
  ratio-mcp credential-store install [--vault PATH] --output PATH
  ratio-mcp pair [--server URL] [--vault PATH]
  ratio-mcp doctor [--fixture-dir PATH]
  ratio-mcp version

Use serve --socket for real data without interactive prompts. Without --socket
or --vault, serve reads encrypted synthetic fixtures. Pair creates a scoped
Local AI device and an encrypted local vault.`)
}
