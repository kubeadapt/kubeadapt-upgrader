# Service Documentation

## Overview

This is a public Go microservice running on the KubeAdapt platform.

## Development

### Prerequisites

- Go 1.26+
- Docker

### Running Locally

```bash
go run ./cmd/server
```

### Running Tests

```bash
go test ./...
```

## Deployment

This service is published to ECR Public on merge to `main`.

- **Build**: GitHub Actions builds multi-arch images (amd64/arm64) and pushes to ECR Public
- **Registry**: Public ECR (us-east-1)
