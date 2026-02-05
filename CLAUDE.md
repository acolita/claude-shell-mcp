# CLAUDE.md - Project Intelligence for claude-shell-mcp

## Project Overview

**claude-shell-mcp** is a Model Context Protocol (MCP) server written in Go that provides persistent, interactive shell sessions over SSH. It enables LLM agents to operate remote terminals with full transparency, handling sudo authentication, environment persistence, and interactive CLI prompts through an explicit "interrupt & resume" pattern.

**Version:** 1.0.0
**Protocol:** MCP 2025-03-26
**Transport:** stdio (primary), SSE (optional)

## Core Principles

1. **Transparency First** - The LLM sees every terminal state change and makes explicit decisions
2. **Session Persistence** - SSH connections and shell state survive across multiple tool calls
3. **Secure by Design** - Credentials never logged; sudo passwords use one-time caching with automatic expiration
4. **Agent Autonomy** - The LLM decides what to type, when to wait, and when to cancel

## Agent Workflow Rules (MANDATORY)

These rules MUST be followed for all work on this project:

### Task Decomposition
- **ALWAYS** break user requests into smaller, manageable steps before starting implementation
- Add all steps to the tasklist using `TaskCreate` BEFORE writing any code
- Each task should be atomic and independently completable
- Order tasks by dependency (use `addBlockedBy` when tasks depend on others)

### Error Handling Workflow
- When encountering ANY error (build failure, test failure, runtime error, etc.):
  1. **STOP** current work immediately
  2. **CREATE** a new task describing the error and required fix using `TaskCreate`
  3. **THEN** proceed to solve it by marking the task as `in_progress`
  4. **MARK** the task as `completed` only after the error is fully resolved
- Never attempt to fix errors without first documenting them as tasks

### Autonomous Backlog Processing
- **ALWAYS** check `TaskList` for pending tasks after completing any work
- **MUST** continue implementing pending tasks from the backlog autonomously
- **DO NOT** ask the user for permission to continue with pending tasks
- **DO NOT** wait for user interaction between tasks
- Work through the entire backlog until all tasks are `completed`
- Only stop for user input when:
  - A task explicitly requires user decision/credentials
  - All pending tasks are completed
  - A critical blocker prevents further progress

### Task Lifecycle
```
1. User request received
2. Decompose into tasks → TaskCreate (multiple)
3. Set dependencies → TaskUpdate (addBlockedBy)
4. Pick first unblocked task → TaskUpdate (status: in_progress)
5. Implement the task
6. If error found → TaskCreate (error fix task) → solve it
7. Mark completed → TaskUpdate (status: completed)
8. Check TaskList → repeat from step 4 until backlog empty
```

## Tech Stack

- **Language:** Go 1.22+
- **SSH Client:** `golang.org/x/crypto/ssh` with `RequestPty` support
- **PTY Management:** `github.com/creack/pty` (local) + custom SSH PTY wrapper
- **MCP Protocol:** `github.com/mark3labs/mcp-go`
- **Credential Cache:** `github.com/zalando/go-keyring` (optional) or encrypted memory buffer
- **Pattern Matching:** `regexp` with prompt heuristics

## Architecture

```
Claude Desktop/Code
        │
        │ stdio (MCP protocol)
        ▼
┌──────────────────┐
│ claude-shell-mcp │  ← Go binary
│   (MCP Server)   │
└──────────────────┘
        │
        │ crypto/ssh
        ▼
┌──────────────────┐
│  Remote Server   │
│  ┌────────────┐  │
│  │ PTY Session│  │  ← Persistent bash/zsh with env vars, sudo cached
│  └────────────┘  │
└──────────────────┘
```

## Directory Structure (Target)

```
claude-shell-mcp/
├── cmd/
│   └── claude-shell-mcp/    # Main binary entrypoint
│       └── main.go
├── internal/
│   ├── mcp/                 # MCP protocol handling
│   │   ├── server.go        # MCP server implementation
│   │   └── tools.go         # Tool definitions and handlers
│   ├── session/             # Session management
│   │   ├── manager.go       # Session lifecycle
│   │   ├── session.go       # Individual session struct
│   │   └── state.go         # State machine (running/awaiting_input/completed)
│   ├── ssh/                 # SSH client wrapper
│   │   ├── client.go        # Connection pooling
│   │   ├── pty.go           # PTY allocation over SSH
│   │   └── auth.go          # Key-based authentication
│   ├── pty/                 # Local PTY support
│   │   └── local.go         # creack/pty wrapper
│   ├── prompt/              # Interactive prompt detection
│   │   ├── detector.go      # Pattern matching engine
│   │   └── patterns.go      # Default regex patterns
│   ├── security/            # Credential handling
│   │   ├── cache.go         # SecureCache with TTL
│   │   └── wipe.go          # Cryptographic memory clearing
│   └── config/              # Configuration parsing
│       └── config.go
├── pkg/                     # Public APIs (if any)
├── test/
│   ├── integration/         # Integration tests with expect scripts
│   └── e2e/                 # Docker-based E2E tests
├── config.example.yaml      # Example configuration
├── CLAUDE.md                # This file
├── REFERENCE.md             # Full specification document
├── ROADMAP.md               # Implementation roadmap
├── go.mod
├── go.sum
└── Makefile
```

## MCP Tools

### Shell Session Tools
| Tool | Purpose |
|------|---------|
| `shell_session_create` | Initialize a persistent SSH/local session |
| `shell_exec` | Execute command with interactive prompt detection |
| `shell_provide_input` | Resume paused session with input (password, confirmation, etc.) |
| `shell_interrupt` | Send SIGINT (Ctrl+C) to break hanging processes |
| `shell_session_status` | Check session health, cwd, environment |
| `shell_session_close` | Graceful session cleanup |

### File Transfer Tools (SCP/SFTP)
| Tool | Purpose |
|------|---------|
| `shell_file_get` | Download a file from remote session (returns content or saves locally) |
| `shell_file_put` | Upload a file to remote session (from content or local file) |
| `shell_file_mv` | Move or rename a file in a session |
| `shell_dir_get` | Download a directory recursively with glob pattern support |
| `shell_dir_put` | Upload a directory recursively with glob pattern support |

### Chunked Transfer Tools (Large Files)
| Tool | Purpose |
|------|---------|
| `shell_file_get_chunked` | Download large files in chunks with resume support |
| `shell_file_put_chunked` | Upload large files in chunks with resume support |
| `shell_transfer_status` | Check progress of a chunked transfer |
| `shell_transfer_resume` | Resume an interrupted chunked transfer |

### SSH Tunnel Tools
| Tool | Purpose |
|------|---------|
| `shell_tunnel_create` | Create an SSH tunnel (local -L or reverse -R port forward) |
| `shell_tunnel_list` | List active tunnels for a session with connection stats |
| `shell_tunnel_close` | Close a specific tunnel |

#### File Transfer Features
- **Checksum verification**: SHA256 checksum calculation and verification
- **Atomic writes**: Temp file + rename to prevent partial files
- **Timestamp preservation**: Maintain file modification times
- **Glob patterns**: Filter files with patterns like `**/*.go`, `*.log`
- **Exclusion patterns**: Skip `.git`, `node_modules`, `__pycache__`, etc.
- **Symlink handling**: Follow, preserve, or skip symbolic links
- **Binary support**: Base64 encoding for binary files
- **Chunked transfers**: Resume capability for large files with per-chunk checksums
- **Progress tracking**: Transfer rate and duration metrics

## The Interrupt & Resume Pattern

This is the core innovation. When an interactive prompt is detected:

1. Server halts reading from PTY
2. Server returns `status: "awaiting_input"` with full context
3. Claude analyzes the prompt and conversation history
4. Claude decides what to do (provide password, say "yes", cancel)
5. Claude calls `shell_provide_input` explicitly
6. Server resumes PTY I/O with the provided input

**Key benefit:** Full audit trail, no hidden sampling calls.

## Build & Run

```bash
# Build
go build -o claude-shell-mcp ./cmd/claude-shell-mcp

# Run with config
./claude-shell-mcp --config config.yaml

# Run in local PTY mode (no SSH, for testing)
./claude-shell-mcp --mode local

# Version check
./claude-shell-mcp --version
```

## Testing

```bash
# Unit tests
go test ./...

# Integration tests (requires local PTY)
go test -tags=integration ./test/integration/...

# E2E tests (requires Docker)
docker-compose -f test/e2e/docker-compose.yml up -d
go test -tags=e2e ./test/e2e/...
```

## Code Conventions

### Go Style
- Follow standard Go conventions (`gofmt`, `golint`)
- Use `slog` for structured JSON logging
- Context with timeout on all I/O operations: `context.WithTimeout`
- Error wrapping with `fmt.Errorf("operation: %w", err)`

### Naming
- Sessions: `sess_<random>` (e.g., `sess_abc123xyz`)
- Internal packages: `internal/`
- No CGO where possible (for static compilation)

### Security Rules
- **NEVER** log passwords, secrets, or credential data
- Use `mask_input: true` for password prompts
- Cryptographically wipe sensitive memory after use
- TTL-based expiration for all cached credentials

### Commit Messages
Use conventional commits:
```
feat: add shell_interrupt tool
fix: prevent credential leak in debug logs
docs: update CLAUDE.md with new patterns
test: add integration test for sudo flow
refactor: extract prompt detection to separate package
```

## Key Patterns

### Session State Machine
```
┌─────────┐    exec()     ┌─────────┐
│ IDLE    │ ─────────────▶│ RUNNING │
└─────────┘               └─────────┘
     ▲                         │
     │                         │ prompt detected
     │                         ▼
     │                   ┌──────────────┐
     │ completed         │ AWAITING_    │
     └───────────────────│ INPUT        │
                         └──────────────┘
                               │
                               │ provide_input()
                               ▼
                         ┌─────────┐
                         │ RUNNING │ (continues)
                         └─────────┘
```

### SecureCache Pattern
```go
type SecureCache struct {
    data      []byte
    createdAt time.Time
    ttl       time.Duration
}

// Always clear on expiration or explicit wipe
func (sc *SecureCache) Clear() {
    for i := range sc.data {
        sc.data[i] = 0  // Cryptographic wipe
    }
    sc.data = nil
}
```

### Default Prompt Patterns
```go
var DefaultPromptPatterns = []PromptPattern{
    {Name: "sudo_password", Regex: `(?i)\[sudo\]\s+password\s+for\s+\w+:\s*$`, Type: "password"},
    {Name: "ssh_host_key", Regex: `(?i)are you sure you want to continue connecting`, Type: "confirmation"},
    {Name: "apt_confirmation", Regex: `(?i)do you want to continue\? \[Y/n\]`, Type: "confirmation"},
}
```

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
  sanitize: true  # NEVER log masked input

prompt_detection:
  custom_patterns:
    - name: "vault_password"
      regex: "Vault password:"
      type: "password"
      mask_input: true
```

## Claude Desktop/Code Integration

```json
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

## Common Workflows

### Deploy to Production
```
1. shell_session_create(host="prod-server")
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

### Download Log File for Analysis
```
1. shell_session_create(host="prod-server")
2. shell_file_get(session_id="s1", remote_path="/var/log/app.log")
   → Returns file content, size, checksum
3. shell_session_close()
```

### Upload Config File
```
1. shell_session_create(host="prod-server")
2. shell_file_put(
     session_id="s1",
     remote_path="/etc/app/config.yaml",
     content="key: value\n...",
     create_dirs=true,
     atomic=true
   )
   → Returns status, checksum, atomic_write: true
3. shell_session_close()
```

### Sync Source Code Directory
```
1. shell_session_create(host="dev-server")
2. shell_dir_put(
     session_id="s1",
     local_path="./src",
     remote_path="/app/src",
     pattern="**/*.go",       # Only .go files
     preserve=true,           # Keep timestamps
     overwrite=true           # Replace existing
   )
   → Returns files_transferred, total_bytes, errors
3. shell_session_close()
```

### Backup Remote Directory
```
1. shell_session_create(host="prod-server")
2. shell_dir_get(
     session_id="s1",
     remote_path="/app/data",
     local_path="./backup/data",
     preserve=true,
     symlinks="skip"          # Don't follow symlinks
   )
   → Returns files_transferred, dirs_created, total_bytes
3. shell_session_close()
```

## Error Handling

### Recoverable
- `connection_lost` - Auto-reconnect with 3 retries
- `sudo_expired` - Re-prompt for password
- `command_not_found` - Suggest package installation

### Fatal
- `auth_failed` - Invalid SSH key/password
- `permission_denied` - File system permissions
- `session_not_found` - Invalid session_id

## Performance Targets

- Binary size: < 20MB (static compilation)
- Memory per session: < 10MB
- Concurrent sessions: 50+ (stress tested to 100+)
- Reconnection: < 3 seconds with state recovery

## Development Resources

- **REFERENCE.md** - Full specification with tool schemas and protocol details
- **ROADMAP.md** - 14-week implementation plan with milestones
- **mcp-go docs** - https://github.com/mark3labs/mcp-go

## Testing Scenarios

Always validate against these real-world scenarios:

1. **Basic:** `ls -la`, `cd`, `pwd` (state persistence)
2. **Moderate:** `git clone`, `npm install` (environment handling)
3. **Complex:** `sudo apt update && sudo apt upgrade` (sudo caching)
4. **Wizard:** `npm init @latest` (interactive prompt handling)

## Quick Reference

```bash
# Check if MCP handshake works
echo '{"jsonrpc":"2.0","method":"initialize","params":{},"id":1}' | ./claude-shell-mcp

# Run linter
golangci-lint run

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```
