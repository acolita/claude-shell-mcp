# Server Setup Workflow Example

This example shows how to provision a new server using claude-shell-mcp.

## Scenario: Setting Up a New Node.js Application Server

### 1. Connect to New Server

```json
{
  "tool": "shell_session_create",
  "arguments": {
    "mode": "ssh",
    "host": "new-server.example.com",
    "user": "root"
  }
}
```

### 2. Update System Packages

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "apt update && apt upgrade -y",
    "timeout_ms": 300000
  }
}
```

This may prompt for confirmation:
```json
{
  "status": "awaiting_input",
  "prompt_type": "confirmation",
  "prompt_text": "Do you want to continue? [Y/n]",
  "hint": "Confirmation required. Suggested response: Y"
}
```

Respond:
```json
{
  "tool": "shell_provide_input",
  "arguments": {
    "session_id": "sess_abc123",
    "input": "Y"
  }
}
```

### 3. Install Essential Packages

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "apt install -y curl git nginx certbot python3-certbot-nginx",
    "timeout_ms": 180000
  }
}
```

### 4. Install Node.js via NodeSource

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && apt install -y nodejs",
    "timeout_ms": 120000
  }
}
```

### 5. Create Application User

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "useradd -m -s /bin/bash appuser"
  }
}
```

### 6. Set Up Application Directory

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "mkdir -p /var/www/myapp && chown appuser:appuser /var/www/myapp"
  }
}
```

### 7. Clone Application Repository

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "su - appuser -c 'git clone https://github.com/myorg/myapp.git /var/www/myapp'"
  }
}
```

### 8. Install PM2 Process Manager

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "npm install -g pm2"
  }
}
```

### 9. Configure Nginx

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "cat > /etc/nginx/sites-available/myapp << 'EOF'\nserver {\n    listen 80;\n    server_name myapp.example.com;\n    location / {\n        proxy_pass http://localhost:3000;\n        proxy_http_version 1.1;\n        proxy_set_header Upgrade $http_upgrade;\n        proxy_set_header Connection 'upgrade';\n        proxy_set_header Host $host;\n        proxy_cache_bypass $http_upgrade;\n    }\n}\nEOF"
  }
}
```

### 10. Enable Site and Restart Nginx

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "ln -sf /etc/nginx/sites-available/myapp /etc/nginx/sites-enabled/ && nginx -t && systemctl restart nginx"
  }
}
```

### 11. Install Dependencies and Start App

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "cd /var/www/myapp && su - appuser -c 'cd /var/www/myapp && npm install && pm2 start npm --name myapp -- start'",
    "timeout_ms": 180000
  }
}
```

### 12. Configure PM2 Startup

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "pm2 startup systemd -u appuser --hp /home/appuser && su - appuser -c 'pm2 save'"
  }
}
```

### 13. Set Up SSL Certificate

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "certbot --nginx -d myapp.example.com --non-interactive --agree-tos -m admin@example.com"
  }
}
```

### 14. Verify Setup

```json
{
  "tool": "shell_exec",
  "arguments": {
    "session_id": "sess_abc123",
    "command": "curl -s http://localhost:3000/health && systemctl status nginx --no-pager"
  }
}
```

### 15. Close Session

```json
{
  "tool": "shell_session_close",
  "arguments": {
    "session_id": "sess_abc123"
  }
}
```

## Tips for Provisioning

1. **Use timeouts appropriately**: Package installations can take several minutes
2. **Handle confirmations**: apt/yum prompts are automatically detected
3. **Cache sudo passwords**: Use `cache_for_sudo: true` for repeated sudo commands
4. **Check session status**: Verify connection health during long operations
5. **Use heredocs for configs**: Write configuration files inline with `cat > file << 'EOF'`
