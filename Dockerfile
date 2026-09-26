FROM scratch

# Copy the binaries from the build context (GoReleaser will place them here)
COPY postgres-mcp-server /postgres-mcp-server
COPY postgres-mcp-client /postgres-mcp-client

# Expose the default port
EXPOSE 8080

# Set the binary as the entrypoint
ENTRYPOINT ["/postgres-mcp-server"]
