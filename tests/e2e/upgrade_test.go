//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kubeadapt/kubeadapt-upgrader/tests/e2e/cluster"
	"github.com/kubeadapt/kubeadapt-upgrader/tests/e2e/helpers"
)

const (
	ClusterName              = "kubeadapt-upgrader-e2e"
	TestNamespace            = "kubeadapt-system"
	StubNodePort             = "30082"
	ChartmuseumNodePort      = "30083"
	InitialChartVersion      = "0.17.0"
	IntermediateChartVersion = "0.17.1"
	TargetChartVersion       = "0.18.0"
	HelmReleaseName          = "kubeadapt-test"
	UpgraderImage            = "localhost/kubeadapt-upgrader:e2e-test"
	StubImage                = "localhost/upgrade-stub:e2e-test"
	ChartmuseumImage         = "ghcr.io/helm/chartmuseum:v0.16.2"
	HelmJobImage             = "alpine/helm:3.14.3"
)

var (
	tc             *cluster.TestCluster
	kubeConfigFile string
	stubBaseURL    string
	chartmuseumURL string
	// repoRoot is the absolute path to the workspace root (for finding chart source)
	repoRoot string
)

// TestMain is the entry point for the E2E test suite.
// It delegates to runMain so that deferred cleanup runs on all exit paths.
func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

// runMain sets up the Kind cluster and test infrastructure, runs the tests,
// and tears down the cluster. Using a return value instead of os.Exit ensures
// all defer statements execute before the process exits.
func runMain(m *testing.M) int {
	// Find repo root: go up from tests/e2e/ to the module root, then workspace root
	_, filename, _, _ := runtime.Caller(0)
	// filename = .../active/kubeadapt-upgrader/tests/e2e/upgrade_test.go
	// repoRoot = 4 levels up: tests/e2e → tests → kubeadapt-upgrader → active → workspace
	repoRoot = filepath.Join(filepath.Dir(filename), "..", "..", "..", "..")

	var err error
	tc, err = cluster.NewTestCluster(ClusterName, filepath.Join(filepath.Dir(filename), "testdata", "kind-config.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Kind cluster: %v\n", err)
		return 1
	}

	// Teardown unless E2E_SKIP_CLEANUP=1 — this defer WILL run because we return, not os.Exit
	defer func() {
		if os.Getenv("E2E_SKIP_CLEANUP") != "1" {
			_ = tc.Teardown()
		}
	}()

	// Load local Docker images into cluster.
	// Pre-load all images to avoid pull timeouts in CI.
	for _, img := range []string{UpgraderImage, StubImage, ChartmuseumImage, HelmJobImage} {
		if err := tc.LoadImage(img); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load image %s: %v\n", img, err)
			return 1
		}
	}

	kubeConfigFile = tc.KubeConfig
	stubBaseURL = fmt.Sprintf("http://localhost:%s", StubNodePort)
	chartmuseumURL = fmt.Sprintf("http://localhost:%s", ChartmuseumNodePort)

	// Deploy infrastructure (namespace, chartmuseum, stub)
	testdataDir := filepath.Join(filepath.Dir(filename), "testdata")
	if err := cluster.DeployInfrastructure(tc, testdataDir); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to deploy infrastructure: %v\n", err)
		return 1
	}

	// Package and upload chart versions
	chartSourceDir := filepath.Join(repoRoot, "platform", "kubeadapt-helm", "charts", "kubeadapt")
	chartsOutputDir, err := os.MkdirTemp("", "e2e-charts-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create charts temp dir: %v\n", err)
		return 1
	}
	defer os.RemoveAll(chartsOutputDir)

	for _, version := range []string{InitialChartVersion, IntermediateChartVersion, TargetChartVersion} {
		chartPath, err := helpers.PackageChart(chartSourceDir, chartsOutputDir, version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to package chart %s: %v\n", version, err)
			return 1
		}
		if err := helpers.UploadToChartmuseum(chartPath, chartmuseumURL); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to upload chart %s: %v\n", version, err)
			return 1
		}
	}

	return m.Run()
}
