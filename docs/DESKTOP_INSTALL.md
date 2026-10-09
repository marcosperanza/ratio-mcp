# Install Ratio MCP locally on macOS or Windows

The desktop build runs on the same computer as Claude Code (or another local
MCP host). Pair it with Ratio once; the vault stays on that computer, its
passphrase is saved in the operating system credential store, and each MCP tool
call fetches encrypted transactions from Ratio and decrypts them locally in
memory. No Raspberry Pi or remote MCP server is required.

## Requirements

- A Ratio account and workspace.
- macOS on Apple Silicon or Intel, or 64-bit Windows.
- Claude Code installed on the same computer.
- Network access during pairing and transaction synchronization.

## Install

Download the archive matching your operating system from the
[Ratio MCP GitHub Releases page](https://github.com/marcosperanza/ratio-mcp/releases)
and extract `ratio-mcp` (`ratio-mcp.exe` on Windows). Keep the
binary in a stable location because Claude Code stores its path. The release
archive also includes this README and its SHA-256 checksum. Verify the checksum
before running a downloaded binary.

For macOS, move the binary into a stable directory such as
`$HOME/.local/bin/ratio-mcp` and make it executable:

```bash
mkdir -p "$HOME/.local/bin"
mv ./ratio-mcp "$HOME/.local/bin/ratio-mcp"
chmod 700 "$HOME/.local/bin/ratio-mcp"
```

For Windows, extract `ratio-mcp.exe` to a stable directory such as
`%LOCALAPPDATA%\Ratio\MCP\ratio-mcp.exe`. No administrator privileges are
required. Keep the executable and vault inside your Windows user profile.

## Pair Ratio and enroll the passphrase

Run pairing from Terminal on macOS or PowerShell on Windows:

```text
ratio-mcp pair --server https://www.ratio-fin.com
```

The command asks you to create and confirm a vault passphrase, displays a
pairing code and the full device fingerprint, then waits for approval in Ratio
under **Bank Connector → Local AI**. Compare the fingerprint and approval
transcript in full before confirming. Pairing saves the encrypted vault under
your user configuration directory and saves its passphrase in the OS credential
store. On macOS, Keychain may ask you to allow the installed binary to access
the item, especially on first use or after replacing an unsigned build. Check
the binary's checksum and source before allowing access. Windows stores it in
Credential Manager for your account.

The default encrypted vault paths are:

| Platform | Vault |
| --- | --- |
| macOS | `~/Library/Application Support/ratio-mcp/vault.json` |
| Windows | `%APPDATA%\ratio-mcp\vault.json` |

Existing vault: if you already paired this device and have copied its encrypted
vault to this computer, enroll its passphrase once instead of pairing again:

```text
ratio-mcp credential-store install --vault "PATH_TO_VAULT"
```

The passphrase is read without echo, checked against the selected vault and
written to the native credential store. On Windows, the vault must be inside
`%APPDATA%\ratio-mcp`; this keeps it within the current user's application data
tree. Do not copy the plaintext passphrase into an environment variable,
configuration file or AI prompt.

## Connect Claude Code

On macOS, register the local MCP for your user (replace the binary path if you
chose a different install directory):

```bash
claude mcp add --scope user ratio -- \
  "$HOME/.local/bin/ratio-mcp" serve --stdio \
  --vault "$HOME/Library/Application Support/ratio-mcp/vault.json"
claude mcp get ratio
```

On Windows PowerShell:

```powershell
claude mcp add --scope user ratio -- `
  "$env:LOCALAPPDATA\Ratio\MCP\ratio-mcp.exe" serve --stdio `
  --vault "$env:APPDATA\ratio-mcp\vault.json"
claude mcp get ratio
```

Restart Claude Code, confirm that `ratio` is connected, then try: “Usa Ratio e
dimmi entrate, uscite e saldo di settembre 2026.” When Claude launches the MCP,
it reads the saved passphrase from the OS credential store and opens the vault
without asking for the passphrase again.

## Security and recovery

- The OS credential store holds the vault passphrase. The vault itself remains
  encrypted with Argon2id and AES-256-GCM and contains the device key and API
  credential. Transaction records are fetched as ciphertext for each tool call
  and decrypted in process memory; no local transaction database is created.
- macOS Keychain and Windows Credential Manager protect the passphrase under
  the signed-in user account. Anyone who can run code as that account may be
  able to request it; this does not protect against malware or a compromised
  user account.
- A cloud AI host receives plaintext fields returned by MCP tools. Select a
  local model if those results must remain on your device.
- Back up the encrypted vault and its passphrase separately. The credential
  store is tied to the local user account and should not be treated as a
  portable backup. To move to another computer, install the encrypted vault
  there and run `credential-store install` with the passphrase.
- Revoking the Local AI device in Ratio invalidates its API credential. Remove
  the corresponding credential from Keychain or Credential Manager and delete
  the local vault when retiring that installation.

## Other MCP hosts

Any host that can launch a local stdio MCP can use the same command and vault
path shown above. Do not give the host an admin socket or the vault passphrase.
