//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/kubeadapt/kubeadapt-upgrader/tests/e2e/helpers"
)

// TestDryRun verifies that dry-run mode detects updates but does NOT create a Job.
func TestDryRun(t *testing.T) {
	stub := helpers.NewStubClient(stubBaseURL)

	// Flush previous requests and set mode to update_available
	if err := stub.Flush(); err != nil {
		t.Fatalf("flush stub: %v", err)
	}
	ctx := context.Background()
	if err := helpers.CleanupJobs(ctx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader"); err != nil {
		t.Fatalf("cleaning up stale jobs: %v", err)
	}
	if err := stub.SetMode("update_available"); err != nil {
		t.Fatalf("set stub mode to update_available: %v", err)
	}

	// Install chart with dry-run enabled
	values := map[string]string{
		// Use locally-loaded images (not private ECR defaults)
		"agent.image.repository":             "localhost/upgrade-stub",
		"agent.image.tag":                    "e2e-test",
		"agent.image.pullPolicy":             "Never",
		"agent.autoUpgrade.image.repository": "localhost/kubeadapt-upgrader",
		"agent.autoUpgrade.image.tag":        "e2e-test",
		"agent.autoUpgrade.image.pullPolicy": "Never",
		// Backend URL pointing to stub
		"agent.config.backendUrl": "http://upgrade-stub.kubeadapt-system.svc:8080",
		"agent.config.token":      "test-token",
		// Auto-upgrade config
		"agent.autoUpgrade.enabled":      "true",
		"agent.autoUpgrade.dryRun":       "true",
		"agent.autoUpgrade.chartRepo":    "http://chartmuseum.kubeadapt-system.svc:8080",
		"agent.autoUpgrade.initialDelay": "5s",
	}
	if err := helpers.InstallChart(kubeConfigFile, TestNamespace, HelmReleaseName, chartmuseumURL, InitialChartVersion, values); err != nil {
		t.Fatalf("install chart: %v", err)
	}
	defer func() {
		_ = helpers.UninstallChart(kubeConfigFile, TestNamespace, HelmReleaseName)
	}()

	// Wait for initial delay + check cycle + buffer
	time.Sleep(20 * time.Second)

	// Assert: check requests received (upgrader DID check for updates)
	checkRequests, err := stub.GetCheckRequests()
	if err != nil {
		t.Fatalf("get check requests: %v", err)
	}
	if len(checkRequests) < 1 {
		t.Errorf("expected at least 1 check request, got %d", len(checkRequests))
	}

	// Assert: NO Job created (dry-run prevents Job creation)
	noJobCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := helpers.WaitForNoJob(noJobCtx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader", 30*time.Second); err != nil {
		t.Errorf("expected no job in dry-run mode: %v", err)
	}

	// Assert: Helm release still at InitialChartVersion
	version, err := helpers.GetReleaseVersion(kubeConfigFile, TestNamespace, HelmReleaseName)
	if err != nil {
		t.Fatalf("get release version: %v", err)
	}
	if version != InitialChartVersion {
		t.Errorf("expected release version %s, got %s", InitialChartVersion, version)
	}

	// Assert: NO reports sent (dry-run does not report completion)
	reports, err := stub.GetReportRequests()
	if err != nil {
		t.Fatalf("get report requests: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("expected 0 report requests in dry-run mode, got %d", len(reports))
	}
}
