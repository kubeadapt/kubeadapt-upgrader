# KubeAdapt Upgrader Makefile

# Variables
BINARY_NAME := upgrader
BUILD_DIR := ./build
CMD_PATH := ./cmd/upgrader
IMAGE_NAME := kubeadapt-upgrader
IMAGE_TAG ?= dev

# Go settings
GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED := 0
GO_BUILD_FLAGS := -ldflags="-w -s"

.PHONY: all build clean test lint tidy docker-build build-e2e-images test-e2e test-e2e-keep clean-e2e help

# Default target
all: tidy build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Binary built at $(BUILD_DIR)/$(BINARY_NAME)"

# Build for local platform
build-local:
	@echo "Building $(BINARY_NAME) for local platform..."
	@mkdir -p $(BUILD_DIR)
	go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Binary built at $(BUILD_DIR)/$(BINARY_NAME)"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@echo "Done"

# Run tests
test:
	@echo "Running tests..."
	go test -v -race -cover ./...

# Run tests with coverage report
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated at coverage.html"

# Run linter
lint:
	@echo "Running linter..."
	golangci-lint run ./...

# Tidy go modules
tidy:
	@echo "Tidying go modules..."
	go mod tidy

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download

# Docker build (disabled by default per user request)
docker-build:
	@echo "Docker build command (run manually if needed):"
	@echo "docker build -t $(IMAGE_NAME):$(IMAGE_TAG) ."

# Verify the build compiles
verify:
	@echo "Verifying build..."
	go build -o /dev/null $(CMD_PATH)
	@echo "Build verified successfully"

# Run locally (requires env vars)
run-local: build-local
	@echo "Running locally (set required env vars first)..."
	$(BUILD_DIR)/$(BINARY_NAME)

# Build E2E test images (also pulls public images needed by Kind)
build-e2e-images:
	@echo "Building E2E test images..."
	DOCKER_BUILDKIT=1 docker build -t localhost/kubeadapt-upgrader:e2e-test .
	DOCKER_BUILDKIT=1 docker build -t localhost/upgrade-stub:e2e-test -f tests/e2e/stub/Dockerfile .
	@echo "Pulling public images for Kind (failures ignored if already cached)..."
	-docker pull ghcr.io/helm/chartmuseum:v0.16.2
	-docker pull alpine/helm:3.14.3
	@echo "E2E images built and public images pulled"

# Run E2E tests
test-e2e: build-e2e-images
	@echo "Running E2E tests..."
	go test -v -timeout 30m -race -count=1 -tags e2e ./tests/e2e/...

# Run E2E tests (keeping cluster)
test-e2e-keep: build-e2e-images
	@echo "Running E2E tests (keeping cluster)..."
	E2E_SKIP_CLEANUP=1 go test -v -timeout 30m -race -count=1 -tags e2e ./tests/e2e/...

# Clean E2E cluster
clean-e2e:
	@echo "Cleaning E2E cluster..."
	-kind delete cluster --name kubeadapt-upgrader-e2e
	@echo "E2E cluster cleaned"

# Help
help:
	@echo "KubeAdapt Upgrader Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  all             - Tidy modules and build (default)"
	@echo "  build           - Build for Linux (container use)"
	@echo "  build-local     - Build for local platform"
	@echo "  clean           - Remove build artifacts"
	@echo "  test            - Run tests"
	@echo "  test-coverage   - Run tests with coverage report"
	@echo "  lint            - Run golangci-lint"
	@echo "  tidy            - Run go mod tidy"
	@echo "  deps            - Download dependencies"
	@echo "  docker-build    - Show docker build command"
	@echo "  verify          - Verify build compiles"
	@echo "  build-e2e-images - Build E2E test Docker images"
	@echo "  test-e2e        - Build images and run E2E tests"
	@echo "  test-e2e-keep   - Run E2E tests (keep cluster for debugging)"
	@echo "  clean-e2e       - Delete E2E Kind cluster"
	@echo "  help            - Show this help"
