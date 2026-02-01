# Stage 1: Build the Go binaries
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build both binaries
RUN CGO_ENABLED=0 GOOS=linux go build -o monitor_app ./cmd/monitor
RUN CGO_ENABLED=0 GOOS=linux go build -o backfill_app ./cmd/backfill

# Stage 2: Final image
FROM alpine:latest
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy both binaries
COPY --from=builder /app/monitor_app .
COPY --from=builder /app/backfill_app .

# No ENTRYPOINT here so we can specify it in docker-compose