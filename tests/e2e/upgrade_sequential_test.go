//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeadapt/kubeadapt-upgrader/tests/e2e/helpers"
)

// TestSequentialMultiHopUpgrade verifies that the upgrader executes a 3-version
// sequential upgrade path (0.17.0 → 0.17.1 → 0.18.0) without manual intervention.
// The stub returns dynamic responses based on CurrentVersion, simulating the
// ingestion-api's sequential path computation.
func TestSequentialMultiHopUpgrade(t *testing.T) {
	ctx := context.Background()

	// Step 1: Configure stub in sequential_upgrade mode and clean state.
	stub := helpers.NewStubClient(stubBaseURL)
	if err := stub.Flush(); err != nil {
		t.Fatalf("flushing stub: %v", err)
	}
	if err := helpers.CleanupJobs(ctx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader"); err != nil {
		t.Fatalf("cleaning up stale jobs: %v", err)
	}
	// Wait for any previous upgrader pods to fully terminate.
	{
		podCtx, podCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer podCancel()
		if err := helpers.WaitForPodsGone(podCtx, tc.Clientset(), TestNamespace, "app.kubernetes.io/component=agent", 90*time.Second); err != nil {
			t.Logf("warning: previous agent pods may still be running: %v", err)
		}
	}
	// Delete stale lock ConfigMap.
	_ = tc.Clientset().CoreV1().ConfigMaps(TestNamespace).Delete(ctx, "kubeadapt-upgrade-lock", metav1.DeleteOptions{})
	if err := stub.SetMode("sequential_upgrade"); err != nil {
		t.Fatalf("setting stub mode: %v", err)
	}

	// Step 2: Install at 0.17.0 with auto-upgrade enabled.
	values := map[string]string{
		"agent.image.repository":             "localhost/upgrade-stub",
		"agent.image.tag":                    "e2e-test",
		"agent.image.pullPolicy":             "Never",
		"agent.autoUpgrade.image.repository": "localhost/kubeadapt-upgrader",
		"agent.autoUpgrade.image.tag":        "e2e-test",
		"agent.autoUpgrade.image.pullPolicy": "Never",
		"agent.config.backendUrl":            "http://upgrade-stub.kubeadapt-system.svc:8080",
		"agent.config.token":                 "test-token",
		"agent.autoUpgrade.enabled":          "true",
		"agent.autoUpgrade.chartRepo":        "http://chartmuseum.kubeadapt-system.svc:8080",
		"agent.autoUpgrade.initialDelay":     "5s",
	}
	if err := helpers.InstallChart(kubeConfigFile, TestNamespace, HelmReleaseName, chartmuseumURL, InitialChartVersion, values); err != nil {
		t.Fatalf("installing initial chart %s: %v", InitialChartVersion, err)
	}

	// Step 3: Cleanup on exit.
	defer func() {
		if err := helpers.UninstallChart(kubeConfigFile, TestNamespace, HelmReleaseName); err != nil {
			t.Logf("warning: uninstalling chart: %v", err)
		}
	}()

	// Step 4: Wait for 2 success reports (hop1: 0.17.0→0.17.1, hop2: 0.17.1→0.18.0).
	// Each hop: upgrader pod starts, checks API, gets next hop, creates Helm job, upgrades,
	// reports success, new pod starts at new version, repeats.
	// Timeout: 15 minutes for 2 full upgrade cycles.
	if err := stub.WaitForReports(2, 15*time.Minute); err != nil {
		t.Fatalf("waiting for 2 sequential upgrade reports: %v", err)
	}

	// Step 5: Assert both hops reported success.
	reports, err := stub.GetReportRequests()
	if err != nil {
		t.Fatalf("getting report requests: %v", err)
	}

	var hop1Found, hop2Found bool
	for _, r := range reports {
		if r.Status == "success" && r.FromVersion == InitialChartVersion && r.ToVersion == IntermediateChartVersion {
			hop1Found = true
		}
		if r.Status == "success" && r.FromVersion == IntermediateChartVersion && r.ToVersion == TargetChartVersion {
			hop2Found = true
		}
	}
	if !hop1Found {
		t.Errorf("expected success report for hop %s→%s, got reports: %+v", InitialChartVersion, IntermediateChartVersion, reports)
	}
	if !hop2Found {
		t.Errorf("expected success report for hop %s→%s, got reports: %+v", IntermediateChartVersion, TargetChartVersion, reports)
	}

	// Step 6: Assert final Helm release version is 0.18.0.
	releasedVersion, err := helpers.GetReleaseVersion(kubeConfigFile, TestNamespace, HelmReleaseName)
	if err != nil {
		t.Fatalf("getting release version: %v", err)
	}
	if releasedVersion != TargetChartVersion {
		t.Errorf("expected final release version %q, got %q", TargetChartVersion, releasedVersion)
	}
}
