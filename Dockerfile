# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o opsagent ./cmd/main.go

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 opsagent && \
    adduser -D -u 1000 -G opsagent opsagent

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/opsagent .

# Copy configuration files
COPY --from=builder /build/config ./config

# Create directories for runtime data
RUN mkdir -p /app/data /app/logs && \
    chown -R opsagent:opsagent /app

# Switch to non-root user
USER opsagent

# Expose ports
EXPOSE 8080 9090

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
ENTRYPOINT ["/app/opsagent"]
CMD ["serve"]
