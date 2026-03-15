package helm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCreateUpgradeJob_OCIRepo(t *testing.T) {
	// Test OCI repo detection: should use chart repo as last arg
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	logger := zap.NewNop()

	namespace := "kubeadapt"
	releaseName := "kubeadapt"
	targetVersion := "0.18.0"
	chartRepo := "oci://ghcr.io/kubeadapt/kubeadapt-helm/kubeadapt"
	upgradeTimeout := 10 * time.Minute
	jobImage := "alpine/helm:3.14.3"

	job, err := CreateUpgradeJob(ctx, clientset, namespace, releaseName, targetVersion, chartRepo, upgradeTimeout, jobImage, logger)
	require.NoError(t, err)
	require.NotNil(t, job)

	// Verify the helm command
	command := job.Spec.Template.Spec.Containers[0].Command
	require.NotEmpty(t, command)

	// Expected command for OCI:
	// helm upgrade --install --atomic --wait --reset-then-reuse-values --timeout 10m0s --version 0.18.0 kubeadapt oci://...
	expectedCommand := []string{
		"helm",
		"upgrade",
		"--install",
		"--atomic",
		"--wait",
		"--reset-then-reuse-values",
		"--timeout", "10m0s",
		"--version", "0.18.0",
		"kubeadapt",
		"oci://ghcr.io/kubeadapt/kubeadapt-helm/kubeadapt",
	}

	assert.Equal(t, expectedCommand, command)

	// Verify last two args are releaseName and chartRepo
	assert.Equal(t, releaseName, command[len(command)-2])
	assert.Equal(t, chartRepo, command[len(command)-1])
}

func TestCreateUpgradeJob_HTTPRepo(t *testing.T) {
	// Test HTTP repo detection: should use ChartName + --repo flag
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	logger := zap.NewNop()

	namespace := "kubeadapt"
	releaseName := "kubeadapt"
	targetVersion := "0.18.0"
	chartRepo := "http://chartmuseum:8080"
	upgradeTimeout := 10 * time.Minute
	jobImage := "alpine/helm:3.14.3"

	job, err := CreateUpgradeJob(ctx, clientset, namespace, releaseName, targetVersion, chartRepo, upgradeTimeout, jobImage, logger)
	require.NoError(t, err)
	require.NotNil(t, job)

	// Verify the helm command
	command := job.Spec.Template.Spec.Containers[0].Command
	require.NotEmpty(t, command)

	// Expected command for HTTP:
	// helm upgrade --install --atomic --wait --reset-then-reuse-values --timeout 10m0s --version 0.18.0 kubeadapt kubeadapt --repo http://chartmuseum:8080
	expectedCommand := []string{
		"helm",
		"upgrade",
		"--install",
		"--atomic",
		"--wait",
		"--reset-then-reuse-values",
		"--timeout", "10m0s",
		"--version", "0.18.0",
		"kubeadapt",
		"kubeadapt",
		"--repo",
		"http://chartmuseum:8080",
	}

	assert.Equal(t, expectedCommand, command)

	// Verify the command contains --repo flag and chart repo
	assert.Contains(t, command, "--repo")
	assert.Contains(t, command, chartRepo)

	// Verify ChartName is used as chart reference (second-to-last before --repo)
	repoIndex := -1
	for i, arg := range command {
		if arg == "--repo" {
			repoIndex = i
			break
		}
	}
	require.NotEqual(t, -1, repoIndex, "--repo flag should be present")
	assert.Equal(t, ChartName, command[repoIndex-1])
}

func TestCreateUpgradeJob_HTTPSRepo(t *testing.T) {
	// Test HTTPS repo detection: should also use ChartName + --repo flag
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	logger := zap.NewNop()

	namespace := "kubeadapt"
	releaseName := "kubeadapt"
	targetVersion := "0.18.0"
	chartRepo := "https://charts.example.com"
	upgradeTimeout := 10 * time.Minute
	jobImage := "alpine/helm:3.14.3"

	job, err := CreateUpgradeJob(ctx, clientset, namespace, releaseName, targetVersion, chartRepo, upgradeTimeout, jobImage, logger)
	require.NoError(t, err)
	require.NotNil(t, job)

	// Verify the helm command
	command := job.Spec.Template.Spec.Containers[0].Command
	require.NotEmpty(t, command)

	// Expected command for HTTPS:
	// helm upgrade --install --atomic --wait --reset-then-reuse-values --timeout 10m0s --version 0.18.0 kubeadapt kubeadapt --repo https://charts.example.com
	expectedCommand := []string{
		"helm",
		"upgrade",
		"--install",
		"--atomic",
		"--wait",
		"--reset-then-reuse-values",
		"--timeout", "10m0s",
		"--version", "0.18.0",
		"kubeadapt",
		"kubeadapt",
		"--repo",
		"https://charts.example.com",
	}

	assert.Equal(t, expectedCommand, command)

	// Verify the command contains --repo flag and chart repo
	assert.Contains(t, command, "--repo")
	assert.Contains(t, command, chartRepo)
}

func TestCreateUpgradeJob_JobMetadata(t *testing.T) {
	// Test that job metadata is correctly set (should not change)
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	logger := zap.NewNop()

	namespace := "kubeadapt"
	releaseName := "kubeadapt"
	targetVersion := "0.18.0"
	chartRepo := "oci://ghcr.io/kubeadapt/kubeadapt-helm/kubeadapt"
	upgradeTimeout := 10 * time.Minute
	jobImage := "alpine/helm:3.14.3"

	job, err := CreateUpgradeJob(ctx, clientset, namespace, releaseName, targetVersion, chartRepo, upgradeTimeout, jobImage, logger)
	require.NoError(t, err)
	require.NotNil(t, job)

	// Verify namespace
	assert.Equal(t, namespace, job.Namespace)

	// Verify labels
	assert.Equal(t, "kubeadapt", job.Labels["app.kubernetes.io/name"])
	assert.Equal(t, "upgrader", job.Labels["app.kubernetes.io/component"])
	assert.Equal(t, "kubeadapt-upgrader", job.Labels["app.kubernetes.io/managed-by"])
	assert.Equal(t, targetVersion, job.Labels["kubeadapt.io/upgrade-version"])

	// Verify annotations
	assert.Equal(t, releaseName, job.Annotations["kubeadapt.io/upgrade-from"])
	assert.Equal(t, targetVersion, job.Annotations["kubeadapt.io/upgrade-to"])

	// Verify pod spec
	assert.Equal(t, "kubeadapt-upgrader", job.Spec.Template.Spec.ServiceAccountName)
	assert.Equal(t, 1, len(job.Spec.Template.Spec.Containers))
	assert.Equal(t, "helm-upgrade", job.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, jobImage, job.Spec.Template.Spec.Containers[0].Image)
}
