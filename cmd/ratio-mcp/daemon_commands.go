//go:build linux

// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/marcosperanza/ratio-mcp/internal/ipc"
	"github.com/marcosperanza/ratio-mcp/internal/keystore"
	"github.com/marcosperanza/ratio-mcp/internal/localcore"
)

func runDaemon(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("daemon", flag.ContinueOnError)
	flags.SetOutput(stderr)
	vaultPath := flags.String("vault", defaultVaultPath(), "encrypted Ratio vault")
	toolsSocket := flags.String("tools-socket", defaultSocketPath("tools.sock"), "tools socket path")
	adminSocket := flags.String("admin-socket", defaultSocketPath("admin.sock"), "administrative socket path")
	credentialName := flags.String("credential-name", keystore.DefaultCredentialName, "systemd credential name")
	credentialFile := flags.String("credential-file", "", "explicit systemd credential runtime file (testing/recovery)")
	pauseFile := flags.String("pause-file", defaultPausePath(), "persistent autounlock pause marker")
	allowedUID := flags.Uint("allowed-uid", uint(os.Geteuid()), "UID allowed to connect to local sockets")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected daemon arguments: %v", flags.Args())
	}
	provider := keystore.SystemdCredential{Name: *credentialName, Path: *credentialFile}
	manager, err := localcore.New(*vaultPath, *pauseFile, provider)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := manager.AutoUnlock(ctx); err != nil {
		fmt.Fprintf(stderr, "ratio-mcp daemon: autounlock unavailable (%s); service remains locked\n", errorCode(err))
	} else {
		fmt.Fprintln(stderr, "ratio-mcp daemon: vault unlocked from system credential")
	}
	go retryAutoUnlock(ctx, manager, stderr)
	servers := []*ipc.Server{
		{Path: *toolsSocket, Kind: ipc.ToolsSocket, Manager: manager, AllowedUID: uint32(*allowedUID)},
		{Path: *adminSocket, Kind: ipc.AdminSocket, Manager: manager, AllowedUID: uint32(*allowedUID)},
	}
	var wg sync.WaitGroup
	errorsChannel := make(chan error, len(servers))
	for _, server := range servers {
		wg.Add(1)
		go func(current *ipc.Server) {
			defer wg.Done()
			errorsChannel <- current.ListenAndServe(ctx)
		}(server)
	}
	fmt.Fprintf(stderr, "ratio-mcp daemon: tools=%s admin=%s\n", *toolsSocket, *adminSocket)
	select {
	case <-ctx.Done():
		wg.Wait()
		return nil
	case err := <-errorsChannel:
		stop()
		wg.Wait()
		return err
	}
}

func retryAutoUnlock(ctx context.Context, manager *localcore.Manager, stderr io.Writer) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			status := manager.Status()
			if status.Ready || status.AutounlockPaused || status.LastErrorCode == localcore.CodeAuthRequired {
				continue
			}
			if err := manager.AutoUnlock(ctx); err == nil {
				fmt.Fprintln(stderr, "ratio-mcp daemon: vault unlocked after retry")
			}
		}
	}
}

func runAdminCommand(method string, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet(method, flag.ContinueOnError)
	flags.SetOutput(stderr)
	adminSocket := flags.String("admin-socket", defaultSocketPath("admin.sock"), "administrative socket path")
	jsonOutput := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var status localcore.Status
	if err := (&ipc.Client{Path: *adminSocket}).Call(ctx, method, nil, &status); err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(status)
	}
	_, err := fmt.Fprintf(stdout, "vault=%s ready=%t autounlockPaused=%t credentialExpiresAt=%s\n", status.Vault, status.Ready, status.AutounlockPaused, status.CredentialExpiresAt)
	return err
}

func runCredentialStore(args []string, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "install" {
		return errors.New("credential-store requires the install command")
	}
	flags := flag.NewFlagSet("credential-store install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	vaultPath := flags.String("vault", defaultVaultPath(), "vault to verify")
	outputPath := flags.String("output", "", "encrypted systemd credential output")
	credentialName := flags.String("name", keystore.DefaultCredentialName, "credential name")
	withKey := flags.String("with-key", "host", "systemd credential key mode")
	useSudo := flags.Bool("use-sudo", false, "run systemd-creds through sudo -n (run sudo -v first)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *outputPath == "" {
		return errors.New("--output is required and no positional arguments are accepted")
	}
	passphrase, err := readPassphrase("Vault passphrase to store in the machine credential store: ", stderr)
	if err != nil {
		return err
	}
	defer wipe(passphrase)
	if _, err := buildRealAnalyticsFromPassphrase(*vaultPath, passphrase); err != nil {
		return errors.New("the supplied passphrase does not unlock the selected vault")
	}
	return encryptSystemdCredential(passphrase, *outputPath, *credentialName, *withKey, *useSudo)
}

func encryptSystemdCredential(passphrase []byte, outputPath, name, withKey string, useSudo bool) error {
	if name == "" || filepath.Base(name) != name {
		return errors.New("invalid credential name")
	}
	switch withKey {
	case "host", "tpm2", "host+tpm2":
	default:
		return errors.New("--with-key must be host, tpm2 or host+tpm2")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".credential-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return err
	}
	defer os.Remove(temporaryPath)
	command := systemdCredsCommand(useSudo, "encrypt", "--with-key="+withKey, "--name="+name, "-", temporaryPath)
	command.Stdin = bytes.NewReader(passphrase)
	var commandError bytes.Buffer
	command.Stderr = &commandError
	if err := command.Run(); err != nil {
		return fmt.Errorf("systemd-creds encryption failed: %s", sanitizedCommandError(commandError.String()))
	}
	if useSudo {
		// systemd-creds ran as root and created the file as root: hand it
		// back to the invoking user before tightening its mode, or chmod,
		// cleanup and the final rename fail (sticky /tmp).
		owner := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
		chown := exec.Command("sudo", "-n", "chown", owner, temporaryPath)
		chown.Stderr = &commandError
		if err := chown.Run(); err != nil {
			_ = exec.Command("sudo", "-n", "rm", "-f", temporaryPath).Run()
			return fmt.Errorf("cannot take ownership of the encrypted credential: %s", sanitizedCommandError(commandError.String()))
		}
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return err
	}
	// The embedded name must be given back: without it systemd-creds checks
	// it against the temporary file name and refuses.
	decrypted := systemdCredsCommand(useSudo, "decrypt", "--name="+name, temporaryPath, "-")
	var verified bytes.Buffer
	decrypted.Stdout = &verified
	decrypted.Stderr = &commandError
	if err := decrypted.Run(); err != nil || !bytes.Equal(verified.Bytes(), passphrase) {
		clearBytes(verified.Bytes())
		return errors.New("encrypted systemd credential could not be verified")
	}
	clearBytes(verified.Bytes())
	return os.Rename(temporaryPath, outputPath)
}

func systemdCredsCommand(useSudo bool, arguments ...string) *exec.Cmd {
	if useSudo {
		return exec.Command("sudo", append([]string{"-n", "systemd-creds"}, arguments...)...)
	}
	return exec.Command("systemd-creds", arguments...)
}

func sanitizedCommandError(value string) string {
	if value == "" {
		return "command failed"
	}
	if len(value) > 240 {
		value = value[:240]
	}
	return value
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func errorCode(err error) string {
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		return coded.ErrorCode()
	}
	return "RATIO_UNAVAILABLE"
}

func defaultSocketPath(name string) string {
	if _, err := os.Stat("/run/ratio-mcp"); err == nil {
		return filepath.Join("/run/ratio-mcp", name)
	}
	runtimeDirectory := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDirectory == "" {
		runtimeDirectory = filepath.Join("/run/user", strconv.Itoa(os.Geteuid()))
	}
	return filepath.Join(runtimeDirectory, "ratio-mcp", name)
}

func defaultPausePath() string {
	directory, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", "autounlock.paused")
	}
	return filepath.Join(directory, "ratio-mcp", "autounlock.paused")
}
