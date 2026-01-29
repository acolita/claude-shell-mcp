# Claude Shell MCP - Implementation Roadmap

## Phase 0: Foundation (Week 1-2)
**Goal:** Working stdio MCP server with local PTY support

### Week 1: Project Scaffold
- [ ] Initialize Go module (`github.com/user/claude-shell-mcp`)
- [ ] Set up CI/CD (GitHub Actions: lint, test, build)
- [ ] Implement MCP protocol handler using `mcp-go`
- [ ] Basic logging infrastructure (slog with JSON)
- [ ] Configuration parsing (YAML/JSON)

**Deliverable:** `claude-shell-mcp --version` runs and responds to `initialize` request

### Week 2: Local PTY Core
- [ ] Integrate `creack/pty` for local shell spawning
- [ ] Implement session management (in-memory store)
- [ ] Tool: `shell_session_create` (local mode)
- [ ] Tool: `shell_exec` with timeout
- [ ] Tool: `shell_session_close`
- [ ] Basic output capture (stdout/stderr separation)

**Deliverable:** Can execute `echo "hello"` and return output via MCP

---

## Phase 1: SSH Backbone (Week 3-4)
**Goal:** Remote SSH connections with session persistence

### Week 3: SSH Client
- [ ] SSH connection pooling using `crypto/ssh`
- [ ] Key-based authentication (RSA, Ed25519)
- [ ] SSH agent forwarding support
- [ ] Host key verification (known_hosts)
- [ ] PTY allocation over SSH channel (`RequestPty`)

**Deliverable:** Connect to remote server and execute `uname -a`

### Week 4: Session Lifecycle
- [ ] Persistent SSH sessions across MCP tool calls
- [ ] Heartbeat/keepalive (prevent connection drop)
- [ ] Automatic reconnection with state recovery
- [ ] Idle timeout detection and cleanup
- [ ] Session multiplexing (multiple sessions per MCP instance)

**Deliverable:** 
```bash
# Turn 1
shell_exec(session_id="s1", command="cd /tmp")
# Turn 2  
shell_exec(session_id="s1", command="pwd") # Returns /tmp
```

---

## Phase 2: The Interrupt Pattern (Week 5-6)
**Goal:** Interactive prompt detection and the "awaiting_input" protocol

### Week 5: Prompt Detection Engine
- [ ] Pattern matching system (regex registry)
- [ ] Default patterns: sudo, SSH confirmations, apt
- [ ] Configurable custom patterns
- [ ] PTY output buffering (last N lines)
- [ ] Non-blocking I/O with `select`-style multiplexing

**Deliverable:** Detects `[sudo] password:` and pauses execution

### Week 6: Interrupt & Resume Tools
- [ ] `shell_exec` returns `status: "awaiting_input"`
- [ ] `shell_provide_input` tool implementation
- [ ] Input injection into PTY (with proper echo handling)
- [ ] `shell_interrupt` (Ctrl+C sending)
- [ ] Multi-turn conversation state machine

**Deliverable:** Full flow:
```
exec("sudo whoami") → awaiting_input → provide_input("pass") → completed (returns "root")
```

---

## Phase 3: Security & Credentials (Week 7-8)
**Goal:** Production-ready credential handling

### Week 7: Secure Cache
- [ ] In-memory encrypted credential cache
- [ ] Automatic cryptographic wipe after TTL
- [ ] Sudo credential caching (5-min default)
- [ ] Passphrase-protected SSH key support
- [ ] Integration with OS keyring (optional)

**Deliverable:** Sudo password cached securely, auto-injected for subsequent commands

### Week 8: Audit & Safety
- [ ] Comprehensive audit logging (no secrets)
- [ ] Command allowlist/blocklist (optional)
- [ ] Session recording (asciicast format)
- [ ] Max session limits per user
- [ ] Rate limiting on authentication attempts

**Deliverable:** Security audit passes, no plaintext passwords in logs

---

## Phase 4: Environment & State (Week 9-10)
**Goal:** Full shell state persistence (env vars, cwd)

### Week 9: State Snapshots
- [ ] Environment variable capture (`env` parsing)
- [ ] Working directory tracking (`pwd`)
- [ ] Shell alias/function detection (bash/zsh)
- [ ] State restoration on session reconnect
- [ ] `shell_session_status` tool

**Deliverable:** 
```
Turn 1: export DB_HOST=localhost
Turn 2: echo $DB_HOST  # Returns localhost via state restoration
```

### Week 10: Shell Compatibility
- [ ] Bash support (primary)
- [ ] Zsh support
- [ ] Fish shell support (nice to have)
- [ ] Shell detection and adaptation
- [ ] Custom shell initialization (.bashrc sourcing control)

**Deliverable:** Works on Ubuntu (bash), macOS (zsh), Alpine (ash)

---

## Phase 5: Advanced Features (Week 11-12)
**Goal:** Complex workflows and developer experience

### Week 11: Complex Interactives
- [ ] Support for `npm init`, `vue create` wizards
- [ ] Git interactive rebase handling
- [ ] Docker-compose TTY allocation
- [ ] Vim/nano detection (block or hand off)
- [ ] Expect-like scripting for known workflows

**Deliverable:** Successfully runs `npm init` answering all prompts via LLM

### Week 12: Developer Experience
- [ ] Configuration file hot-reload
- [ ] Verbose debug mode (hex PTY dumps)
- [ ] Local testing mode (mock SSH server)
- [ ] Claude Desktop integration guide
- [ ] Docker image for easy deployment

**Deliverable:** 100% test coverage on critical paths, documentation complete

---

## Phase 6: Integration & Hardening (Week 13-14)
**Goal:** Production deployment and ecosystem integration

### Week 13: Claude Code Integration
- [ ] Test with Claude Code (not just Claude Desktop)
- [ ] Work around Claude Code bash limitations
- [ ] Optimize for Claude Code's specific tool calling patterns
- [ ] Example workflows (deployment, debugging, log analysis)

**Deliverable:** End-to-end demo: "Deploy my app" across 3 servers with git pull, npm install, pm2 restart

### Week 14: Performance & Scale
- [ ] Connection pooling optimization
- [ ] Memory usage profiling (leak detection)
- [ ] Concurrent session stress testing (100+ sessions)
- [ ] Binary size optimization (strip, upx)
- [ ] Cross-compilation (Linux, macOS, Windows)

**Deliverable:** v1.0.0 release, binary < 20MB, handles 50 concurrent sessions

---

## Future Phases (Post v1.0)

### Phase 7: Ecosystem (Month 4)
- [ ] VS Code extension for session visualization
- [ ] Web dashboard for session monitoring
- [ ] Ansible/Terraform integration modules
- [ ] Support for AWS SSM, GCP IAP (beyond SSH)

### Phase 8: Intelligence (Month 5)
- [ ] LLM-based prompt classification (not just regex)
- [ ] Automatic error recovery (suggest fixes for common failures)
- [ ] Smart sudo detection (parse sudoers, predict password needs)
- [ ] Integration with 1Password/Bitwarden CLI for secrets

---

## Milestones Summary

| Phase | Date | Key Metric |
|-------|------|------------|
| Foundation | Week 2 | MCP protocol handshake working |
| SSH Backbone | Week 4 | Remote command execution |
| Interrupt Pattern | Week 6 | Interactive sudo works |
| Security | Week 8 | Security audit passed |
| Environment | Week 10 | State persistence across calls |
| Advanced | Week 12 | npm init wizard completion |
| v1.0 Release | Week 14 | Production ready |

---

## Risk Mitigation

### Technical Risks

**PTY Deadlocks**
- *Mitigation:* Strict timeouts on all I/O operations, `context.WithTimeout` everywhere
- *Fallback:* Kill session, start fresh if unresponsive for >30s

**SSH Connection Drops**
- *Mitigation:* Aggressive keepalives, automatic reconnection with exponential backoff
- *Fallback:* Resume with state restoration (cwd, env vars)

**Credential Leaks in Logs**
- *Mitigation:* Automated scanning in CI for `password`, `secret` in test outputs
- *Fallback:* Memory-only storage, never persist to disk

### Resource Constraints

**Single Binary Size**
- Target: <20MB (Go compiles statically)
- Strategy: Avoid CGO where possible, use `crypto/ssh` not external binaries

**Memory per Session**
- Target: <10MB per SSH session
- Strategy: Stream output, don't buffer entire PTY history

## Contribution Guidelines

- **Go Version:** 1.22+
- **Testing:** All new features require integration tests
- **Documentation:** Update CLAUDE.md for protocol changes, ROADMAP.md for schedule changes
- **Commits:** Conventional commits (`feat:`, `fix:`, `docs:`)

## Feedback Loop

After each phase, test with real Claude Code scenarios:

1. **Basic:** `ls -la`, `cd`, `pwd` (state persistence)
2. **Moderate:** `git clone`, `npm install` (environment handling)
3. **Complex:** `sudo apt update && sudo apt upgrade` (sudo caching)
4. **Wizard:** `npm init @latest` (interactive prompt handling)

Adjust roadmap based on friction points discovered.

---

*Last updated: 2025-01-30*  
*Target v1.0 release: 14 weeks from start*
```

These documents provide a complete specification for building this tool. The **CLAUDE.md** focuses on the protocol design, Go architecture, and MCP tool schemas, while the **ROADMAP.md** breaks down the 14-week implementation into concrete deliverables with risk mitigation strategies.

Key architectural decisions captured:
- **Explicit interrupt pattern** instead of sampling (as you requested)
- **Go's `crypto/ssh` + `creack/pty`** for robust SSH/PTY handling  
- **Secure in-memory credential cache** with automatic wiping
- **Session state machine** (running/awaiting_input/completed)
- **14-week phased approach** delivering incremental value
