---
name: ratio-local-tools-setup
description: Install and configure Ratio MCP and/or Ratio Bank Connector on Linux, macOS, Windows, or Docker. Use when a user asks to pair Ratio Local AI, connect Claude Code or Codex to Ratio MCP, configure Enable Banking, install the bank connector, or troubleshoot either local tool.
---

# Set up Ratio local tools

Guide the user through installing the Ratio MCP, the Ratio Bank Connector, or both. They are separate tools: MCP reads and decrypts authorized Ratio transactions locally for an AI client; the connector retrieves bank transactions with the user's own Enable Banking application, encrypts them locally, and syncs them to Ratio. Installing one does not require installing the other.

## Start with the user's setup

Ask only for details needed to choose instructions: operating system and architecture, AI client (Claude Code or Codex), and which component(s) they want. Determine whether they prefer a desktop process or an always-on Linux/Docker host. Never ask them to paste a password, Ratio API key, Enable Banking private key, pairing secret, vault contents, or transaction data into chat. Have them enter secrets directly in the local CLI or Ratio UI.

Use the checked-in guides as the operational source of truth when this skill is installed from a repository:

- MCP desktop: `docs/DESKTOP_INSTALL.md`
- MCP Linux daemon: `docs/DAEMON_SETUP.md`
- MCP remote desktop-to-Raspberry setup: `docs/REMOTE_MCP_MAC_WINDOWS.md`
- Connector installation and Enable Banking setup: `docs/INSTALLATION.md`

If a guide is not present in the current checkout, use the matching public repository documentation: `https://github.com/marcosperanza/ratio-mcp` or `https://github.com/marcosperanza/ratio-bank-connector`. Use its Releases page for binaries. Do not invent CLI flags; check `--help` or the shipped guide when versions differ.

## Install the Bank Connector

1. Explain prerequisites: a Ratio workspace, a connector API key created in Ratio's connector settings, and the user's own Enable Banking restricted application with its App ID, private key, and linked/whitelisted accounts. Provider terms and eligibility belong to the user and may change; point them to Enable Banking's current terms and restricted-account documentation.
2. Download the archive matching OS/CPU from the official connector Releases page. Download its adjacent `.sha256` file and verify the checksum before execution. Releases may not be signed/notarized; state that plainly where relevant.
3. Run the local setup wizard (`bank-connector setup-addon`), then `bank-connector doctor --network`. Do not put credentials in shell history, issue reports, logs, screenshots, or an AI prompt.
4. Install the per-user background task using the repository guide: Linux `scripts/install-linux.sh` (systemd user service), macOS `scripts/install-macos.sh` (LaunchAgent), Windows `scripts/install-windows.ps1` (Scheduled Task). Docker is intended for Linux hosts/NAS/Raspberry Pi; use the repository's Compose instructions and private config/key directories.
5. Verify the service/task is active using the OS-specific commands in `docs/INSTALLATION.md`. In Ratio, add a bank account, wait for the connector authorization URL, complete bank consent, then confirm the account links and its first due sync completes. Don't schedule `sync --force`; it deliberately bypasses the normal provider-call interval.

## Install and pair Ratio MCP

1. Download the matching MCP release and checksum from its official Releases page, verify the checksum, and place the binary in a stable user-owned path. Do not run an unverified binary.
2. For macOS/Windows desktop use, run `ratio-mcp pair --server https://www.ratio-fin.com` in a local terminal. The user creates the vault passphrase locally and approves the device in Ratio under **Bank Connector → Local AI**. Have them compare the entire displayed public-key fingerprint and approval transcript before approving. Pairing stores the encrypted vault locally and enrolls the passphrase in macOS Keychain or Windows Credential Manager. Do not request or handle either secret.
3. For Linux desktop recovery, follow the desktop guide; for unattended Raspberry Pi/Linux, follow `docs/DAEMON_SETUP.md` and use the systemd credential flow. For a Mac/Windows AI client using a Raspberry Pi daemon, follow `docs/REMOTE_MCP_MAC_WINDOWS.md`. Never expose the admin socket to an AI client; only configure the tools socket.
4. Register the local stdio server with Claude Code or Codex. Use absolute executable and vault paths. For a desktop vault, the MCP arguments are `serve --stdio --vault <absolute-vault-path>`.

Claude Code example (macOS):

```sh
claude mcp add --scope user ratio -- \
  "$HOME/.local/bin/ratio-mcp" serve --stdio \
  --vault "$HOME/Library/Application Support/ratio-mcp/vault.json"
claude mcp get ratio
```

Claude Code PowerShell example (Windows):

```powershell
claude mcp add --scope user ratio -- `
  "$env:LOCALAPPDATA\Ratio\MCP\ratio-mcp.exe" serve --stdio `
  --vault "$env:APPDATA\ratio-mcp\vault.json"
claude mcp get ratio
```

Codex CLI example:

```sh
codex mcp add ratio -- /absolute/path/to/ratio-mcp serve --stdio --vault /absolute/path/to/vault.json
codex mcp list
```

If the CLI form is unavailable in the installed Codex version, configure its `~/.codex/config.toml` using the supported stdio server format:

```toml
[mcp_servers.ratio]
command = "/absolute/path/to/ratio-mcp"
args = ["serve", "--stdio", "--vault", "/absolute/path/to/vault.json"]
```

For a Linux daemon, replace `--vault <path>` with `--socket <tools-socket-path>` as shown in `docs/DAEMON_SETUP.md`. Never pass the vault passphrase as an argument or environment variable, and never place it in the agent configuration.

## Verify and explain the privacy boundary

Ask the user to restart/reload the AI client, confirm the MCP is connected (`claude mcp get ratio` or `codex mcp list`), and try a bounded month summary such as: “Usa Ratio e dimmi entrate, uscite e saldo di settembre 2026.” The MCP computes deterministic totals locally; the AI host receives the plaintext tool result. If the host uses a cloud model, transaction details returned by MCP may be sent to that provider. Recommend a local model when the user wants those results to stay on their device.

For troubleshooting, collect only OS/client versions, sanitized command output, and error text. Ask the user to redact URLs containing tokens, config values, private keys, vault files, and transaction descriptions. Check binary path, permissions, vault path, keychain enrollment, pairing/revocation status, connector service logs, network access, and system time. Never suggest resetting/re-pairing or rotating keys until explaining the effect on the existing installation.

Do not install or execute downloaded code, run privileged service changes, or edit user-level AI configuration without clear user authorization. Prefer commands from the shipped documentation and make each secret-entry step happen directly on the user's machine.
