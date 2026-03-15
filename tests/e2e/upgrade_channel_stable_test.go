//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeadapt/kubeadapt-upgrader/tests/e2e/helpers"
)

// TestChannelStableNoUpdate verifies that the upgrader does NOT upgrade when configured with
// channel=stable and the stub is in channel_aware mode (stable channel → UpdateAvailable=false,
// simulating that only fast releases exist, no stable release yet). We assert:
// - Check requests are received (upgrader is polling)
// - Check request includes channel="stable"
// - No Job is created (no upgrade attempted)
// - Helm release remains at InitialChartVersion
// - No reports are sent (no upgrade status to report)
func TestChannelStableNoUpdate(t *testing.T) {
	stub := helpers.NewStubClient(stubBaseURL)

	// Flush any prior state and set stub to channel_aware mode
	if err := stub.Flush(); err != nil {
		t.Fatalf("flush stub: %v", err)
	}
	ctx := context.Background()
	if err := helpers.CleanupJobs(ctx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader"); err != nil {
		t.Fatalf("cleaning up stale jobs: %v", err)
	}
	// Wait for any previous upgrader (agent) pods from prior tests to fully terminate.
	// Without this, a late success report from a previous test's pod can leak
	// into this test's stub state after the Flush above.
	{
		podCtx, podCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer podCancel()
		if err := helpers.WaitForPodsGone(podCtx, tc.Clientset(), TestNamespace, "app.kubernetes.io/component=agent", 90*time.Second); err != nil {
			t.Logf("warning: previous agent pods may still be running: %v", err)
		}
	}
	// Delete stale lock ConfigMap if it exists from a previous test (ignore not-found error).
	_ = tc.Clientset().CoreV1().ConfigMaps(TestNamespace).Delete(ctx, "kubeadapt-upgrade-lock", metav1.DeleteOptions{})
	if err := stub.SetMode("channel_aware"); err != nil {
		t.Fatalf("set stub mode to channel_aware: %v", err)
	}

	// Install chart with auto-upgrade enabled, channel set to "stable"
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
		// Channel selection — stable channel has no release yet in channel_aware mode
		"agent.autoUpgrade.channel": "stable",
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

	// Assert the first check request includes channel="stable"
	var checkReq struct {
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal(checkRequests[0], &checkReq); err != nil {
		t.Fatalf("unmarshal check request: %v", err)
	}
	if checkReq.Channel != "stable" {
		t.Errorf("expected channel %q in check request, got %q", "stable", checkReq.Channel)
	}

	// Assert NO Job was created (channel_aware mode: stable → UpdateAvailable=false, no upgrade attempted)
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
