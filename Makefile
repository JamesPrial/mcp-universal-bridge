.PHONY: all build test clean install docker release

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty)
BUILD_TIME = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT = $(shell git rev-parse HEAD)
LDFLAGS = -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)"

# Binary name
BINARY = mcp-bridge

all: build

build:
	@echo "Building $(BINARY) $(VERSION)..."
	@go build $(LDFLAGS) -o $(BINARY) cmd/mcp-bridge/main.go

test:
	@echo "Running tests..."
	@go test -v -race -cover ./...

test-coverage:
	@echo "Running tests with coverage..."
	@./test.sh

test-unit:
	@echo "Running unit tests..."
	@go test -v -race -coverprofile=coverage.out ./pkg/... ./internal/...
	@go tool cover -func=coverage.out

test-integration:
	@echo "Running integration tests..."
	@go test -v -tags=integration ./...

test-short:
	@echo "Running short tests..."
	@go test -short -v ./...

bench:
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem ./...

clean:
	@echo "Cleaning..."
	@rm -f $(BINARY)
	@rm -rf dist/
	@go clean -cache

install: build
	@echo "Installing $(BINARY)..."
	@sudo cp $(BINARY) /usr/local/bin/
	@echo "Installed to /usr/local/bin/$(BINARY)"

docker:
	@echo "Building Docker image..."
	@docker build -t $(BINARY):$(VERSION) -t $(BINARY):latest .

docker-push: docker
	@echo "Pushing Docker image..."
	@docker tag $(BINARY):latest jamesprial/$(BINARY):latest
	@docker tag $(BINARY):$(VERSION) jamesprial/$(BINARY):$(VERSION)
	@docker push jamesprial/$(BINARY):latest
	@docker push jamesprial/$(BINARY):$(VERSION)

# Cross-compilation for releases
release:
	@echo "Building releases..."
	@mkdir -p dist
	
	# Linux AMD64
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) \
		-o dist/$(BINARY)_linux_amd64 cmd/mcp-bridge/main.go
	
	# Linux ARM64
	@GOOS=linux GOARCH=arm64 go build $(LDFLAGS) \
		-o dist/$(BINARY)_linux_arm64 cmd/mcp-bridge/main.go
	
	# Darwin AMD64
	@GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) \
		-o dist/$(BINARY)_darwin_amd64 cmd/mcp-bridge/main.go
	
	# Darwin ARM64 (M1/M2)
	@GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) \
		-o dist/$(BINARY)_darwin_arm64 cmd/mcp-bridge/main.go
	
	# Windows AMD64
	@GOOS=windows GOARCH=amd64 go build $(LDFLAGS) \
		-o dist/$(BINARY)_windows_amd64.exe cmd/mcp-bridge/main.go
	
	@echo "Releases built in dist/"
	@ls -lh dist/

# Development helpers
run:
	@go run cmd/mcp-bridge/main.go --debug --server http://localhost:3000

fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@gofmt -s -w .

lint:
	@echo "Running linter..."
	@golangci-lint run

deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

help:
	@echo "Available targets:"
	@echo "  build    - Build the binary"
	@echo "  test     - Run tests"
	@echo "  bench    - Run benchmarks"
	@echo "  clean    - Clean build artifacts"
	@echo "  install  - Install binary to /usr/local/bin"
	@echo "  docker   - Build Docker image"
	@echo "  release  - Build release binaries for all platforms"
	@echo "  run      - Run in development mode"
	@echo "  fmt      - Format code"
	@echo "  lint     - Run linter"
	@echo "  deps     - Download dependencies"