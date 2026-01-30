# Claude Code Integration Guide

This guide covers best practices for integrating `claude-shell-mcp` with Claude Code (the CLI tool for AI-assisted development).

## Overview

Claude Code is a command-line interface that enables AI-assisted software development. When combined with `claude-shell-mcp`, it gains the ability to:

- Execute commands on remote servers via SSH
- Handle interactive prompts (sudo, confirmations, wizards)
- Maintain persistent shell state across tool calls
- Perform complex multi-server deployments

## Configuration

### MCP Settings File

Add `claude-shell-mcp` to your Claude Code MCP settings:

**Location:** `~/.config/claude-code/mcp_servers.json` (Linux) or `~/Library/Application Support/claude-code/mcp_servers.json` (macOS)

```json
{
  "mcpServers": {
    "remote-shell": {
      "command": "claude-shell-mcp",
      "args": ["--config", "/path/to/config.yaml"],
      "env": {
        "SSH_AUTH_SOCK": "/run/user/1000/ssh-agent.socket"
      }
    }
  }
}
```

### Configuration File

Create a configuration file for your servers:

```yaml
# ~/.config/claude-shell-mcp/config.yaml
mode: ssh

servers:
  - name: production
    host: prod.example.com
    user: deploy
    key_path: ~/.ssh/prod_ed25519

  - name: staging
    host: staging.example.com
    user: deploy
    key_path: ~/.ssh/staging_ed25519

security:
  sudo_cache_ttl: 5m
  max_sessions_per_user: 10
  command_blocklist:
    - "rm -rf /"
    - "mkfs"
    - "> /dev/sda"

logging:
  level: info
  sanitize: true

recording:
  enabled: true
  path: ~/.local/share/claude-shell-mcp/recordings
```

## Usage Patterns

### Pattern 1: Direct Server Commands

For simple command execution:

```
User: "Check disk space on production server"

Claude Code:
1. shell_session_create(mode="ssh", host="prod.example.com", user="deploy")
2. shell_exec(session_id, "df -h")
3. shell_session_close(session_id)
```

### Pattern 2: Multi-Step Deployments

For complex workflows with state persistence:

```
User: "Deploy the latest code to staging"

Claude Code:
1. shell_session_create(mode="ssh", host="staging.example.com")
2. shell_exec("cd /var/www/app")
3. shell_exec("git pull origin main")
4. shell_exec("npm install")
5. shell_exec("npm run build")
6. shell_exec("sudo systemctl restart app")
   -> Returns awaiting_input (sudo password)
7. shell_provide_input(input="***", cache_for_sudo=true)
8. shell_exec("curl localhost:3000/health")
9. shell_session_close(session_id)
```

### Pattern 3: Interactive Wizards

For tools that require multiple prompts:

```
User: "Initialize a new npm project on the server"

Claude Code:
1. shell_session_create(mode="ssh", host="dev.example.com")
2. shell_exec("mkdir myproject && cd myproject && npm init")
   -> Returns awaiting_input (package name)
3. shell_provide_input("my-awesome-project")
   -> Returns awaiting_input (version)
4. shell_provide_input("1.0.0")
   -> Returns awaiting_input (description)
5. shell_provide_input("An awesome project")
   ... continue for remaining prompts
```

### Pattern 4: Log Analysis

For tailing and analyzing logs:

```
User: "Check the last 100 lines of error logs on production"

Claude Code:
1. shell_session_create(mode="ssh", host="prod.example.com")
2. shell_exec("tail -100 /var/log/app/error.log")
3. Analyze output and summarize for user
4. shell_session_close(session_id)
```

## Best Practices

### 1. Use Appropriate Timeouts

Different commands require different timeout values:

| Operation | Recommended Timeout |
|-----------|---------------------|
| Simple commands (ls, pwd) | 5000ms (default) |
| Git operations | 60000ms |
| Package installation (npm/apt) | 180000ms |
| Build processes | 300000ms |
| Database migrations | 600000ms |

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "npm install",
    "timeout_ms": 180000
  }
}
```

### 2. Cache Sudo Passwords

When running multiple sudo commands, cache the password:

```json
{
  "tool": "shell_provide_input",
  "arguments": {
    "session_id": "sess_abc123",
    "input": "password",
    "cache_for_sudo": true
  }
}
```

The password will be automatically provided for subsequent sudo prompts within the TTL (default: 5 minutes).

### 3. Check Session Status

For long-running operations, verify connection health:

```json
{
  "tool": "shell_session_status",
  "arguments": {
    "session_id": "sess_abc123"
  }
}
```

Returns:
```json
{
  "session_id": "sess_abc123",
  "state": "idle",
  "connected": true,
  "cwd": "/var/www/app",
  "env": {
    "NODE_ENV": "production"
  }
}
```

### 4. Handle Editor Detection

When a command opens an editor (vim, nano), the session returns with `prompt_type: "editor"`:

```json
{
  "status": "awaiting_input",
  "prompt_type": "editor",
  "hint": "Detected vim editor. Use :q! to exit without saving, :wq to save and exit"
}
```

For git operations that open editors, consider using non-interactive alternatives:
- `git commit -m "message"` instead of `git commit`
- `GIT_EDITOR=cat git rebase -i` for viewing only
- `EDITOR=true git commit --amend --no-edit`

### 5. Use Environment Variables

Set environment variables for the session:

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "export NODE_ENV=production && npm start"
  }
}
```

Environment variables persist across commands in the same session.

### 6. Handle Connection Drops

If a session disconnects, create a new one. The server automatically attempts reconnection with state restoration:

```json
{
  "error": "connection_lost",
  "message": "SSH connection dropped, attempting reconnection..."
}
```

## Troubleshooting

### Session Not Found

```json
{
  "error": "session_not_found",
  "message": "Session sess_abc123 does not exist"
}
```

**Solution:** Create a new session. Sessions are ephemeral and don't persist across claude-shell-mcp restarts.

### Permission Denied

```json
{
  "error": "permission_denied",
  "message": "Cannot execute command: blocked by policy"
}
```

**Solution:** Check the `command_blocklist` in your configuration.

### Authentication Failed

```json
{
  "error": "auth_failed",
  "message": "SSH authentication failed for user@host"
}
```

**Solutions:**
1. Verify SSH key path and permissions
2. Check if SSH agent is running
3. Verify host key in known_hosts

### Timeout Exceeded

```json
{
  "error": "timeout",
  "message": "Command execution exceeded 5000ms timeout"
}
```

**Solution:** Increase `timeout_ms` for long-running commands.

## Security Considerations

### 1. Never Log Credentials

The server sanitizes logs by default. Ensure `logging.sanitize: true` is set.

### 2. Use SSH Keys

Prefer SSH key authentication over passwords:

```yaml
servers:
  - name: production
    host: prod.example.com
    user: deploy
    key_path: ~/.ssh/prod_ed25519
```

### 3. Limit Command Access

Use blocklists for dangerous commands:

```yaml
security:
  command_blocklist:
    - "rm -rf /"
    - ":(){ :|:& };:"
    - "> /dev/sda"
```

### 4. Session Limits

Prevent resource exhaustion:

```yaml
security:
  max_sessions_per_user: 10
  idle_timeout: 30m
```

## Session Recording

Sessions can be recorded for audit and debugging:

```yaml
recording:
  enabled: true
  path: ~/.local/share/claude-shell-mcp/recordings
```

Recordings are stored in asciicast v2 format and can be played back with:

```bash
asciinema play ~/.local/share/claude-shell-mcp/recordings/sess_abc123.cast
```

## Example: Full Deployment Workflow

Here's a complete example of deploying an application:

```
User: "Deploy version 2.1.0 to production"

Claude Code executes:

1. Create session
   shell_session_create(mode="ssh", host="prod.example.com", user="deploy")
   -> session_id: "sess_deploy_001"

2. Navigate to app directory
   shell_exec("cd /var/www/myapp")
   -> status: "completed", output: ""

3. Check current version
   shell_exec("git describe --tags")
   -> status: "completed", output: "v2.0.3"

4. Fetch latest tags
   shell_exec("git fetch --tags")
   -> status: "completed"

5. Checkout new version
   shell_exec("git checkout v2.1.0")
   -> status: "completed", output: "HEAD is now at abc1234 v2.1.0"

6. Install dependencies
   shell_exec("npm ci", timeout_ms=180000)
   -> status: "completed"

7. Run database migrations
   shell_exec("npm run migrate", timeout_ms=120000)
   -> status: "completed", output: "Migrations complete"

8. Restart application
   shell_exec("sudo systemctl restart myapp")
   -> status: "awaiting_input", prompt_type: "password"

9. Provide sudo password
   shell_provide_input(input="***", cache_for_sudo=true)
   -> status: "completed"

10. Verify health
    shell_exec("curl -s localhost:3000/health | jq .")
    -> status: "completed", output: '{"status":"healthy","version":"2.1.0"}'

11. Check service status
    shell_exec("sudo systemctl status myapp --no-pager")
    -> status: "completed" (sudo password auto-injected from cache)

12. Close session
    shell_session_close("sess_deploy_001")

Report to user: "Successfully deployed v2.1.0 to production. Health check passed."
```

## Additional Resources

- [REFERENCE.md](../REFERENCE.md) - Full MCP protocol specification
- [ROADMAP.md](../ROADMAP.md) - Project implementation roadmap
- [Example Workflows](../examples/) - More workflow examples
