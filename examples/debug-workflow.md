# Debug Workflow Example

This example shows how to debug a production issue using claude-shell-mcp.

## Scenario: Investigating High Memory Usage

### 1. Connect to Server

```json
{
  "tool": "shell_session_create",
  "arguments": {
    "mode": "ssh",
    "host": "prod.example.com",
    "user": "ops"
  }
}
```

### 2. Check System Overview

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "free -h && df -h && uptime"
  }
}
```

### 3. Find Memory-Hungry Processes

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "ps aux --sort=-%mem | head -20"
  }
}
```

### 4. Check Application Logs

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "tail -100 /var/log/myapp/app.log | grep -i error"
  }
}
```

### 5. Monitor Live Logs

For real-time monitoring, use a limited tail:

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "timeout 10 tail -f /var/log/myapp/app.log",
    "timeout_ms": 15000
  }
}
```

### 6. Check Network Connections

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "netstat -tlnp 2>/dev/null | grep -E ':(80|443|3000)'"
  }
}
```

### 7. Query Database (if needed)

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "mysql -u app -p -e 'SELECT COUNT(*) FROM sessions WHERE created_at > NOW() - INTERVAL 1 HOUR'"
  }
}
```

If password is prompted:
```json
{
  "status": "awaiting_input",
  "prompt_type": "password",
  "prompt_text": "Enter password:"
}
```

### 8. Check Docker Containers (if applicable)

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'"
  }
}
```

### 9. Inspect Docker Logs

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "docker logs --tail 50 myapp-container"
  }
}
```

## Handling Long-Running Commands

For commands that might take a while, use appropriate timeouts:

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "find /var/log -name '*.log' -mtime -1 -size +100M",
    "timeout_ms": 60000
  }
}
```

## Interrupting Commands

If a command is taking too long:

```json
{
  "tool": "shell_interrupt",
  "arguments": {
    "session_id": "sess_abc123"
  }
}
```

## Session Status Check

To check current session state:

```json
{
  "tool": "shell_session_status",
  "arguments": {
    "session_id": "sess_abc123"
  }
}
```

Response includes:
- Current working directory
- Environment variables
- Shell aliases
- Connection status
- Sudo cache status
