# Build stage
FROM golang:1.19-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY *.go ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o proxmox-dns-server .

# Runtime stage
FROM alpine:latest

# Install ca-certificates for any HTTPS calls
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /build/proxmox-dns-server .

# Expose DNS port
EXPOSE 53/udp

# Run as root (required for Proxmox commands)
USER root

ENTRYPOINT ["/app/proxmox-dns-server"]
