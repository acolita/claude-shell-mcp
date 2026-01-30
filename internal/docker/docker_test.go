package docker

import (
	"strings"
	"testing"
)

func TestBuildExecCommand(t *testing.T) {
	tests := []struct {
		name     string
		opts     ExecOptions
		contains []string
	}{
		{
			name: "basic exec",
			opts: ExecOptions{
				Container: "mycontainer",
				Command:   "ls -la",
			},
			contains: []string{"docker", "exec", "mycontainer", "/bin/sh", "-c", "ls -la"},
		},
		{
			name: "interactive exec",
			opts: ExecOptions{
				Container:   "mycontainer",
				Command:     "bash",
				Interactive: true,
			},
			contains: []string{"docker", "exec", "-it", "mycontainer", "bash"},
		},
		{
			name: "exec with user",
			opts: ExecOptions{
				Container:   "mycontainer",
				Command:     "whoami",
				User:        "root",
				Interactive: true,
			},
			contains: []string{"-u", "root"},
		},
		{
			name: "exec with workdir",
			opts: ExecOptions{
				Container: "mycontainer",
				Command:   "pwd",
				WorkDir:   "/app",
			},
			contains: []string{"-w", "/app"},
		},
		{
			name: "exec with env",
			opts: ExecOptions{
				Container: "mycontainer",
				Command:   "env",
				Env:       map[string]string{"FOO": "bar"},
			},
			contains: []string{"-e", "FOO=bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildExecCommand(tt.opts)
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("BuildExecCommand() = %q, should contain %q", result, expected)
				}
			}
		})
	}
}

func TestBuildComposeExecCommand(t *testing.T) {
	tests := []struct {
		name     string
		opts     ComposeExecOptions
		contains []string
	}{
		{
			name: "basic compose exec",
			opts: ComposeExecOptions{
				Service: "web",
				Command: "ls",
			},
			contains: []string{"docker", "compose", "exec", "web"},
		},
		{
			name: "non-interactive compose exec",
			opts: ComposeExecOptions{
				Service:     "web",
				Command:     "cat /etc/hostname",
				Interactive: false,
			},
			contains: []string{"-T"},
		},
		{
			name: "compose exec with custom file",
			opts: ComposeExecOptions{
				Service:     "db",
				Command:     "psql",
				ComposeFile: "docker-compose.prod.yml",
				Interactive: true,
			},
			contains: []string{"-f", "docker-compose.prod.yml"},
		},
		{
			name: "compose exec with index",
			opts: ComposeExecOptions{
				Service:     "worker",
				Command:     "ps aux",
				Index:       2,
				Interactive: true,
			},
			contains: []string{"--index", "2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildComposeExecCommand(tt.opts)
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("BuildComposeExecCommand() = %q, should contain %q", result, expected)
				}
			}
		})
	}
}

func TestBuildRunCommand(t *testing.T) {
	tests := []struct {
		name     string
		opts     RunOptions
		contains []string
	}{
		{
			name: "basic run",
			opts: RunOptions{
				Image:   "alpine",
				Command: "echo hello",
			},
			contains: []string{"docker", "run", "alpine", "echo", "hello"},
		},
		{
			name: "interactive run with rm",
			opts: RunOptions{
				Image:       "ubuntu",
				Command:     "bash",
				Interactive: true,
				Remove:      true,
			},
			contains: []string{"--rm", "-it", "ubuntu", "bash"},
		},
		{
			name: "run with volumes and ports",
			opts: RunOptions{
				Image:   "nginx",
				Volumes: []string{"/host/path:/container/path"},
				Ports:   []string{"8080:80"},
				Detach:  true,
			},
			contains: []string{"-d", "-v", "/host/path:/container/path", "-p", "8080:80"},
		},
		{
			name: "run with network",
			opts: RunOptions{
				Image:   "myapp",
				Network: "mynetwork",
				Name:    "myapp-container",
			},
			contains: []string{"--network", "mynetwork", "--name", "myapp-container"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildRunCommand(tt.opts)
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("BuildRunCommand() = %q, should contain %q", result, expected)
				}
			}
		})
	}
}

func TestIsDockerCommand(t *testing.T) {
	tests := []struct {
		cmd      string
		expected bool
	}{
		{"docker ps", true},
		{"docker-compose up", true},
		{"docker exec -it mycontainer bash", true},
		{"ls -la", false},
		{"podman run alpine", false},
		{"  docker images", true},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := IsDockerCommand(tt.cmd); got != tt.expected {
				t.Errorf("IsDockerCommand(%q) = %v, want %v", tt.cmd, got, tt.expected)
			}
		})
	}
}

func TestNeedsTTY(t *testing.T) {
	tests := []struct {
		cmd      string
		expected bool
	}{
		{"docker exec -it mycontainer bash", true},
		{"docker run -it alpine sh", true},
		{"docker exec mycontainer cat /etc/hostname", false},
		{"docker run alpine echo hello", false},
		{"docker compose exec web python manage.py shell", true},
		{"docker run -d nginx", false},
		{"docker exec mycontainer mysql -u root", true},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := NeedsTTY(tt.cmd); got != tt.expected {
				t.Errorf("NeedsTTY(%q) = %v, want %v", tt.cmd, got, tt.expected)
			}
		})
	}
}

func TestEnsureTTYFlags(t *testing.T) {
	tests := []struct {
		cmd      string
		expected string
	}{
		{
			cmd:      "docker exec mycontainer bash",
			expected: "docker exec -it mycontainer bash",
		},
		{
			cmd:      "docker exec -it mycontainer bash",
			expected: "docker exec -it mycontainer bash",
		},
		{
			cmd:      "docker run alpine bash",
			expected: "docker run -it alpine bash",
		},
		{
			cmd:      "docker run alpine echo hello",
			expected: "docker run alpine echo hello", // No shell, no TTY needed
		},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := EnsureTTYFlags(tt.cmd); got != tt.expected {
				t.Errorf("EnsureTTYFlags(%q) = %q, want %q", tt.cmd, got, tt.expected)
			}
		})
	}
}
