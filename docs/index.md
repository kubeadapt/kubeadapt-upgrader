# kubeadapt-upgrader

> This documentation is public so you can see exactly what runs inside your cluster. It is maintained internally as part of our development process. While we don't expect external contributions, we welcome any feedback or pull requests.

**In-cluster Helm upgrade operator** for the Kubeadapt platform. Runs inside your cluster, polls the Kubeadapt backend for available chart updates, and applies Helm upgrades automatically with rollback safety.

## What It Does

kubeadapt-upgrader is a single Go binary deployed as a Deployment in your Kubernetes cluster. On a configurable interval it:

1. Polls the Kubeadapt backend API for available Helm chart updates
2. Selects the target version based on your upgrade policy, channel, and the backend's recommended upgrade path
3. Acquires a distributed lock (Kubernetes ConfigMap) to prevent concurrent upgrades
4. Creates a Kubernetes Job that runs `helm upgrade --atomic --reuse-values` with the target version
5. Waits for the Job to complete, detects rollbacks if the upgrade fails, and reports the result back to the platform

The upgrader does not run Helm in-process. Instead it spawns a short-lived Job with the `alpine/helm` image, inheriting the existing release values. The `--atomic` flag ensures that failed upgrades are automatically rolled back by Helm.

## Key Features

- **Configurable upgrade policies** - choose `minor`, `patch`, or `all` to control which versions are applied
- **Release channels** - `stable` (default) for tested releases, `fast` for early access
- **Dry-run mode** - log what would be upgraded without making changes
- **Multi-hop upgrade paths** - the backend can return a sequential path (e.g. v0.35.0 to v0.35.2 to v0.36.0), and the upgrader executes one hop per cycle
- **Distributed locking** - a ConfigMap-based lock with 30-minute TTL prevents concurrent upgrades and handles pod crashes
- **Automatic rollback detection** - after a failed upgrade, the upgrader checks Helm release history to confirm whether `--atomic` rolled back successfully
- **Multi-cloud aware** - detects AWS (EKS), GCP (GKE), Azure (AKS), or on-premise at startup via instance metadata endpoints
- **Graceful shutdown** - critical operations (Job wait, lock release, status report) use a background context so they complete even when the pod receives SIGTERM during a self-upgrade

## Architecture Overview

```
Your Kubernetes Cluster
+---------------------------------------------------------+
|                                                         |
|  kubeadapt-upgrader (Deployment)                        |
|  +---------------------------------------------------+ |
|  |  Config: policy, channel, interval, dry-run        | |
|  |                                                    | |
|  |  Check Loop (configurable interval, default 6h)    | |
|  |  -> Backend API: POST /api/v1/updates/check        | |
|  |  -> Version Selection (multi-hop path support)     | |
|  |  -> Distributed Lock (ConfigMap)                   | |
|  |  -> Helm Upgrade Job (alpine/helm container)       | |
|  |  -> Rollback Detection (Helm history)              | |
|  |  -> Status Report: POST /api/v1/updates/report     | |
|  +------------------------+---------------------------+ |
|                           |                             |
|  kubeadapt-upgrade-*      | creates                     |
|  (Job, short-lived)       v                             |
|  +---------------------------------------------------+ |
|  |  helm upgrade --install --atomic --wait            | |
|  |    --reuse-values --timeout 15m --version X.Y.Z    | |
|  |    kubeadapt oci://ghcr.io/kubeadapt/...           | |
|  +---------------------------------------------------+ |
|                           | HTTPS                       |
+---------------------------+-----------------------------+
                            |
                            v
                Kubeadapt Platform API
```

At startup the upgrader detects your cloud platform (EKS, AKS, GKE, or on-premise) and includes this in update checks so the backend can provide platform-specific upgrade guidance.

## Configuration

All configuration is via environment variables. The Helm chart sets these through its values file.

### Required

| Variable | Description |
|----------|-------------|
| `KUBEADAPT_BACKEND_API_ENDPOINT` | URL of the Kubeadapt backend API |
| `KUBEADAPT_AGENT_TOKEN` | Authentication token (Bearer) |
| `POD_NAME` | Pod name, set via Downward API |
| `POD_NAMESPACE` | Pod namespace, set via Downward API |

### Optional

| Variable | Default | Description |
|----------|---------|-------------|
| `KUBEADAPT_UPGRADE_ENABLED` | `false` | Enable the auto-upgrade check loop |
| `KUBEADAPT_UPGRADE_CHECK_INTERVAL` | `6h` | How often to check for updates |
| `KUBEADAPT_UPGRADE_POLICY` | `minor` | Version policy: `minor`, `patch`, or `all` |
| `KUBEADAPT_UPGRADE_CHANNEL` | `stable` | Release channel: `stable` or `fast` |
| `KUBEADAPT_UPGRADE_DRY_RUN` | `false` | Log upgrades without applying them |
| `KUBEADAPT_UPGRADE_TIMEOUT` | `15m` | Helm upgrade timeout |
| `KUBEADAPT_UPGRADE_INITIAL_DELAY` | `1m` | Delay before the first check after startup |
| `KUBEADAPT_UPGRADE_JOB_IMAGE` | `alpine/helm:3.14.3` | Container image for the Helm upgrade Job |
| `KUBEADAPT_UPGRADE_CHART_REPO` | `oci://ghcr.io/kubeadapt/kubeadapt-helm/kubeadapt` | Helm chart repository URL |
| `HELM_RELEASE_NAME` | `kubeadapt` | Name of the Helm release to upgrade |
| `KUBEADAPT_CHART_VERSION` | (empty) | Current chart version, used for update comparison |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

## Quick Start

Install via the official Helm chart at [github.com/kubeadapt/kubeadapt-helm](https://github.com/kubeadapt/kubeadapt-helm).

```bash
helm repo add kubeadapt https://kubeadapt.github.io/kubeadapt-helm
helm repo update

helm install kubeadapt kubeadapt/kubeadapt \
  --namespace kubeadapt \
  --create-namespace \
  --set agent.apiKey=<YOUR_API_KEY> \
  --set upgrader.enabled=true \
  --set upgrader.upgradePolicy=minor \
  --set upgrader.upgradeChannel=stable
```

The Helm chart handles RBAC, ServiceAccount, and all required permissions. See the [kubeadapt-helm repository](https://github.com/kubeadapt/kubeadapt-helm) for the full values reference.

## Startup Behavior

When the upgrader starts, it logs its configuration and detected platform:

```
kubeadapt-upgrader starting
Configuration loaded  backend_endpoint=https://...  pod_name=kubeadapt-upgrader-xyz  chart_version=0.17.0  upgrade_enabled=true  check_interval=6h0m0s  policy=minor  channel=stable  dry_run=false
Kubernetes client initialized
Platform detected  platform=eks
Upgrader started successfully
Starting upgrader  check_interval=6h0m0s  policy=minor  channel=stable  dry_run=false  enabled=true
Scheduling initial upgrade check  delay=1m0s
```

If `upgrade_enabled=false`, the upgrader logs that auto-upgrade is disabled and does not start the check loop.

## Upgrade Lifecycle

A single upgrade cycle follows these steps:

1. **Check** - POST to `/api/v1/updates/check` with current version, policy, channel, and platform
2. **Select** - Pick the target version from the upgrade path (first hop), recommended version, or latest version
3. **Lock** - Acquire the `kubeadapt-upgrade-lock` ConfigMap. If another pod holds it, skip this cycle
4. **Upgrade** - Create a Kubernetes Job running `helm upgrade --atomic --wait --reuse-values`
5. **Wait** - Poll Job status every 5 seconds until success or failure
6. **Detect rollback** - If the Job failed, check Helm release history for automatic rollback
7. **Report** - POST to `/api/v1/updates/report` with status (`success`, `failed`, or `rolled_back`)
8. **Release** - Delete the lock ConfigMap

Steps 5, 7, and 8 use a background context so they complete even if the pod receives SIGTERM during a self-upgrade (the new pod starts while the old one finishes reporting).

## Distributed Locking

The upgrader uses a Kubernetes ConfigMap named `kubeadapt-upgrade-lock` to coordinate upgrades:

- Only one pod can hold the lock at a time
- The lock stores the holder's pod name, a timestamp, and the version range being upgraded
- If a pod crashes while holding the lock, the lock expires after 30 minutes and another pod can take over
- Race conditions during lock acquisition are handled via Kubernetes API conflict detection

## What's Next

| Topic | Description |
|-------|-------------|
| [kubeadapt-helm](https://github.com/kubeadapt/kubeadapt-helm) | Helm chart values reference and upgrade instructions |
| [kubeadapt-agent](https://github.com/kubeadapt/kubeadapt-agent) | The metrics collector agent that runs alongside the upgrader |
