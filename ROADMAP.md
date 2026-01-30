# Claude Shell MCP - Implementation Roadmap

## Phase 0: Foundation (Week 1-2)
**Goal:** Working stdio MCP server with local PTY support

### Week 1: Project Scaffold
- [x] Initialize Go module (`github.com/acolita/claude-shell-mcp`)
- [x] Set up CI/CD (GitHub Actions: lint, test, build)
- [x] Implement MCP protocol handler using `mcp-go`
- [x] Basic logging infrastructure (slog with JSON)
- [x] Configuration parsing (YAML/JSON)

**Deliverable:** `claude-shell-mcp --version` runs and responds to `initialize` request ✅

### Week 2: Local PTY Core
- [x] Integrate `creack/pty` for local shell spawning
- [x] Implement session management (in-memory store)
- [x] Tool: `shell_session_create` (local mode)
- [x] Tool: `shell_exec` with timeout
- [x] Tool: `shell_session_close`
- [x] Basic output capture (stdout/stderr separation)

**Deliverable:** Can execute `echo "hello"` and return output via MCP ✅

---

## Phase 1: SSH Backbone (Week 3-4)
**Goal:** Remote SSH connections with session persistence

### Week 3: SSH Client
- [x] SSH connection pooling using `crypto/ssh`
- [x] Key-based authentication (RSA, Ed25519)
- [x] SSH agent forwarding support
- [x] Host key verification (known_hosts)
- [x] PTY allocation over SSH channel (`RequestPty`)
- [x] Password authentication support

**Deliverable:** Connect to remote server and execute `uname -a` ✅

### Week 4: Session Lifecycle
- [x] Persistent SSH sessions across MCP tool calls
- [x] Heartbeat/keepalive (prevent connection drop)
- [x] Automatic reconnection with state recovery
- [x] Idle timeout detection and cleanup
- [x] Session multiplexing (multiple sessions per MCP instance)

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
- [x] Pattern matching system (regex registry)
- [x] Default patterns: sudo, SSH confirmations, apt, npm, git
- [x] Configurable custom patterns
- [x] PTY output buffering (last N lines)
- [x] Non-blocking I/O with deadline-based reads

**Deliverable:** Detects `[sudo] password:` and pauses execution ✅

### Week 6: Interrupt & Resume Tools
- [x] `shell_exec` returns `status: "awaiting_input"`
- [x] `shell_provide_input` tool implementation
- [x] Input injection into PTY (with proper echo handling)
- [x] `shell_interrupt` (Ctrl+C sending)
- [x] Multi-turn conversation state machine

**Deliverable:** Full flow: ✅
```
exec("sudo whoami") → awaiting_input → provide_input("pass") → completed (returns "root")
```

---

## Phase 3: Security & Credentials (Week 7-8)
**Goal:** Production-ready credential handling

### Week 7: Secure Cache
- [x] In-memory encrypted credential cache
- [x] Automatic cryptographic wipe after TTL
- [x] Sudo credential caching (5-min default)
- [x] Passphrase-protected SSH key support
- [x] Integration with OS keyring (optional)

**Deliverable:** Sudo password cached securely, auto-injected for subsequent commands ✅

### Week 8: Audit & Safety
- [x] Comprehensive audit logging (no secrets)
- [x] Command allowlist/blocklist (optional)
- [x] Session recording (asciicast format)
- [x] Max session limits per user
- [x] Rate limiting on authentication attempts

**Deliverable:** Security audit passes, no plaintext passwords in logs ✅

---

## Phase 4: Environment & State (Week 9-10)
**Goal:** Full shell state persistence (env vars, cwd)

### Week 9: State Snapshots
- [x] Environment variable capture (`env` parsing)
- [x] Working directory tracking (`pwd`)
- [x] Shell alias/function detection (bash/zsh)
- [x] State restoration on session reconnect
- [x] `shell_session_status` tool

**Deliverable:** ✅
```
Turn 1: export DB_HOST=localhost
Turn 2: echo $DB_HOST  # Returns localhost via state restoration
```

### Week 10: Shell Compatibility
- [x] Bash support (primary)
- [x] Zsh support
- [x] Fish shell support (basic)
- [x] Shell detection and adaptation
- [x] Custom shell initialization (.bashrc sourcing control)

**Deliverable:** Works on Ubuntu (bash), macOS (zsh), Alpine (ash)

---

## Phase 5: Advanced Features (Week 11-12)
**Goal:** Complex workflows and developer experience

### Week 11: Complex Interactives
- [x] Support for `npm init`, `vue create` wizards
- [x] Git interactive rebase handling
- [x] Docker-compose TTY allocation
- [x] Vim/nano detection (block or hand off)
- [x] Expect-like scripting for known workflows

**Deliverable:** Successfully runs `npm init` answering all prompts via LLM

### Week 12: Developer Experience
- [x] Configuration file hot-reload
- [x] Verbose debug mode (hex PTY dumps)
- [x] Local testing mode (mock SSH server)
- [x] Claude Desktop integration guide
- [x] Docker image for easy deployment

**Deliverable:** 100% test coverage on critical paths, documentation complete

---

## Phase 6: Integration & Hardening (Week 13-14)
**Goal:** Production deployment and ecosystem integration

### Week 13: Claude Code Integration
- [x] Test with Claude Code (not just Claude Desktop)
- [x] Work around Claude Code bash limitations
- [x] Optimize for Claude Code's specific tool calling patterns
- [x] Example workflows (deployment, debugging, log analysis)

**Deliverable:** End-to-end demo: "Deploy my app" across 3 servers with git pull, npm install, pm2 restart

### Week 14: Performance & Scale
- [x] Connection pooling optimization
- [x] Memory usage profiling (leak detection)
- [x] Concurrent session stress testing (100+ sessions)
- [x] Binary size optimization (strip with -s -w)
- [x] Cross-compilation (Linux, macOS, Windows)

**Deliverable:** v1.0.0 release, binary < 20MB, handles 50 concurrent sessions

---

## Future Phases (Post v1.0)

### Phase 7: Ecosystem (Month 4)
- [ ] VS Code extension for session visualization
- [ ] Web dashboard for session monitoring
- [ ] Ansible/Terraform integration modules
- [ ] Support for AWS SSM, GCP IAP (beyond SSH)

### Phase 8: Intelligence (Month 5)
- [x] LLM-based prompt classification (not just regex)
- [x] Automatic error recovery (suggest fixes for common failures)
- [x] Smart sudo detection (parse sudoers, predict password needs)
- [ ] Integration with 1Password/Bitwarden CLI for secrets

### Phase 9: Control Plane Architecture (Month 6)
**Goal:** Reliable process tracking via dedicated control session per host

#### 9.1: Control Plane Foundation
- [x] `ControlSession` type - lightweight session without prompt detection
- [x] One control session per host (shared across all sessions to that host)
- [x] Lazy initialization (created on first session to host)
- [x] PTY device path capture for each session (`/dev/pts/X`)

#### 9.2: Process Tracking
- [ ] Capture foreground PID on command execution
- [ ] `ps -t pts/X` to list processes on a PTY
- [ ] `pstree -p <shell_pid>` for process hierarchy
- [ ] Process state detection via `/proc/<pid>/stat`

#### 9.3: Reliable Termination
- [x] `pkill -t pts/X` to kill all processes on PTY
- [x] `kill -9 <pid>` for targeted termination
- [x] Graceful escalation: SIGINT → SIGTERM → SIGKILL (fallback)
- [ ] Verification that process actually died

#### 9.4: Input Detection via Control Plane
- [ ] Check process state: `S` (sleeping) on PTY read = waiting for input
- [ ] `/proc/<pid>/wchan` inspection for blocked syscall
- [ ] Combine with existing heuristics for better accuracy

**Deliverable:** Timeout reliably kills any process, including `top`, `vim`, `less`

### Phase 10: Local eBPF Integration (Month 7)
**Goal:** Perfect process tracking for local sessions via kernel instrumentation

#### 10.1: eBPF Foundation
- [ ] eBPF program loader using `cilium/ebpf`
- [ ] Capability detection (CAP_BPF or root)
- [ ] Graceful fallback if eBPF unavailable
- [ ] Ring buffer for kernel→userspace events

#### 10.2: Process Lifecycle Tracking
- [ ] `tracepoint/sched/sched_process_exit` - instant exit notification
- [ ] `tracepoint/sched/sched_process_fork` - track child processes
- [ ] `tracepoint/syscalls/sys_enter_execve` - know what binary runs
- [ ] PID filtering to only watch our sessions

#### 10.3: Input Detection via eBPF
- [ ] `tracepoint/syscalls/sys_enter_read` on PTY fd
- [ ] Detect when process blocks on terminal read
- [ ] Extract "last output before read" as prompt hint
- [ ] Sub-millisecond latency on state changes

#### 10.4: Signal Tracking
- [ ] `tracepoint/signal/signal_deliver` - confirm signals received
- [ ] Know if SIGINT was caught vs killed process
- [ ] Automatic escalation if signal ignored

**Deliverable:** Local sessions have perfect "awaiting_input" detection, instant completion notification

### Phase 11: Remote eBPF Agent (Month 8-9)
**Goal:** eBPF-powered monitoring for remote SSH sessions

#### 11.1: Agent Binary
- [ ] `claude-shell-agent` - single static binary (~5MB)
- [ ] Embeds eBPF programs (CO-RE for portability)
- [ ] Runs with CAP_BPF or as root
- [ ] Minimal dependencies (just kernel 5.x+)

#### 11.2: Agent Protocol
- [ ] Protobuf-based event streaming
- [ ] Events: ProcessExit, ProcessSpawn, WaitingForInput, SignalDelivered
- [ ] Commands: Watch, Unwatch, Kill, InjectInput
- [ ] Bidirectional communication over SSH channel

#### 11.3: Agent Discovery & Launch
- [ ] Check for agent: `ssh host "claude-shell-agent --version"`
- [ ] Capability negotiation (what eBPF features available)
- [ ] On-demand launch via SSH
- [ ] Optional: auto-deploy if missing (with user consent)

#### 11.4: Hybrid Session Manager
- [ ] Per-host capability tracking
- [ ] Automatic selection: Agent (eBPF) > Control Plane > Heuristics
- [ ] Seamless fallback if agent crashes
- [ ] Mixed-mode: some hosts with agent, some without

**Architecture:**
```
SessionManager
├── HostConnection (localhost)
│   ├── Agent: eBPF direct (no SSH needed)
│   ├── Sessions: [sess_a, sess_b]
│   └── Fallback: Control Plane
│
├── HostConnection (prod-server)
│   ├── Agent: claude-shell-agent v1.0 (eBPF over SSH)
│   ├── Sessions: [sess_c]
│   └── Fallback: Control Plane
│
└── HostConnection (legacy-server)
    ├── Agent: not available (kernel 4.x)
    ├── Sessions: [sess_d]
    └── Fallback: Control Plane (active)
```

**Deliverable:** Remote sessions get eBPF-level accuracy when agent is deployed

### Phase 12: Agent Deployment & Management (Month 10)
**Goal:** Easy agent rollout across infrastructure

#### 12.1: Installation Methods
- [ ] Package repositories (apt, dnf, apk)
- [ ] Single binary download (curl | sh)
- [ ] Docker sidecar mode
- [ ] Ansible/Terraform modules

#### 12.2: Auto-Deployment
- [ ] MCP server offers to install agent on first connect
- [ ] `scp` binary to remote host
- [ ] Verify checksum before execution
- [ ] Cleanup on session end (optional)

#### 12.3: Agent Updates
- [ ] Version compatibility matrix
- [ ] Automatic update detection
- [ ] Rolling update without session interruption
- [ ] Rollback on failure

#### 12.4: Security Considerations
- [ ] Agent runs with minimal privileges (CAP_BPF only)
- [ ] Mutual authentication (agent ↔ MCP server)
- [ ] Audit log of all agent actions
- [ ] Rate limiting on event stream

**Deliverable:** One-command agent deployment, automatic updates

### Phase 13: File Transfer - SCP (Month 11)
**Goal:** Seamless file GET/PUT over SSH sessions - a killer feature for LLM agents

#### 13.1: Core Transfer Tools
- [ ] `shell_file_get` tool - download file from remote session
  - Return file content directly (small files, <1MB)
  - Save to local path (large files)
  - Support binary and text modes
- [ ] `shell_file_put` tool - upload file to remote session
  - Accept content directly (small files)
  - Read from local path (large files)
  - Set permissions on upload (chmod)

#### 13.2: Directory Operations
- [ ] Recursive directory download (`-r` flag)
- [ ] Recursive directory upload
- [ ] Glob pattern support (`*.log`, `src/**/*.go`)
- [ ] Exclusion patterns (`.git`, `node_modules`)

#### 13.3: Large File Handling
- [ ] Streaming transfer for files >1MB
- [ ] Progress reporting (bytes transferred, percentage)
- [ ] Chunked transfer with resume capability
- [ ] Checksum verification (SHA256)

#### 13.4: Session Integration
- [ ] Use existing SSH session (no reconnect needed)
- [ ] Respect session's working directory for relative paths
- [ ] Transfer while command is running (separate channel)
- [ ] Atomic writes (temp file + rename)

#### 13.5: Advanced Features
- [ ] Compression on-the-fly (gzip for text files)
- [ ] Diff-based sync (only transfer changed portions)
- [ ] Symlink handling (follow vs preserve)
- [ ] Preserve timestamps and permissions

**Use Cases:**
```
# Download log file for analysis
shell_file_get(session_id="s1", remote_path="/var/log/app.log")

# Upload config file
shell_file_put(session_id="s1", remote_path="/etc/app/config.yaml", content="...")

# Download entire directory
shell_file_get(session_id="s1", remote_path="/app/src", local_path="./backup", recursive=true)

# Upload deployment artifact
shell_file_put(session_id="s1", remote_path="/app/release.tar.gz", local_path="./dist/release.tar.gz")
```

**Deliverable:** LLM can read, write, and sync files on remote servers without shell workarounds (cat, base64, scp)

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
| File Transfer (SCP) | Month 11 | GET/PUT files without shell workarounds |

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

*Last updated: 2026-01-30*
*v1.0.0 Released: 2026-01-29*
