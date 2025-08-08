# Multi-stage build for minimal image size
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.Version=$(git describe --tags --always) \
    -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
    -X main.GitCommit=$(git rev-parse HEAD)" \
    -o mcp-bridge \
    cmd/mcp-bridge/main.go

# Final minimal image
FROM alpine:latest

# Add ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN adduser -D -u 1000 mcp

# Copy binary from builder
COPY --from=builder /build/mcp-bridge /usr/local/bin/
RUN chmod +x /usr/local/bin/mcp-bridge

# Switch to non-root user
USER mcp

# Default environment variables
ENV MCP_LOG_FORMAT=json \
    MCP_LOG_LEVEL=info

ENTRYPOINT ["mcp-bridge"]