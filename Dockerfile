# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go module files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY *.go ./

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o proxmox-dns-server .

# Runtime stage
FROM alpine:3.20

WORKDIR /app

# Install ca-certificates for HTTPS calls to Proxmox API
RUN apk add --no-cache ca-certificates

# Copy the binary from builder
COPY --from=builder /app/proxmox-dns-server .

# Copy web templates
COPY web/ ./web/

# Expose DNS ports (UDP and TCP) and web UI port
EXPOSE 53/udp 53/tcp 8080

ENTRYPOINT ["/app/proxmox-dns-server"]
