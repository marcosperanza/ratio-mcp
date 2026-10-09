# Connect Claude from macOS or Windows to Ratio MCP on a Raspberry Pi

This setup runs the Ratio MCP daemon and its encrypted vault on the Raspberry Pi. A Mac or Windows computer runs Claude Code and starts a short-lived MCP stdio process through OpenSSH. SSH transports the MCP protocol; a forced command on the Raspberry Pi allows the SSH key to run only the Ratio MCP tools bridge. The vault, device token, workspace key and decryption stay on the Raspberry Pi.

This is the supported cross-device setup. It does not install the Ratio daemon or vault on macOS/Windows. The client computer receives plaintext tool results, so a cloud model such as Claude may process those results according to the user's Claude settings.

## 1. Prepare the Raspberry Pi

Install and pair Ratio MCP on the Pi, then configure the persistent `ratio-mcp@default` service by following [DAEMON_SETUP.md](DAEMON_SETUP.md). Confirm that the daemon is unlocked and the tools socket exists:

```bash
ratio-mcp status --admin-socket /run/ratio-mcp/default/admin.sock
sudo -u ratio-user test -S /run/ratio-mcp/default/tools.sock
```

Use the same dedicated, unprivileged Unix account for the MCP service and SSH forced command. Replace `ratio-user` and the binary path below with the values used by your installation. Do not expose the admin socket over SSH or the network.

Create a dedicated SSH key on each client computer and add its public key to `/home/ratio-user/.ssh/authorized_keys`. Restrict that key to the stdio bridge. For example, the authorized-keys line should look like this (replace the key material):

```text
restrict,command="/opt/ratio-mcp/bin/ratio-mcp serve --stdio --socket /run/ratio-mcp/default/tools.sock" ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... ratio-mcp-client
```

`restrict` disables PTY, port forwarding, agent forwarding and X11 forwarding. The forced command ignores commands requested by the client and starts only the MCP bridge. Keep the SSH private key on its client computer and protect `authorized_keys` from other users. If your OpenSSH server is too old to support `restrict`, use `no-port-forwarding,no-agent-forwarding,no-X11-forwarding,no-pty` and test the restrictions explicitly.

The bridge runs as `ratio-user`, which must be the daemon's configured user because the tools socket is permission-protected and checks the connecting UID. If SSH must use a different Unix account, configure a dedicated tools group and validate cross-UID socket access before deployment; do not loosen the socket to world access.

## 2. Set up SSH on macOS

macOS includes OpenSSH. In Terminal, create a dedicated key (press Enter to leave the passphrase empty only if the machine account is protected by a strong login password and disk encryption; otherwise set a passphrase and use Keychain):

```bash
ssh-keygen -t ed25519 -f ~/.ssh/ratio-mcp -C ratio-mcp-client
```

Add the contents of `~/.ssh/ratio-mcp.pub` to the restricted `authorized_keys` entry on the Pi. On first connection, inspect the host-key fingerprint through a trusted Pi console, then connect interactively and compare it before accepting:

```bash
ssh-keyscan -t ed25519 raspberrypi.local
ssh -T -i ~/.ssh/ratio-mcp -o IdentitiesOnly=yes ratio-user@raspberrypi.local
```

Do not trust a fingerprint merely because `ssh-keyscan` returned it; verify it on the Pi with `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`. The forced command may print MCP protocol errors in this interactive test; that is expected. After accepting the verified host key, use the MCP configuration below.

## 3. Set up SSH on Windows

Install or enable **OpenSSH Client** in Windows Optional Features. In PowerShell:

```powershell
ssh-keygen -t ed25519 -f "$env:USERPROFILE\.ssh\ratio-mcp" -C ratio-mcp-client
Get-Content "$env:USERPROFILE\.ssh\ratio-mcp.pub"
```

Add that public key to the restricted `authorized_keys` entry on the Pi. Verify the Pi's SSH host-key fingerprint through a trusted console before accepting it in Windows:

```powershell
ssh-keyscan -t ed25519 raspberrypi.local
ssh -T -i "$env:USERPROFILE\.ssh\ratio-mcp" -o IdentitiesOnly=yes ratio-user@raspberrypi.local
```

Compare the fingerprint printed by `ssh-keyscan` with `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` on the Pi. Keep the private key under your Windows profile and restrict its ACL to your account. If Windows reports an overly permissive key file, run `icacls "$env:USERPROFILE\.ssh\ratio-mcp" /inheritance:r /grant:r "$env:USERNAME:(R)"` from PowerShell.

## 4. Configure Claude Code

On macOS, register the remote bridge for your user:

```bash
claude mcp add --scope user ratio -- \
  /usr/bin/ssh -T -i "$HOME/.ssh/ratio-mcp" \
  -o IdentitiesOnly=yes -o BatchMode=yes \
  ratio-user@raspberrypi.local
claude mcp get ratio
```

On Windows PowerShell, use the OpenSSH executable and your key path:

```powershell
claude mcp add --scope user ratio -- `
  C:\Windows\System32\OpenSSH\ssh.exe -T `
  -i "$env:USERPROFILE\.ssh\ratio-mcp" `
  -o IdentitiesOnly=yes -o BatchMode=yes `
  ratio-user@raspberrypi.local
claude mcp get ratio
```

Replace `raspberrypi.local` with the Pi's stable LAN hostname or address. `BatchMode=yes` makes Claude fail clearly instead of waiting for a password prompt. The authorized key's forced command starts the MCP server, so do not append a remote shell command. If Claude Code is installed in WSL, configure it inside WSL and use that environment's SSH key and `ssh` binary.

Restart Claude Code and check that `ratio` is connected. Try a narrow question such as: “Usa Ratio e dimmi entrate, uscite e saldo di settembre 2026.” The Pi must be running and the daemon unlocked. If you want access from outside the home network, use a private VPN such as WireGuard/Tailscale and keep SSH key restrictions in place; do not expose the MCP or admin socket directly to the public internet.

## Security notes

- Each client has its own SSH key so access can be revoked independently by removing one `authorized_keys` entry.
- The SSH key grants only the read-only Ratio MCP tools available through the tools socket. It does not grant a shell or administrative socket access.
- SSH encrypts transport between the client and Pi. The selected AI host still receives plaintext returned by tools; choose a local model if those fields must remain on your network.
- Ratio's server continues to store encrypted transaction data and does not receive the MCP master key.
- Back up the encrypted vault and systemd credential using the recovery procedure in [DAEMON_SETUP.md](DAEMON_SETUP.md); do not copy either into the client computer.
