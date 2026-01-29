# Deploy Workflow Example

This example shows how to deploy an application using claude-shell-mcp.

## Workflow Steps

### 1. Create SSH Session to Production Server

```json
{
  "tool": "shell_session_create",
  "arguments": {
    "mode": "ssh",
    "host": "prod.example.com",
    "user": "deploy"
  }
}
```

Response:
```json
{
  "session_id": "sess_abc123",
  "status": "connected",
  "mode": "ssh"
}
```

### 2. Navigate to Application Directory

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "cd /var/www/myapp"
  }
}
```

### 3. Pull Latest Code

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "git pull origin main"
  }
}
```

### 4. Install Dependencies

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "npm install",
    "timeout_ms": 120000
  }
}
```

### 5. Run Database Migrations

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "npm run migrate"
  }
}
```

### 6. Restart Application (with sudo)

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "sudo systemctl restart myapp"
  }
}
```

If sudo password is required, response will be:
```json
{
  "status": "awaiting_input",
  "prompt_type": "password",
  "prompt_text": "[sudo] password for deploy:",
  "hint": "Password required. Provide the password to continue."
}
```

Provide password:
```json
{
  "tool": "shell_provide_input",
  "arguments": {
    "session_id": "sess_abc123",
    "input": "your-password",
    "cache_for_sudo": true
  }
}
```

### 7. Verify Deployment

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "curl -s http://localhost:3000/health"
  }
}
```

### 8. Close Session

```json
{
  "tool": "shell_session_close",
  "arguments": {
    "session_id": "sess_abc123"
  }
}
```

## Multi-Server Deployment

For deploying to multiple servers, create separate sessions for each server and run commands in parallel:

```
Session 1 (prod1): git pull && npm install && pm2 reload all
Session 2 (prod2): git pull && npm install && pm2 reload all
Session 3 (prod3): git pull && npm install && pm2 reload all
```

The LLM agent can coordinate the deployment, waiting for each server to complete before moving to the next, or running them in parallel for zero-downtime deployments.
