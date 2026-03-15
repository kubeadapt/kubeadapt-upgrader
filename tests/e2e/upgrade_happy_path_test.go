//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeadapt/kubeadapt-upgrader/tests/e2e/helpers"
)

// TestHappyPathUpgrade exercises the full upgrade lifecycle:
// install v0.17.0 → upgrader detects update_available → creates Helm Job → upgrades to v0.18.0 → reports success.
func TestHappyPathUpgrade(t *testing.T) {
	ctx := context.Background()

	// Step 1: Configure stub to advertise an available upgrade.
	stub := helpers.NewStubClient(stubBaseURL)
	if err := stub.Flush(); err != nil {
		t.Fatalf("flushing stub: %v", err)
	}
	if err := helpers.CleanupJobs(ctx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader"); err != nil {
		t.Fatalf("cleaning up stale jobs: %v", err)
	}
	// Delete stale lock ConfigMap if it exists from a previous test (ignore not-found error).
	_ = tc.Clientset().CoreV1().ConfigMaps(TestNamespace).Delete(ctx, "kubeadapt-upgrade-lock", metav1.DeleteOptions{})
	if err := stub.SetMode("update_available"); err != nil {
		t.Fatalf("setting stub mode: %v", err)
	}

	// Step 2: Install the initial chart version with auto-upgrade enabled.
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
		t.Fatalf("installing initial chart %s: %v", InitialChartVersion, err)
	}

	// Step 3: Ensure cleanup regardless of test outcome.
	defer func() {
		if err := helpers.UninstallChart(kubeConfigFile, TestNamespace, HelmReleaseName); err != nil {
			t.Logf("warning: uninstalling chart: %v", err)
		}
	}()

	// Step 4: Wait for the upgrade Job created by kubeadapt-upgrader.
	jobName, succeeded, err := helpers.WaitForJob(ctx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader", 5*time.Minute)
	if err != nil {
		t.Fatalf("waiting for upgrade job: %v", err)
	}

	// Step 5: Assert the Job succeeded.
	if !succeeded {
		t.Errorf("upgrade job %q did not succeed", jobName)
	}

	// Step 6: Assert the stub received at least one check request from the upgrader.
	checks, err := stub.GetCheckRequests()
	if err != nil {
		t.Fatalf("getting check requests from stub: %v", err)
	}
	if len(checks) < 1 {
		t.Errorf("expected at least 1 check request, got %d", len(checks))
	}

	// Step 7: Assert the stub received a success report.
	if err := stub.WaitForReports(1, 2*time.Minute); err != nil {
		t.Fatalf("waiting for upgrade report: %v", err)
	}
	reports, err := stub.GetReportRequests()
	if err != nil {
		t.Fatalf("getting report requests from stub: %v", err)
	}
	foundSuccess := false
	for _, r := range reports {
		if r.Status == "success" {
			foundSuccess = true
			break
		}
	}
	if !foundSuccess {
		t.Errorf("expected at least one report with status=success, got reports: %+v", reports)
	}

	// Step 8: Assert the Helm release was upgraded to the target version.
	releasedVersion, err := helpers.GetReleaseVersion(kubeConfigFile, TestNamespace, HelmReleaseName)
	if err != nil {
		t.Fatalf("getting release version: %v", err)
	}
	if releasedVersion != TargetChartVersion {
		t.Errorf("expected release version %q, got %q", TargetChartVersion, releasedVersion)
	}
}
