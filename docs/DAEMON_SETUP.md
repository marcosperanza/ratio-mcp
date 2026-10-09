# Ratio local service

The production path separates the long-lived key holder from short-lived MCP
hosts. `ratio-mcp daemon` is the only runtime process that opens the encrypted
vault. Claude Code and llama.cpp launch `ratio-mcp serve --stdio --socket ...`,
which can list tools without a credential and delegates calls over a Unix
socket.

## Raspberry Pi system service

The checked-in unit and commands are an installation example for a Linux
Raspberry Pi. Replace `ratio-user`, `/home/ratio-user`, `/opt/ratio-mcp/bin/ratio-mcp` and UID
`1000` with your login account, its home, the installed binary path and the
output of `id -u`. Run the daemon and MCP host as the same OS user; the current
tools socket is mode `0600` and authorized by peer UID. This systemd service
template is Linux-only; macOS and Windows run the MCP directly as an AI-launched
stdio process. See [DESKTOP_INSTALL.md](DESKTOP_INSTALL.md). Each Linux instance
name is a local profile with independent vault, credential, sockets and lock
state. The current installation uses `default`;
additional examples are `family` or `consulting`.
The encrypted credential is bound to the machine's systemd host key because
this Raspberry does not currently expose a complete TPM2 device.

One-time credential enrollment reads the vault passphrase from a terminal,
verifies it against the vault and passes it to `systemd-creds` over a pipe:

```bash
sudo systemd-creds setup
sudo install -d -m 700 /etc/ratio-mcp/default
sudo -v

/path/to/ratio-mcp/bin/ratio-mcp credential-store install \
  --vault $HOME/.config/ratio-mcp/vault.json \
  --use-sudo \
  --output /tmp/ratio-mcp-vault-passphrase.cred

sudo install -o root -g root -m 600 \
  /tmp/ratio-mcp-vault-passphrase.cred \
  /etc/ratio-mcp/default/vault-passphrase.cred
sudo install -o root -g root -m 600 \
  deploy/systemd/profile.env.example \
  /etc/ratio-mcp/default/config
sudoedit /etc/ratio-mcp/default/config
```

In that config file replace `ratio-user` with your login name and `1000` with
`id -u`. Install the service template, then edit the installed copy before
enabling it:

```bash
sudo install -o root -g root -m 644 \
  deploy/systemd/ratio-mcp@.service \
  /etc/systemd/system/ratio-mcp@.service
sudoedit /etc/systemd/system/ratio-mcp@.service
```

Set `User` and `Group` to the same login account used by Claude, and set
`ExecStart` to the installed MCP binary path. This is required because the
tools socket is mode `0600` and the daemon checks the connecting process UID.

Remove the temporary encrypted copy after installation. The plaintext is not
stored in the unit, process arguments or environment. Root access or a complete
copy of the host key and encrypted credential can recover it; host-key
autounlock is not equivalent to TPM-backed storage.

`--use-sudo` invokes `sudo -n systemd-creds`: the preceding `sudo -v` is
deliberate. It prevents `sudo` from consuming the vault passphrase from stdin
as an operating-system password.

Install and verify the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now ratio-mcp@default.service

/path/to/ratio-mcp/bin/ratio-mcp status \
  --admin-socket /run/ratio-mcp/default/admin.sock
```

Expected status is `vault=unlocked ready=true`. Test an actual authorized tool
before removing the previous runtime passphrase workaround. A reboot test is a
separate operational step and must be scheduled; a service restart verifies the
same non-interactive credential recovery path without rebooting the machine.

Administrative lock persists across service restarts:

```bash
ratio-mcp lock --admin-socket /run/ratio-mcp/default/admin.sock
ratio-mcp unlock --admin-socket /run/ratio-mcp/default/admin.sock
```

`unlock` reloads the system credential; it never asks Claude or the MCP stdio
process for a password.

## MCP hosts

Use only the tools socket:

```json
{
  "mcpServers": {
    "ratio": {
      "command": "/path/to/ratio-mcp/bin/ratio-mcp",
      "args": [
        "serve",
        "--stdio",
        "--socket",
        "/run/ratio-mcp/default/tools.sock"
      ],
      "timeout_ms": 45000
    }
  }
}
```

Never expose `/run/ratio-mcp/default/admin.sock` to an MCP host. The current unit and AI
host run under the same Unix account, so this is an API separation rather than
strong isolation. A public release should use a dedicated service user and a
tools group after cross-UID tests.

## Failure behavior

- Missing or undecryptable system credential: daemon stays locked and sockets
  remain available for status.
- Wrong vault password: daemon stays locked with `RATIO_LOCKED`.
- Administrative lock creates `/var/lib/ratio-mcp/<profile>/autounlock.paused`, surviving
  daemon and machine restarts until `unlock` succeeds.
- Daemon unavailable: MCP initialization and tool listing still work; tool calls
  return `RATIO_UNAVAILABLE` immediately.
- Credential expiry or revocation: no fallback to fixtures, cookies or cloud.
