# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy only source code directories needed for build
# (sensitive files excluded via .dockerignore as defense in depth)
COPY cmd/ cmd/
COPY internal/ internal/

# Build the binary
ARG VERSION=dev
ARG GIT_COMMIT=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT}" \
    -o claude-shell-mcp \
    ./cmd/claude-shell-mcp

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies and create non-root user
RUN apk add --no-cache \
    ca-certificates \
    openssh-client \
    bash && \
    adduser -D -h /home/mcp mcp && \
    mkdir -p /home/mcp/.config/claude-shell-mcp && \
    chown -R mcp:mcp /home/mcp

USER mcp
WORKDIR /home/mcp

# Copy binary from builder
COPY --from=builder /build/claude-shell-mcp /usr/local/bin/

# Default environment
ENV HOME=/home/mcp

# Volume for SSH keys and config
VOLUME ["/home/mcp/.ssh", "/home/mcp/.config/claude-shell-mcp"]

# Entry point
ENTRYPOINT ["claude-shell-mcp"]
CMD ["--mode", "local"]
