// Package docker provides Docker and Docker Compose integration for claude-shell-mcp.
package docker

import (
	"fmt"
	"strings"
)

// ExecOptions configures Docker exec command generation.
type ExecOptions struct {
	Container   string            // Container name or ID
	Command     string            // Command to execute
	User        string            // User to run as (optional)
	WorkDir     string            // Working directory (optional)
	Env         map[string]string // Environment variables (optional)
	Interactive bool              // Allocate TTY (-it)
	Detach      bool              // Run in background (-d)
	Privileged  bool              // Extended privileges
}

// BuildExecCommand generates a docker exec command with proper TTY allocation.
func BuildExecCommand(opts ExecOptions) string {
	var args []string
	args = append(args, "docker", "exec")

	// TTY and interactive flags
	if opts.Interactive && !opts.Detach {
		args = append(args, "-it")
	} else if opts.Interactive {
		args = append(args, "-t")
	}

	if opts.Detach {
		args = append(args, "-d")
	}

	if opts.User != "" {
		args = append(args, "-u", opts.User)
	}

	if opts.WorkDir != "" {
		args = append(args, "-w", opts.WorkDir)
	}

	for key, value := range opts.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	if opts.Privileged {
		args = append(args, "--privileged")
	}

	args = append(args, opts.Container)

	// Handle command - if it contains spaces and isn't already a shell command, wrap it
	if opts.Command != "" {
		if strings.Contains(opts.Command, " ") && !strings.HasPrefix(opts.Command, "/bin/") && !strings.HasPrefix(opts.Command, "sh ") && !strings.HasPrefix(opts.Command, "bash ") {
			args = append(args, "/bin/sh", "-c", opts.Command)
		} else {
			args = append(args, strings.Fields(opts.Command)...)
		}
	}

	return strings.Join(args, " ")
}

// ComposeExecOptions configures Docker Compose exec command generation.
type ComposeExecOptions struct {
	Service     string            // Service name
	Command     string            // Command to execute
	User        string            // User to run as (optional)
	WorkDir     string            // Working directory (optional)
	Env         map[string]string // Environment variables (optional)
	Interactive bool              // Allocate TTY (-it)
	Index       int               // Container index if scaled (optional, 0 means default)
	ComposeFile string            // Custom compose file (optional)
	ProjectDir  string            // Project directory (optional)
}

// BuildComposeExecCommand generates a docker-compose exec command with proper TTY allocation.
func BuildComposeExecCommand(opts ComposeExecOptions) string {
	var args []string

	// Use docker compose (v2) by default
	args = append(args, "docker", "compose")

	if opts.ComposeFile != "" {
		args = append(args, "-f", opts.ComposeFile)
	}

	if opts.ProjectDir != "" {
		args = append(args, "--project-directory", opts.ProjectDir)
	}

	args = append(args, "exec")

	// TTY allocation - compose v2 allocates TTY by default when stdin is a tty
	// Use -T to disable TTY if not interactive
	if !opts.Interactive {
		args = append(args, "-T")
	}

	if opts.User != "" {
		args = append(args, "-u", opts.User)
	}

	if opts.WorkDir != "" {
		args = append(args, "-w", opts.WorkDir)
	}

	for key, value := range opts.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	if opts.Index > 0 {
		args = append(args, "--index", fmt.Sprintf("%d", opts.Index))
	}

	args = append(args, opts.Service)

	// Handle command
	if opts.Command != "" {
		if strings.Contains(opts.Command, " ") && !strings.HasPrefix(opts.Command, "/bin/") && !strings.HasPrefix(opts.Command, "sh ") && !strings.HasPrefix(opts.Command, "bash ") {
			args = append(args, "/bin/sh", "-c", opts.Command)
		} else {
			args = append(args, strings.Fields(opts.Command)...)
		}
	}

	return strings.Join(args, " ")
}

// RunOptions configures Docker run command generation.
type RunOptions struct {
	Image       string            // Image name
	Command     string            // Command to execute (optional)
	Name        string            // Container name (optional)
	User        string            // User to run as (optional)
	WorkDir     string            // Working directory (optional)
	Env         map[string]string // Environment variables
	Volumes     []string          // Volume mounts (host:container format)
	Ports       []string          // Port mappings (host:container format)
	Interactive bool              // Allocate TTY (-it)
	Remove      bool              // Remove container after exit (--rm)
	Detach      bool              // Run in background (-d)
	Network     string            // Network to connect to
	Privileged  bool              // Extended privileges
}

// BuildRunCommand generates a docker run command with proper TTY allocation.
func BuildRunCommand(opts RunOptions) string {
	var args []string
	args = append(args, "docker", "run")

	if opts.Remove {
		args = append(args, "--rm")
	}

	// TTY and interactive flags
	if opts.Interactive && !opts.Detach {
		args = append(args, "-it")
	} else if opts.Interactive {
		args = append(args, "-t")
	}

	if opts.Detach {
		args = append(args, "-d")
	}

	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}

	if opts.User != "" {
		args = append(args, "-u", opts.User)
	}

	if opts.WorkDir != "" {
		args = append(args, "-w", opts.WorkDir)
	}

	for key, value := range opts.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	for _, vol := range opts.Volumes {
		args = append(args, "-v", vol)
	}

	for _, port := range opts.Ports {
		args = append(args, "-p", port)
	}

	if opts.Network != "" {
		args = append(args, "--network", opts.Network)
	}

	if opts.Privileged {
		args = append(args, "--privileged")
	}

	args = append(args, opts.Image)

	if opts.Command != "" {
		args = append(args, strings.Fields(opts.Command)...)
	}

	return strings.Join(args, " ")
}

// IsDockerCommand returns true if the command is a Docker-related command.
func IsDockerCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	return strings.HasPrefix(cmd, "docker ") ||
		strings.HasPrefix(cmd, "docker-compose ") ||
		cmd == "docker" ||
		cmd == "docker-compose"
}

// NeedsTTY returns true if the Docker command likely needs TTY allocation.
func NeedsTTY(cmd string) bool {
	cmdLower := strings.ToLower(cmd)

	// Commands that already have TTY flags
	if strings.Contains(cmdLower, " -it") || strings.Contains(cmdLower, " --interactive") {
		return true
	}

	// Attach always needs TTY
	if strings.Contains(cmdLower, "docker attach") {
		return true
	}

	// Interactive commands that need TTY
	interactiveCommands := []string{
		"bash", "sh", "zsh", "fish",
		"/bin/bash", "/bin/sh", "/bin/zsh",
		"python", "python3", "ipython",
		"node", "ruby", "irb",
		"rails console", "rails c",
		"mysql", "psql", "redis-cli", "mongo", "mongosh",
		"htop", "top", "vim", "vi", "nano", "less", "more",
		"manage.py shell", // Django shell
	}

	// Check if any interactive command appears in the command string
	for _, ic := range interactiveCommands {
		// Check for the command as a word (not part of another word)
		icLower := strings.ToLower(ic)
		if strings.Contains(cmdLower, " "+icLower+" ") ||
			strings.HasSuffix(cmdLower, " "+icLower) ||
			strings.Contains(cmdLower, "/"+icLower+" ") ||
			strings.HasSuffix(cmdLower, "/"+icLower) {
			return true
		}
	}

	return false
}

// EnsureTTYFlags ensures the command has proper TTY flags if needed.
func EnsureTTYFlags(cmd string) string {
	cmd = strings.TrimSpace(cmd)

	// If it's docker exec without -t or -it, add them
	if strings.HasPrefix(cmd, "docker exec ") && !strings.Contains(cmd, " -t") && !strings.Contains(cmd, " -it") {
		cmd = strings.Replace(cmd, "docker exec ", "docker exec -it ", 1)
	}

	// Same for docker run if it looks interactive
	if strings.HasPrefix(cmd, "docker run ") && NeedsTTY(cmd) && !strings.Contains(cmd, " -t") && !strings.Contains(cmd, " -it") {
		cmd = strings.Replace(cmd, "docker run ", "docker run -it ", 1)
	}

	return cmd
}
