# kubeadapt-upgrader

![Go](https://img.shields.io/badge/Go-1.26-blue)

In-cluster Helm upgrade operator for the [Kubeadapt](https://kubeadapt.io) platform.

- Polls the Kubeadapt backend for available Helm chart updates and applies them automatically
- Supports upgrade policies (minor, patch, all) and channels (stable, fast) with dry-run mode
- Uses distributed locking to prevent concurrent upgrades across multiple pods
- Multi-cloud aware: detects AWS, GCP, and Azure at startup for platform-specific upgrade paths

## Quick Start

Install via Helm from the [kubeadapt-helm](https://github.com/kubeadapt/kubeadapt-helm) repository:

```bash
helm repo add kubeadapt https://kubeadapt.github.io/kubeadapt-helm
helm install kubeadapt kubeadapt/kubeadapt \
  --set agent.apiKey=<KUBEADAPT_API_KEY> \
  --set upgrader.enabled=true
```

The upgrader is deployed as part of the Kubeadapt Helm chart. Set `upgrader.enabled=true` and configure your upgrade policy through Helm values.

## Documentation

Full documentation is in the `docs/` directory:

- [Overview](docs/index.md) - what the upgrader does, architecture, and upgrade lifecycle

## Development

```bash
make build       # compile binary to build/upgrader (linux/amd64)
make build-local # compile for local platform
make test        # run unit tests with race detector
make lint        # run golangci-lint
make verify      # verify build compiles
```

E2E tests require Docker and Kind:

```bash
make test-e2e
```

## License

Apache 2.0. See [LICENSE](LICENSE).
