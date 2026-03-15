//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/kubeadapt/kubeadapt-upgrader/tests/e2e/helpers"
)

// TestBlockedVersion verifies that the upgrader does NOT upgrade when the target version is blocked.
// The stub is configured in "blocked" mode which returns UpdateAvailable: false with
// BlockedVersions containing the target version. Since UpdateAvailable is false, the upgrader
// returns early at the "No update available" check and does not attempt an upgrade. We assert:
// - Check requests are received (upgrader is polling)
// - No Job is created (no upgrade attempted)
// - Helm release remains at InitialChartVersion
// - No reports are sent (no upgrade status to report)
func TestBlockedVersion(t *testing.T) {
	stub := helpers.NewStubClient(stubBaseURL)

	// Flush any prior state and set stub to "blocked" mode
	if err := stub.Flush(); err != nil {
		t.Fatalf("flush stub: %v", err)
	}
	ctx := context.Background()
	if err := helpers.CleanupJobs(ctx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader"); err != nil {
		t.Fatalf("cleaning up stale jobs: %v", err)
	}
	// Wait for any previous upgrader (agent) pods from prior tests to fully terminate.
	// Without this, a late success report from TestBackendErrorRecovery's pod can leak
	// into this test's stub state after the Flush above.
	{
		podCtx, podCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer podCancel()
		if err := helpers.WaitForPodsGone(podCtx, tc.Clientset(), TestNamespace, "app.kubernetes.io/component=agent", 90*time.Second); err != nil {
			t.Logf("warning: previous agent pods may still be running: %v", err)
		}
	}
	if err := stub.SetMode("blocked"); err != nil {
		t.Fatalf("set stub mode to blocked: %v", err)
	}

	// Install chart with auto-upgrade enabled
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
		"agent.autoUpgrade.chartRepo":    "http://chartmuseum.kubeadapt-system.svc:8080",
		"agent.autoUpgrade.initialDelay": "5s",
	}
	if err := helpers.InstallChart(kubeConfigFile, TestNamespace, HelmReleaseName, chartmuseumURL, InitialChartVersion, values); err != nil {
		t.Fatalf("install chart: %v", err)
	}
	defer func() {
		_ = helpers.UninstallChart(kubeConfigFile, TestNamespace, HelmReleaseName)
	}()

	// Wait for initial delay (5s) + check cycle + buffer
	time.Sleep(20 * time.Second)

	// Assert check requests were received (upgrader is polling)
	checkRequests, err := stub.GetCheckRequests()
	if err != nil {
		t.Fatalf("get check requests: %v", err)
	}
	if len(checkRequests) < 1 {
		t.Errorf("expected at least 1 check request, got %d", len(checkRequests))
	}

	// Assert NO Job was created (blocked version — no upgrade attempted)
	noJobCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := helpers.WaitForNoJob(noJobCtx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader", 30*time.Second); err != nil {
		t.Errorf("expected no job to be created: %v", err)
	}

	// Assert Helm release is still at InitialChartVersion
	version, err := helpers.GetReleaseVersion(kubeConfigFile, TestNamespace, HelmReleaseName)
	if err != nil {
		t.Fatalf("get release version: %v", err)
	}
	if version != InitialChartVersion {
		t.Errorf("expected release version %q, got %q", InitialChartVersion, version)
	}

	// Assert NO reports were sent (no upgrade status to report)
	reports, err := stub.GetReportRequests()
	if err != nil {
		t.Fatalf("get report requests: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("expected 0 report requests, got %d", len(reports))
	}
}
