# kubeadapt-upgrader

![Go](https://img.shields.io/badge/Go-1.26-blue)

In-cluster Helm upgrade operator for the [Kubeadapt](https://kubeadapt.io) platform.

- Polls the Kubeadapt backend for available Helm chart updates and applies them automatically
- Supports upgrade policies (minor, patch, all) and channels (stable, fast) with dry-run mode
- Uses distributed locking to prevent concurrent upgrades across multiple pods
- Multi-cloud aware: detects EKS, GKE, AKS, and on-premise environments

## Quick Start

Install via Helm from the [kubeadapt-helm](https://github.com/kubeadapt/kubeadapt-helm) repository:

```bash
helm repo add kubeadapt https://kubeadapt.github.io/kubeadapt-helm
helm install kubeadapt kubeadapt/kubeadapt \
  --namespace kubeadapt \
  --create-namespace \
  --set agent.config.token=<YOUR_TOKEN> \
  --set agent.autoUpgrade.enabled=true
```

The upgrader is deployed as a sidecar container in the agent pod. Set `agent.autoUpgrade.enabled=true` and configure your upgrade policy through Helm values.

## Documentation

- [Overview](docs/index.md) - architecture, upgrade lifecycle, and configuration reference
- [Official docs](https://kubeadapt.io/docs/v1/configuration/auto-upgrade/) - auto-upgrade guide

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
