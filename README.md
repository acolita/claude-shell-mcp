# claude-shell-mcp

[![Version](https://img.shields.io/badge/version-1.0.0-blue.svg)](https://github.com/acolita/claude-shell-mcp/releases)
[![Go Version](https://img.shields.io/badge/go-1.22+-00ADD8.svg)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

MCP server providing persistent, interactive shell sessions over SSH for LLM agents.

## Features

- **Persistent Sessions**: Shell state (cwd, env vars) survives across tool calls
- **SSH Support**: Connect to remote servers with password, key, or SSH agent auth
- **Prompt Detection**: Automatically detects sudo prompts, confirmations, etc.
- **Sudo Caching**: Securely caches sudo passwords with TTL-based expiration
- **Interrupt Pattern**: LLM sees every prompt and decides what to input

## Installation

### Download Binary

Download the latest release from [GitHub Releases](https://github.com/acolita/claude-shell-mcp/releases):

```bash
# Linux (amd64)
curl -Lo claude-shell-mcp https://github.com/acolita/claude-shell-mcp/releases/latest/download/claude-shell-mcp-linux-amd64
chmod +x claude-shell-mcp
sudo mv claude-shell-mcp /usr/local/bin/

# macOS (Apple Silicon)
curl -Lo claude-shell-mcp https://github.com/acolita/claude-shell-mcp/releases/latest/download/claude-shell-mcp-darwin-arm64
chmod +x claude-shell-mcp
sudo mv claude-shell-mcp /usr/local/bin/
```

### Build from Source

```bash
git clone https://github.com/acolita/claude-shell-mcp.git
cd claude-shell-mcp
make build
sudo cp claude-shell-mcp /usr/local/bin/
```

### Docker

```bash
docker pull ghcr.io/acolita/claude-shell-mcp:latest
docker run -v ~/.ssh:/root/.ssh:ro ghcr.io/acolita/claude-shell-mcp --mode ssh
```

### Verify Installation

```bash
claude-shell-mcp --version
```

## Configuration

### Claude Code

Add to your Claude Code MCP settings (`~/.claude/settings.json`):

```json
{
  "mcpServers": {
    "shell": {
      "command": "claude-shell-mcp",
      "args": ["--mode", "local"]
    }
  }
}
```

### Claude Desktop

Add to Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json` on macOS):

```json
{
  "mcpServers": {
    "shell": {
      "command": "/usr/local/bin/claude-shell-mcp",
      "args": ["--mode", "local"]
    }
  }
}
```

### SSH Configuration

For SSH sessions, create `~/.config/claude-shell-mcp/config.yaml`:

```yaml
mode: ssh

servers:
  - name: production
    host: prod.example.com
    port: 22
    user: deploy
    key_path: ~/.ssh/id_ed25519

security:
  sudo_cache_ttl: 5m
  idle_timeout: 30m
  max_sessions_per_user: 10

logging:
  level: info
  sanitize: true
```

Then configure Claude:

```json
{
  "mcpServers": {
    "remote-shell": {
      "command": "claude-shell-mcp",
      "args": ["--config", "~/.config/claude-shell-mcp/config.yaml"],
      "env": {
        "SSH_AUTH_SOCK": "/run/user/1000/keyring/ssh"
      }
    }
  }
}
```

## MCP Tools

### shell_session_create

Create a new shell session.

```json
{
  "mode": "local",        // or "ssh"
  "host": "server.com",   // for ssh mode
  "port": 22,             // for ssh mode
  "user": "username"      // for ssh mode
}
```

### shell_exec

Execute a command in a session.

```json
{
  "session_id": "sess_abc123",
  "command": "ls -la",
  "timeout_ms": 30000
}
```

Returns `status: "completed"` or `status: "awaiting_input"` if a prompt is detected.

### shell_provide_input

Respond to an interactive prompt.

```json
{
  "session_id": "sess_abc123",
  "input": "yes",
  "cache_for_sudo": true   // cache password for subsequent sudo prompts
}
```

### shell_interrupt

Send Ctrl+C to cancel a running command.

```json
{
  "session_id": "sess_abc123"
}
```

### shell_session_status

Get detailed session information including environment variables.

```json
{
  "session_id": "sess_abc123"
}
```

### shell_session_close

Close and cleanup a session.

```json
{
  "session_id": "sess_abc123"
}
```

## Example Workflows

### Deploy to Production

```
1. shell_session_create(mode="ssh", host="prod", user="deploy")
2. shell_exec("cd /app && git pull")
3. shell_exec("npm install")
4. shell_exec("sudo systemctl restart app")
   → Returns awaiting_input (sudo password)
5. shell_provide_input(input="***", cache_for_sudo=true)
   → Completes, sudo cached for 5 minutes
6. shell_session_close()
```

### Interactive npm init

```
1. shell_session_create(mode="local")
2. shell_exec("npm init")
   → Returns awaiting_input (package name prompt)
3. shell_provide_input("my-package")
   → Returns awaiting_input (version prompt)
4. shell_provide_input("1.0.0")
   ... (continue for each prompt)
```

## Security

- Passwords are stored as byte slices, not strings
- Secure memory wiping with multi-pass overwrite
- TTL-based expiration for cached credentials
- Sanitized logging (no credentials in logs)
- Host key verification via known_hosts

## Development

```bash
# Run tests
make test

# Build for all platforms
make build-all

# Test MCP handshake
make test-mcp
```

## License

MIT
