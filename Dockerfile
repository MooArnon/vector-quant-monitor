# Stage 1: Build the Go binary
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy dependency files first (for caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the binary
# CGO_ENABLED=0 creates a static binary (no external C library dependencies)
RUN CGO_ENABLED=0 GOOS=linux go build -o monitor_app ./cmd/monitor

# Stage 2: Create the final tiny image
FROM alpine:latest

WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /app/monitor_app .
