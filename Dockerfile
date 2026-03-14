# Build stage - use native platform for faster compilation
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

# Build arguments for version injection
ARG VERSION=dev
ARG COMMIT_HASH=unknown
ARG BUILD_TIME=unknown

# Declare buildx automatic platform variables
ARG TARGETOS
ARG TARGETARCH

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Download dependencies first (better caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application for target platform with version info
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-w -s -X main.Version=${VERSION} -X main.CommitHash=${COMMIT_HASH} -X main.BuildTime=${BUILD_TIME}" \
    -o app ./cmd/app

# Runtime stage - distroless for minimal attack surface
FROM gcr.io/distroless/static:nonroot

WORKDIR /

# Copy the binary from builder stage
COPY --from=builder /app/app /app

ENTRYPOINT ["/app"]
