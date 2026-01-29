# claude-shell-mcp Makefile

VERSION ?= 0.1.0-alpha
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)"

.PHONY: all build clean test lint run install

all: build

build:
	go build $(LDFLAGS) -o claude-shell-mcp ./cmd/claude-shell-mcp

install:
	go install $(LDFLAGS) ./cmd/claude-shell-mcp

clean:
	rm -f claude-shell-mcp
	go clean ./...

test:
	go test ./...

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run

fmt:
	go fmt ./...

vet:
	go vet ./...

run: build
	./claude-shell-mcp --mode local

# Test MCP handshake
test-mcp:
	@echo '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}' | ./claude-shell-mcp 2>/dev/null | head -1 | jq .

# Build for multiple platforms
build-all:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/claude-shell-mcp-linux-amd64 ./cmd/claude-shell-mcp
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/claude-shell-mcp-darwin-amd64 ./cmd/claude-shell-mcp
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/claude-shell-mcp-darwin-arm64 ./cmd/claude-shell-mcp
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/claude-shell-mcp-windows-amd64.exe ./cmd/claude-shell-mcp
