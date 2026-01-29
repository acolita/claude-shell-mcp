# Claude Remote Shell MCP (claude-shell-mcp)

**Version:** 0.1.0-alpha  
**Protocol:** Model Context Protocol (MCP) 2025-03-26  
**Transport:** stdio (primary), SSE (optional)

## Vision

A Model Context Protocol server that provides persistent, interactive shell sessions over SSH with full transparency for LLM agents. Unlike traditional SSH command execution, this tool enables Claude to operate remote terminals as if they were local—handling sudo authentication, environment persistence, and interactive CLI wizards through an explicit "interrupt & resume" pattern.

## Core Principles

1. **Transparency First**: The LLM sees every terminal state change and makes explicit decisions on inputs
2. **Session Persistence**: SSH connections and shell states (env vars, cwd, sudo privileges) survive across multiple Claude turns
3. **Secure by Design**: Credentials are never logged; sudo passwords use one-time caching with automatic expiration
4. **Agent Autonomy**: The LLM decides what to type, when to wait, and when to cancel—not hidden heuristics

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Claude Desktop/Code                      │
│                                                                  │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  User Prompt: "Deploy the app to production"              │  │
│  │                                                           │  │
│  │  Turn 1: tools/shell_exec(command="ssh prod-server")      │  │
│  │  Turn 2: tools/shell_provide_input(input="cd /app")       │  │
│  │  Turn 3: tools/shell_exec(command="git pull")             │  │
│  │  Turn 4: tools/shell_exec(command="sudo systemctl...")    │  │
│  │          → Returns: {status: "awaiting_input", prompt:    │  │
│  │             "[sudo] password:"}                           │  │
│  │  Turn 5: tools/shell_provide_input(input="***")           │  │
│  │                                                           │  │
│  └───────────────────────────────────────────────────────────┘  │
│                           │                                      │
│                           │ stdio                                │
│                           ▼                                      │
│              ┌──────────────────────┐                            │
│              │   claude-shell-mcp   │                            │
│              │   (Go binary)        │                            │
│              └──────────────────────┘                            │
│                           │                                      │
│                           │ crypto/ssh                           │
│                           ▼                                      │
│              ┌──────────────────────┐                            │
│              │   Remote Server      │                            │
│              │   ┌──────────────┐   │                            │
│              │   │ PTY Session  │   │  <-- Persistent bash/zsh   │
│              │   │ with env vars│   │                            │
│              │   │ sudo cached  │   │                            │
│              │   └──────────────┘   │                            │
│              └──────────────────────┘                            │
└─────────────────────────────────────────────────────────────────┘
```

## Go Implementation Stack

- **SSH Client**: `golang.org/x/crypto/ssh` with `RequestPty` support
- **PTY Management**: `github.com/creack/pty` (local fallback) + custom SSH PTY wrapper
- **Pattern Matching**: `regexp` with prompt heuristics
- **State Management**: In-memory `sync.Map` of sessions (extensible to Redis/etcd)
- **Credential Cache**: `github.com/zalando/go-keyring` (optional) or encrypted memory buffer
- **MCP Protocol**: `github.com/mark3labs/mcp-go`

## MCP Tools Specification

### Tool: `shell_session_create`

Initialize a persistent SSH session.

**Input:**
```json
{
  "host": "prod-web-01.internal",
  "port": 22,
  "user": "ubuntu",
  "auth": {
    "type": "key", 
    "path": "~/.ssh/id_rsa",
    "passphrase_env": "SSH_KEY_PASSPHRASE"
  },
  "pty": {
    "term": "xterm-256color",
    "width": 80,
    "height": 24
  },
  "auto_sudo": false,
  "idle_timeout_minutes": 30
}
```

**Output:**
```json
{
  "session_id": "sess_abc123xyz",
  "status": "connected",
  "shell": "/bin/bash",
  "initial_cwd": "/home/ubuntu",
  "sudo_cached": false
}
```

### Tool: `shell_exec`

Execute a command with interactive prompt detection.

**Input:**
```json
{
  "session_id": "sess_abc123xyz",
  "command": "sudo apt update",
  "timeout_ms": 5000,
  "prompt_detection": {
    "enabled": true,
    "patterns": ["password.*:", "\\\\[Y/n\\\\]", "continue\\\\?"],
    "auto_respond_simple": false
  }
}
```

**Output - Success:**
```json
{
  "status": "completed",
  "exit_code": 0,
  "stdout": "Hit:1 http://archive.ubuntu.com...",
  "stderr": "",
  "final_cwd": "/home/ubuntu",
  "env_snapshot": {
    "PATH": "/usr/local/sbin:/usr/local/bin...",
    "DEBIAN_FRONTEND": "noninteractive"
  }
}
```

**Output - Awaiting Input (The Key Pattern):**
```json
{
  "status": "awaiting_input",
  "session_id": "sess_abc123xyz",
  "prompt_type": "password",
  "prompt_text": "[sudo] password for ubuntu:",
  "context_buffer": "Reading package lists... Done\nBuilding dependency tree... \n[sudo] password for ubuntu:",
  "options": {
    "can_provide_text": true,
    "can_send_ctrl_c": true,
    "can_send_enter": true,
    "mask_input": true
  },
  "hint": "Sudo authentication required. Provide password or cancel."
}
```

### Tool: `shell_provide_input`

Resume a paused session with LLM-provided input.

**Input:**
```json
{
  "session_id": "sess_abc123xyz",
  "input": "mySecretPassword123",
  "input_type": "text",
  "cache_for_sudo": true,
  "expect_subsequent_prompt": false
}
```

**Output:**
```json
{
  "status": "completed", // or "awaiting_input" if another prompt appears
  "session_id": "sess_abc123xyz",
  "stdout": "0 upgraded, 0 newly installed...",
  "sudo_authenticated": true,
  "auth_valid_minutes": 5
}
```

### Tool: `shell_interrupt`

Send SIGINT (Ctrl+C) to break out of hanging processes.

**Input:**
```json
{
  "session_id": "sess_abc123xyz",
  "signal": "SIGINT"
}
```

### Tool: `shell_session_status`

Check session health, current directory, and environment.

**Output:**
```json
{
  "session_id": "sess_abc123xyz",
  "connected": true,
  "idle_time_seconds": 45,
  "current_cwd": "/home/ubuntu/app",
  "sudo_active": true,
  "sudo_expires_in_seconds": 240,
  "env_vars": {"NODE_ENV": "production"}
}
```

### Tool: `shell_session_close`

Graceful cleanup.

## The "Interrupt & Resume" Protocol

This is the core innovation. When an interactive prompt is detected:

1. **Server halts** reading from PTY (pauses the goroutine)
2. **Server returns** `status: "awaiting_input"` with full context
3. **Claude analyzes** the prompt text and conversation history
4. **Claude decides** what to do (provide password, say "yes", cancel, etc.)
5. **Claude calls** `shell_provide_input` explicitly
6. **Server resumes** PTY I/O with the provided input

**Benefits:**
- Full audit trail of decisions
- LLM uses context (e.g., "The user mentioned the password is in 1Password")
- No hidden sampling calls
- User can see exactly what the terminal is asking

## Security Model

### Credential Handling

```go
// Secure password cache (in-memory only)
type SecureCache struct {
    data      []byte
    createdAt time.Time
    ttl       time.Duration
    once      sync.Once
}

func (sc *SecureCache) Get() ([]byte, error) {
    if time.Since(sc.createdAt) > sc.ttl {
        sc.Clear()
        return nil, errors.New("expired")
    }
    return sc.data, nil
}

func (sc *SecureCache) Clear() {
    sc.once.Do(func() {
        // Cryptographic wipe
        for i := range sc.data {
            sc.data[i] = 0
        }
        sc.data = nil
    })
}
```

### Sudo Escalation Flow

1. **First sudo**: Returns `awaiting_input` to Claude
2. **Claude obtains** password (from user or secure store)
3. **Claude sends** via `shell_provide_input` with `cache_for_sudo: true`
4. **Server stores** in SecureCache (5 min TTL)
5. **Subsequent sudo**: Server auto-injects from cache, returns `sudo_authenticated: true` in response
6. **Cache expires**: Next sudo returns `awaiting_input` again

### Session Isolation

- Each session is a separate SSH connection
- Environment variables are sandboxed per session
- No cross-session credential leakage
- Automatic cleanup of zombie sessions after `idle_timeout`

## Prompt Detection Patterns

Built-in regex patterns for common interactive prompts:

```go
var DefaultPromptPatterns = []PromptPattern{
    {
        Name: "sudo_password",
        Regex: regexp.MustCompile(`(?i)\[sudo\]\s+password\s+for\s+\w+:\s*$`),
        Type: "password",
        MaskInput: true,
    },
    {
        Name: "ssh_host_key",
        Regex: regexp.MustCompile(`(?i)are you sure you want to continue connecting \(yes/no`),
        Type: "confirmation",
        SuggestedResponse: "yes",
    },
    {
        Name: "apt_confirmation",
        Regex: regexp.MustCompile(`(?i)do you want to continue\? \[Y/n\]`),
        Type: "confirmation",
        SuggestedResponse: "Y",
    },
    {
        Name: "npm_init_package_name",
        Regex: regexp.MustCompile(`(?i)package name:\s*\([^)]*\)\s*$`),
        Type: "text",
    },
}
```

## Error Handling

### Recoverable States

- `connection_lost`: Auto-reconnect with 3 retries
- `sudo_expired`: Re-prompt for password
- `command_not_found`: Suggest package installation

### Fatal Errors

- `auth_failed`: Invalid SSH key/password
- `permission_denied`: File system permissions
- `session_not_found`: Invalid session_id

## Configuration

```yaml
# config.yaml
servers:
  - name: production
    host: prod.internal
    user: deploy
    key_path: ~/.ssh/prod_ed25519
    
security:
  sudo_cache_ttl: 5m
  idle_timeout: 30m
  max_sessions_per_user: 10
  
logging:
  level: info
  # NEVER log input when mask_input is true
  sanitize: true
  
prompt_detection:
  custom_patterns:
    - name: "vault_password"
      regex: "Vault password:"
      type: "password"
      mask_input: true
```

## Development Mode

Local testing without SSH:

```go
// Local PTY mode for testing
if cfg.Mode == "local" {
    pty, _ := pty.Start(exec.Command("/bin/bash"))
    session := &Session{pty: pty}
}
```

## MCP Capabilities

Required capabilities:
- `tools`: For all tool definitions
- `logging`: For verbose PTY output debugging

Optional capabilities:
- `resources`: Expose session logs as resources
- `prompts`: Pre-defined prompts for common workflows (e.g., "Deploy to production")

## Integration with Claude Code

Claude Code has specific limitations this tool solves:

1. **Environment Persistence**: `export FOO=bar` in Turn 1 → `echo $FOO` in Turn 2 returns "bar"
2. **Working Directory**: `cd /etc` persists across calls
3. **Sudo Context**: Authenticated sudo survives multiple tool calls
4. **Raw Mode**: Interactive programs (vim, nano) work via the interrupt/resume pattern

Recommended Claude Code configuration:

```json
// .claude/settings.json
{
  "mcpServers": {
    "remote-shell": {
      "command": "claude-shell-mcp",
      "args": ["--config", "/path/to/config.yaml"],
      "env": {
        "SSH_KEY_PASSPHRASE": "op://Private/prod-ssh/password"
      }
    }
  }
}
```

## Testing Strategy

1. **Unit Tests**: Pattern matching regexes, SecureCache lifecycle
2. **Integration Tests**: Local PTY sessions with expect scripts
3. **E2E Tests**: Docker-based SSH servers testing full workflows:
   - Git operations with SSH keys
   - sudo apt update/install
   - npm init interactive wizard
   - Docker-compose up with interactive prompts

## Future Extensions

- **File Transfer**: Integrated SFTP for `file_read`/`file_write` without separate SCP
- **Port Forwarding**: Dynamic SSH tunnels exposed as local HTTP resources
- **Multi-hop**: SSH jump host support (bastion → internal)
- **Session Recording**: ASCIICast output for auditing
- **Collaboration**: Share session ID with other Claude instances (read-only or read-write)

## License

MIT - See LICENSE file
