# ratio-mcp

Project website: https://www.ratio-fin.com · [Online user manual](https://www.ratio-fin.com/it/manual)

Local MCP server for Ratio. It is designed to decrypt and analyse Ratio data on
hardware controlled by the user while the Ratio backend continues to store only
ciphertext.

Without a vault the server reads encrypted synthetic fixtures. The real-data
path pairs a dedicated Local AI device from Ratio, stores its scoped API key
and master key in an Argon2id/AES-GCM encrypted local vault, and downloads the
workspace's encrypted transactions.

## Pair a Local AI device

Run from an interactive terminal; the passphrase is read without echo:

```bash
./bin/ratio-mcp pair \
  --server https://www.ratio-fin.com \
  --vault "$HOME/.config/ratio-mcp/vault.json"
```

Enter the displayed code in Ratio under **Bank Connector → Local AI**. Compare
the complete public-key fingerprint before approval, then compare the approval
transcript displayed by both sides. The Ratio account password is never given
to this binary.

For a local Ratio checkout, use `--server http://127.0.0.1:3000` and open
`http://127.0.0.1:3000/it/dashboard/connector` when prompted.

On macOS and Windows, and for one-off recovery on Linux, MCP can open the vault
directly:

```bash
./bin/ratio-mcp serve --stdio --vault "$HOME/.config/ratio-mcp/vault.json"
```

macOS and Windows retrieve the passphrase from Keychain or Credential Manager
after pairing. On Linux, direct mode prompts for it; use the persistent daemon
below for unattended MCP hosts.

On macOS and Windows, Ratio MCP runs as a local stdio process launched by the AI
client. Pairing stores the vault passphrase in macOS Keychain or Windows
Credential Manager, so normal launches do not ask for it again:

```bash
ratio-mcp pair --server https://www.ratio-fin.com
```

Then configure Claude Code with `ratio-mcp serve --stdio --vault PATH`; see
[docs/DESKTOP_INSTALL.md](docs/DESKTOP_INSTALL.md) for installation and exact
commands. Existing vaults can be enrolled once with
`ratio-mcp credential-store install --vault PATH`.

On a Raspberry Pi or Linux server, the recommended persistent setup uses a
long-lived daemon and a machine credential. The daemon unlocks once, while
Claude Code, llama.cpp and other MCP hosts use a keyless stdio bridge:

```bash
ratio-mcp daemon \
  --vault "$HOME/.config/ratio-mcp/vault.json" \
  --tools-socket /run/ratio-mcp/default/tools.sock \
  --admin-socket /run/ratio-mcp/default/admin.sock

ratio-mcp serve --stdio --socket /run/ratio-mcp/default/tools.sock
```

On the Raspberry, the instance `ratio-mcp@default` asks systemd to decrypt the
profile's stored machine credential at service startup, including after reboot.
Additional profiles get independent credentials, vaults, sockets and state.
See [docs/DAEMON_SETUP.md](docs/DAEMON_SETUP.md). The `serve --vault` and
`--passphrase-file` path remains available only for recovery and compatibility;
do not put it in an MCP host configuration.

## Current tools

- `ratio_list_workspaces`: lists the workspaces currently authorized for the
  Local AI device, including name, ID, role and the pairing default.
- `ratio_get_month_summary`: exact income, expenses, period balance and expense
  categories for a `YYYY-MM` month and optional workspace name or ID.
- `ratio_search_transactions`: bounded search over descriptions, merchants and
  categories for a `YYYY-MM` month and optional workspace. It tolerates short
  natural-language queries, punctuation, month names and common Italian or
  English filler words.

Cross-workspace access requires the explicit `workspaces:read` device scope.
Ratio verifies current membership on every list and transaction request; the
shared master key alone never grants API access. Omitting `workspace` keeps the
workspace selected during pairing as the default.

Amounts are returned as signed integer minor units (`1234` means EUR 12.34). The
Go process performs every calculation and also returns a deterministic
`authoritativeText`; the language model should present those values without
recalculating them. Every response includes data coverage, completeness, source
revision and rules version.

## Build and verify on Raspberry Pi

Requirements: Go 1.22 or newer.

```bash
make check
make build
./bin/ratio-mcp doctor
./bin/ratio-mcp version
```

`doctor` validates that every fixture decrypts and parses, but does not print
transaction contents, keys or ciphertext.

## Run with llama.cpp

Add the following development-only entry to the `mcpServers` object used by
`llama-server`:

```json
{
  "ratio": {
    "command": "/absolute/path/to/ratio-mcp",
    "args": [
      "serve",
      "--stdio",
      "--fixture-dir",
      "/absolute/path/to/ratio-mcp/testdata/fixtures"
    ],
    "timeout_ms": 20000
  }
}
```

The server writes protocol messages to stdout and diagnostics to stderr. Run it
directly only for a JSON-RPC smoke test:

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
  | ./bin/ratio-mcp serve --stdio
```

Useful MiniCPM prompts against the synthetic dataset:

- `Usa Ratio e dimmi entrate, uscite e saldo di settembre 2026.`
- `Cerca la transazione del supermercato di settembre 2026.`
- `Dimmi il riepilogo di settembre 2026 senza usare strumenti.` The model must
  not invent fixture values when no tool result is available.

## Run with Claude Code

Register the same mock server as a local, project-specific MCP:

```bash
cd /path/to/ratio-mcp
claude mcp add --scope local ratio-mock -- \
  "$(pwd)/bin/ratio-mcp" serve --stdio \
  --fixture-dir "$(pwd)/testdata/fixtures"
claude mcp get ratio-mock
```

Then start Claude Code in this directory and ask it to use Ratio, or run a
non-interactive check:

```bash
claude -p --model haiku \
  "Usa Ratio per cercare la spesa del supermercato a settembre 2026."
```

The local configuration is private to this checkout. Claude can see the
plaintext returned by the selected tools; the mock contains synthetic data, but
the production policy must account for data sent to cloud models.

## Repository layout

```text
cmd/ratio-mcp/          CLI: serve, doctor, version
internal/core/          repository, key-provider and policy ports
internal/cryptoutil/    Ratio-compatible AES-256-GCM decoding
internal/fixture/       encrypted synthetic repository and test key provider
internal/mcp/           newline-delimited JSON-RPC/MCP stdio transport
internal/policy/        fixed local authorization and output limits
internal/service/       decryption and deterministic analytics
testdata/               encrypted fixtures and Web Crypto vector
docs/                   architecture and security boundaries
```

## Security boundary

Fixture keys are public deterministic test material and must never be reused.
For real data, the MCP process opens the encrypted local vault after device
pairing and requests authorized transaction ciphertext from Ratio. The vault
contains the device credential and workspace key; the process keeps plaintext
keys in memory only while running. MCP tools never expose keys, recovery blobs
or arbitrary ciphertext.

The MCP host can see every plaintext field returned by a tool. If that host uses
a cloud model, those returned fields can leave the home network. Output policy
therefore belongs in this server rather than in the model prompt.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and
[docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) for the implemented boundary and
remaining security work.

The real-data daemon, machine credential and per-profile systemd setup are in
[docs/DAEMON_SETUP.md](docs/DAEMON_SETUP.md). For a native macOS/Windows setup,
follow [docs/DESKTOP_INSTALL.md](docs/DESKTOP_INSTALL.md). To connect those
clients to an existing Raspberry Pi daemon instead, follow the optional
[restricted SSH guide](docs/REMOTE_MCP_MAC_WINDOWS.md).

## Release roadmap

Device pairing, scoped credentials and the encrypted key vault are implemented.
Each tool call fetches ciphertext from Ratio and decrypts it in the local MCP
process; no local transaction database is created. Linux uses the persistent
systemd daemon; macOS and Windows run the MCP locally and protect the vault
passphrase with the native OS credential store. The local AI host receives
plaintext tool results.
Review [docs/DESKTOP_INSTALL.md](docs/DESKTOP_INSTALL.md) before installing a
desktop binary.

### Workspace custom categories

When transactions contain `custom:<uuid>` category IDs, the MCP fetches the
workspace's encrypted catalog from `/api/local-ai/categories` using the existing
`transactions:read` scope and decrypts it locally with the paired master key.
Category IDs remain stable; results expose `categoryName` separately, and category
filters/search accept either IDs or custom names. Renaming or archiving does not
rewrite transaction history. Catalog names never determine transfer/investment
exclusions. A missing or unreadable catalog produces an explicit error rather
than silently grouping IDs under invented names. This requires the corresponding
Ratio server category-catalog migration/API; update both components before using
custom categories. Ordinary built-in categories retain their existing behavior.


## License and notices

Ratio MCP source code is licensed under **AGPL-3.0-only**. See [LICENSE](LICENSE), [licensing details](LICENSING.md), [third-party notices](THIRD_PARTY_NOTICES.md), and [trademark guidance](TRADEMARKS.md). The source archive corresponding to each binary release is available from the same GitHub Release.

For Ratio's online product manual, see [ratio-fin.com/it/manual](https://www.ratio-fin.com/it/manual). Tool-specific setup and operations guides are in [`docs/`](docs/).
