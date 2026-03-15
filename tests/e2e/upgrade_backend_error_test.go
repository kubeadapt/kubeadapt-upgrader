//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/kubeadapt/kubeadapt-upgrader/tests/e2e/helpers"
)

// TestBackendErrorRecovery verifies that the upgrader handles backend 500 errors
// gracefully and recovers when the backend becomes healthy.
//
// Flow:
//  1. Stub returns 500 on check requests — upgrader logs error and retries on next tick.
//  2. No upgrade Job should be created while the backend is unhealthy.
//  3. Stub switches to "update_available" — upgrader picks up the update on the next tick.
//  4. Upgrade Job completes, success report sent, Helm release at TargetChartVersion.
func TestBackendErrorRecovery(t *testing.T) {
	ctx := context.Background()

	// Step 1: Configure stub to return 500 errors on check requests.
	stub := helpers.NewStubClient(stubBaseURL)
	if err := stub.Flush(); err != nil {
		t.Fatalf("flushing stub: %v", err)
	}
	if err := helpers.CleanupJobs(ctx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader"); err != nil {
		t.Fatalf("cleaning up stale jobs: %v", err)
	}
	if err := stub.SetMode("error_500"); err != nil {
		t.Fatalf("setting stub mode to error_500: %v", err)
	}

	// Step 2: Install the initial chart version with auto-upgrade enabled and a short check interval.
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
		"agent.autoUpgrade.enabled":       "true",
		"agent.autoUpgrade.chartRepo":     "http://chartmuseum.kubeadapt-system.svc:8080",
		"agent.autoUpgrade.initialDelay":  "5s",
		"agent.autoUpgrade.checkInterval": "30s",
	}
	if err := helpers.InstallChart(kubeConfigFile, TestNamespace, HelmReleaseName, chartmuseumURL, InitialChartVersion, values); err != nil {
		t.Fatalf("installing initial chart %s: %v", InitialChartVersion, err)
	}

	// Step 3: Ensure cleanup regardless of test outcome.
	defer func() {
		if err := helpers.UninstallChart(kubeConfigFile, TestNamespace, HelmReleaseName); err != nil {
			t.Logf("warning: uninstalling chart: %v", err)
		}
	}()

	// Step 4: Wait for initial delay + first check attempt (should fail with 500).
	// initialDelay=5s + buffer for pod startup and first tick → 20s is sufficient.
	time.Sleep(20 * time.Second)

	// Step 5: Assert at least one check request was received (even though it returned 500).
	checks, err := stub.GetCheckRequests()
	if err != nil {
		t.Fatalf("getting check requests from stub: %v", err)
	}
	if len(checks) < 1 {
		t.Errorf("expected at least 1 check request during error phase, got %d", len(checks))
	}

	// Step 6: Assert NO upgrade Job was created while backend returned errors.
	// The upgrader's runCheck() on HTTP error just logs and returns — no Job created.
	err = helpers.WaitForNoJob(ctx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader", 10*time.Second)
	if err != nil {
		t.Fatalf("expected no upgrade job during error phase, but found one: %v", err)
	}

	// Step 7: Switch stub to advertise an available upgrade — upgrader should recover on next tick.
	if err := stub.SetMode("update_available"); err != nil {
		t.Fatalf("setting stub mode to update_available: %v", err)
	}

	// Step 8: Wait for the upgrade Job to appear and complete.
	jobName, succeeded, err := helpers.WaitForJob(ctx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader", 5*time.Minute)
	if err != nil {
		t.Fatalf("waiting for upgrade job after recovery: %v", err)
	}

	// Step 9: Assert the Job succeeded.
	if !succeeded {
		t.Errorf("upgrade job %q did not succeed after backend recovery", jobName)
	}

	// Step 10: Wait for a success report specifically.
	// The upgrader sends an in_progress report first, then a success report after the Job
	// completes. We must poll until we see status=success, not just any report.
	successDeadline := time.Now().Add(2 * time.Minute)
	var foundSuccess bool
	for time.Now().Before(successDeadline) {
		reports, err := stub.GetReportRequests()
		if err != nil {
			t.Fatalf("getting report requests from stub: %v", err)
		}
		for _, r := range reports {
			if r.Status == "success" {
				foundSuccess = true
				break
			}
		}
		if foundSuccess {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !foundSuccess {
		reports, _ := stub.GetReportRequests()
		t.Errorf("expected at least one report with status=success, got reports: %+v", reports)
	}

	// Step 11: Assert the Helm release was upgraded to the target version.
	releasedVersion, err := helpers.GetReleaseVersion(kubeConfigFile, TestNamespace, HelmReleaseName)
	if err != nil {
		t.Fatalf("getting release version: %v", err)
	}
	if releasedVersion != TargetChartVersion {
		t.Errorf("expected release version %q, got %q", TargetChartVersion, releasedVersion)
	}
}
