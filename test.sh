#!/bin/bash

# Test script for MCP Universal Bridge
# Runs all tests and generates coverage report

set -e

echo "=== MCP Universal Bridge Test Suite ==="
echo

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
COVERAGE_THRESHOLD=80
COVERAGE_DIR="coverage"
COVERAGE_FILE="$COVERAGE_DIR/coverage.out"
COVERAGE_HTML="$COVERAGE_DIR/coverage.html"

# Create coverage directory
mkdir -p $COVERAGE_DIR

# Clean previous coverage
rm -f $COVERAGE_FILE
rm -f $COVERAGE_HTML

echo "Running unit tests with coverage..."
echo "=================================="

# Run tests with coverage for each package
go test -v -race -coverprofile=$COVERAGE_DIR/mcp.out ./pkg/mcp
go test -v -race -coverprofile=$COVERAGE_DIR/config.out ./internal/config
go test -v -race -coverprofile=$COVERAGE_DIR/transport.out ./internal/transport
go test -v -race -coverprofile=$COVERAGE_DIR/bridge.out ./internal/bridge

# Combine coverage files
echo "mode: set" > $COVERAGE_FILE
tail -n +2 $COVERAGE_DIR/mcp.out >> $COVERAGE_FILE 2>/dev/null || true
tail -n +2 $COVERAGE_DIR/config.out >> $COVERAGE_FILE 2>/dev/null || true
tail -n +2 $COVERAGE_DIR/transport.out >> $COVERAGE_FILE 2>/dev/null || true
tail -n +2 $COVERAGE_DIR/bridge.out >> $COVERAGE_FILE 2>/dev/null || true

echo
echo "Running integration tests..."
echo "============================"
go test -v -tags=integration ./...

echo
echo "Running benchmark tests..."
echo "========================="
go test -bench=. -benchmem ./... | grep -E "^(Benchmark|goos:|goarch:|cpu:|pkg:)"

echo
echo "Coverage Report"
echo "==============="

# Generate coverage report
go tool cover -func=$COVERAGE_FILE | tail -n 1

# Extract coverage percentage
COVERAGE=$(go tool cover -func=$COVERAGE_FILE | tail -n 1 | awk '{print $3}' | sed 's/%//')
COVERAGE_INT=${COVERAGE%.*}

echo
if [ "$COVERAGE_INT" -ge "$COVERAGE_THRESHOLD" ]; then
    echo -e "${GREEN}✅ Coverage: ${COVERAGE}% (threshold: ${COVERAGE_THRESHOLD}%)${NC}"
else
    echo -e "${RED}❌ Coverage: ${COVERAGE}% (below threshold: ${COVERAGE_THRESHOLD}%)${NC}"
fi

# Generate HTML coverage report
go tool cover -html=$COVERAGE_FILE -o=$COVERAGE_HTML
echo
echo "HTML coverage report generated: $COVERAGE_HTML"

# Detailed coverage by package
echo
echo "Coverage by Package"
echo "==================="
go tool cover -func=$COVERAGE_FILE | grep -E "^github.com" | sort

# Check for uncovered critical functions
echo
echo "Critical Functions Coverage"
echo "==========================="
CRITICAL_FUNCTIONS=(
    "Connect"
    "Send"
    "Receive"
    "Start"
    "Route"
    "Marshal"
    "ParseMessage"
)

for func in "${CRITICAL_FUNCTIONS[@]}"; do
    coverage=$(go tool cover -func=$COVERAGE_FILE | grep "$func" | head -n 1)
    if [ -n "$coverage" ]; then
        percent=$(echo "$coverage" | awk '{print $3}')
        if [[ "$percent" == "100.0%" ]]; then
            echo -e "${GREEN}✅ $func: $percent${NC}"
        elif [[ "$percent" == "0.0%" ]]; then
            echo -e "${RED}❌ $func: $percent${NC}"
        else
            echo -e "${YELLOW}⚠️  $func: $percent${NC}"
        fi
    fi
done

# Run race condition detection
echo
echo "Race Condition Detection"
echo "========================"
echo "Running with race detector..."
if go test -race -short ./... > /dev/null 2>&1; then
    echo -e "${GREEN}✅ No race conditions detected${NC}"
else
    echo -e "${RED}❌ Race conditions detected!${NC}"
    go test -race -short ./...
fi

# Memory leak detection (basic)
echo
echo "Memory Analysis"
echo "==============="
go test -run=TestMemoryLeaks -memprofile=$COVERAGE_DIR/mem.prof ./... 2>/dev/null || true
if [ -f "$COVERAGE_DIR/mem.prof" ]; then
    echo "Memory profile generated: $COVERAGE_DIR/mem.prof"
    echo "Run 'go tool pprof $COVERAGE_DIR/mem.prof' to analyze"
fi

# Test with different configurations
echo
echo "Configuration Tests"
echo "==================="

# Test with JSON storage
echo -n "Testing with JSON storage backend... "
STORAGE_TYPE=json go test -short ./internal/transport > /dev/null 2>&1 && \
    echo -e "${GREEN}✅ Passed${NC}" || \
    echo -e "${RED}❌ Failed${NC}"

# Test with SQLite storage
echo -n "Testing with SQLite storage backend... "
STORAGE_TYPE=sqlite go test -short ./internal/transport > /dev/null 2>&1 && \
    echo -e "${GREEN}✅ Passed${NC}" || \
    echo -e "${RED}❌ Failed${NC}"

# Summary
echo
echo "Test Summary"
echo "============"
TOTAL_TESTS=$(go test -list . ./... 2>/dev/null | grep -c "^Test")
echo "Total test functions: $TOTAL_TESTS"
echo "Coverage: ${COVERAGE}%"
echo "Threshold: ${COVERAGE_THRESHOLD}%"

if [ "$COVERAGE_INT" -ge "$COVERAGE_THRESHOLD" ]; then
    echo -e "${GREEN}✅ All tests passed with sufficient coverage!${NC}"
    exit 0
else
    echo -e "${RED}❌ Tests passed but coverage is below threshold${NC}"
    exit 1
fi