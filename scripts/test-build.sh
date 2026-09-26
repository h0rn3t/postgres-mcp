#!/bin/bash

# Test build script to verify everything compiles correctly
# This runs the same build commands that the release workflow will use

set -e

echo "Testing build for multiple platforms..."

# Test builds for main platforms
echo "Building for Linux amd64..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/postgres-mcp-server-linux-amd64 ./server
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/postgres-mcp-client-linux-amd64 ./client

echo "Building for macOS amd64..."
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/postgres-mcp-server-darwin-amd64 ./server
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/postgres-mcp-client-darwin-amd64 ./client

echo "Building for macOS arm64..."
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/postgres-mcp-server-darwin-arm64 ./server
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/postgres-mcp-client-darwin-arm64 ./client

echo "Building for Windows amd64..."
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/postgres-mcp-server-windows-amd64.exe ./server
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/postgres-mcp-client-windows-amd64.exe ./client

echo "All builds successful!"

# Test version flags (local build)
echo "Testing version flags..."
go build -o /tmp/postgres-mcp-server-test ./server
go build -o /tmp/postgres-mcp-client-test ./client

echo "Server version:"
/tmp/postgres-mcp-server-test -version

echo "Client version:"
/tmp/postgres-mcp-client-test -version

# Clean up
rm -f /tmp/postgres-mcp-*

echo "All tests passed!"
