# claude-shell-mcp Makefile

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "1.5.1")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)"

.PHONY: all build clean test test-e2e lint run install test-mcp build-all fmt vet

all: build

build:
	go build $(LDFLAGS) -o claude-shell-mcp ./cmd/claude-shell-mcp

install: build
	sudo cp claude-shell-mcp /usr/local/bin/

clean:
	rm -f claude-shell-mcp
	rm -rf dist/
	rm -f coverage.out coverage.html
	go clean ./...

test:
	go test -v -race ./...

test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
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
test-mcp: build
	@echo '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}' | ./claude-shell-mcp 2>/dev/null | head -1 | jq .

# Build for multiple platforms
build-all:
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/claude-shell-mcp-linux-amd64 ./cmd/claude-shell-mcp
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/claude-shell-mcp-linux-arm64 ./cmd/claude-shell-mcp
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/claude-shell-mcp-darwin-amd64 ./cmd/claude-shell-mcp
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/claude-shell-mcp-darwin-arm64 ./cmd/claude-shell-mcp
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/claude-shell-mcp-windows-amd64.exe ./cmd/claude-shell-mcp

# E2E tests with Docker SSH containers
test-e2e:
	docker compose -f test/e2e/docker-compose.yml up -d --build --wait
	go test -tags=e2e -v -timeout 300s ./test/e2e/...; \
	status=$$?; \
	docker compose -f test/e2e/docker-compose.yml down -v; \
	exit $$status

# Show version
version:
	@echo $(VERSION)
