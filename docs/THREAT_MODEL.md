# Ratio MCP threat model

## Assets

- workspace master key and scoped device credential;
- decrypted transaction descriptions, amounts, categories and counterparties;
- encrypted local vault with device key and API credential;
- MCP tool results, which may be forwarded to a cloud model.

## Trust boundaries

The Ratio backend is outside the plaintext trust boundary. The computer running
`ratio-mcp` and its local secret store are trusted with decrypted data. On Linux
this can be a Raspberry Pi daemon; on macOS and Windows the MCP process runs
under the signed-in user. The MCP host is trusted only for fields allowed by its
assigned policy.
A cloud inference provider is an additional recipient of any tool output sent by
that host.

## Controls implemented now

- read-only, named tools instead of SQL or arbitrary record export;
- fixed result limit and closed JSON schemas;
- exact integer monetary arithmetic;
- AES-GCM authentication before parsing;
- scoped device authorization and a selected workspace, not arbitrary SQL;
- stdout reserved for MCP protocol traffic;
- diagnostics omit keys and transaction contents;
- vault encrypted with Argon2id and AES-256-GCM;
- macOS Keychain and Windows Credential Manager for desktop vault passphrases;
- passphrase protected by systemd credentials for the Linux daemon.

Transaction records are currently fetched as ciphertext from Ratio when a tool
runs, decrypted in memory and returned as bounded results. The MCP does not
persist a local transaction database or run a background sync.

## Remaining work

- redacted policy as the default for cloud-backed hosts;
- cumulative query budgets to limit reconstruction through repeated searches;
- signed and notarized desktop release artifacts with a safe update path;
- memory/core-dump hardening and tests proving secrets never enter logs;
- prompt-injection handling before email, PDF or Home Assistant data is added.

Revoking a device token stops future API access. It cannot erase plaintext or
keys already copied from a device that was compromised while unlocked.
