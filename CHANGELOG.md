# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.0] - 2026-01-30

### Fixed
- Password input timing: simplified from stty polling to fixed 100ms delay (per pexpect's proven approach)
- Unified line ending for all input to `\n` (removed special `\r` handling for passwords)
- Fixed prompt re-detection after providing input by clearing output buffer
- Password prompts now complete in a single `shell_provide_input` call

### Changed
- `waitForEchoDisabled()` now uses simple fixed delay instead of complex stty polling
- `ProvideInput()` now resets output buffer and pending prompt state before reading

## [1.0.0] - 2026-01-29

### Added

#### Core Features
- MCP server with stdio transport for Claude Desktop/Code integration
- Persistent local PTY sessions using `creack/pty`
- SSH session support with key-based and password authentication
- SSH agent forwarding support
- Host key verification via known_hosts

#### Session Management
- Session lifecycle management (create, exec, status, close)
- Environment variable capture and persistence
- Working directory tracking across commands
- Shell alias and function detection
- State restoration on SSH reconnect
- Session multiplexing (multiple sessions per instance)
- Heartbeat/keepalive to prevent connection drops
- Idle timeout detection and cleanup

#### Interactive Prompt Handling
- Automatic prompt detection with regex patterns
- Default patterns for: sudo, SSH confirmations, apt, yum, npm, git, Docker
- Password, confirmation, text, editor, and pager prompt types
- `awaiting_input` status with full context for LLM decision
- `shell_provide_input` tool for resuming paused sessions
- `shell_interrupt` tool for sending Ctrl+C
- Vim/nano/less/more detection with suggested responses

#### Security
- Secure sudo password caching with TTL-based expiration
- Cryptographic memory wiping for sensitive data
- Command blocklist/allowlist filtering
- Rate limiting on authentication attempts with lockout
- Sanitized logging (no credentials in logs)
- In-memory encrypted credential storage

#### Developer Experience
- Configuration file hot-reload with fsnotify
- Verbose debug mode with PTY output logging
- Asciicast v2 session recording for audit and debugging
- Docker image for easy deployment
- Cross-compilation for Linux, macOS, Windows (amd64/arm64)

#### Shell Compatibility
- Bash support (primary)
- Zsh support
- Fish shell support (basic)
- Shell detection and adaptive prompt commands
- Custom shell initialization control (.bashrc sourcing)

#### Documentation
- Comprehensive CLAUDE.md with project guidelines
- REFERENCE.md with full MCP protocol specification
- Claude Code integration guide
- Example workflows: deployment, debugging, server setup

### Performance
- Binary size: ~6.5MB (target < 20MB)
- Memory per session: < 10MB
- Concurrent sessions: stress tested to 100+
- Connection pooling for SSH sessions

### MCP Tools

| Tool | Description |
|------|-------------|
| `shell_session_create` | Create a new local or SSH shell session |
| `shell_exec` | Execute command with interactive prompt detection |
| `shell_provide_input` | Resume paused session with input |
| `shell_interrupt` | Send SIGINT (Ctrl+C) to running command |
| `shell_session_status` | Get session health, cwd, environment |
| `shell_session_close` | Graceful session cleanup |

## [0.1.0-alpha] - 2026-01-15

### Added
- Initial project scaffold
- Basic MCP protocol handler using mcp-go
- Local PTY support with creack/pty
- Session management (in-memory store)
- Basic SSH client with crypto/ssh
- Prompt detection framework
- Configuration parsing (YAML/JSON)
- Logging infrastructure with slog

---

[1.1.0]: https://github.com/acolita/claude-shell-mcp/releases/tag/v1.1.0
[1.0.0]: https://github.com/acolita/claude-shell-mcp/releases/tag/v1.0.0
[0.1.0-alpha]: https://github.com/acolita/claude-shell-mcp/releases/tag/v0.1.0-alpha
