# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the stub binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o stub-backend ./tests/e2e/stub

# Runtime stage
FROM alpine:3.20

# Install ca-certificates for HTTPS (if stub ever needs outbound calls)
RUN apk add --no-cache ca-certificates

# Copy binary from builder
COPY --from=builder /app/stub-backend /stub-backend

# Run as non-root user
USER 65534:65534

EXPOSE 8080

ENTRYPOINT ["/stub-backend"]
