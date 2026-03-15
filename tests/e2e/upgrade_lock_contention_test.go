//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeadapt/kubeadapt-upgrader/tests/e2e/helpers"
)

// TestLockContention verifies that the upgrader backs off when a lock is held
// by another holder, and proceeds with the upgrade once the lock is released.
func TestLockContention(t *testing.T) {
	stub := helpers.NewStubClient(stubBaseURL)

	// Reset stub state and set mode to update_available.
	if err := stub.Flush(); err != nil {
		t.Fatalf("flush stub: %v", err)
	}
	if err := stub.SetMode("update_available"); err != nil {
		t.Fatalf("set stub mode: %v", err)
	}

	// Install the upgrader chart at the initial version.
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
		"agent.autoUpgrade.initialDelay":  "5s",
		"agent.autoUpgrade.checkInterval": "30s",
		"agent.autoUpgrade.chartRepo":     "http://chartmuseum.kubeadapt-system.svc:8080",
	}
	if err := helpers.InstallChart(kubeConfigFile, TestNamespace, HelmReleaseName, chartmuseumURL, InitialChartVersion, values); err != nil {
		t.Fatalf("install chart at %s: %v", InitialChartVersion, err)
	}
	defer func() {
		if err := helpers.UninstallChart(kubeConfigFile, TestNamespace, HelmReleaseName); err != nil {
			t.Errorf("uninstall chart: %v", err)
		}
	}()

	ctx := context.Background()

	// Clean up stale jobs and lock ConfigMap from previous test runs.
	if err := helpers.CleanupJobs(ctx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader"); err != nil {
		t.Fatalf("cleaning up stale jobs: %v", err)
	}
	// Delete stale lock ConfigMap if it exists (ignore not-found error).
	_ = tc.Clientset().CoreV1().ConfigMaps(TestNamespace).Delete(ctx, "kubeadapt-upgrade-lock", metav1.DeleteOptions{})

	// Create a fake lock ConfigMap before the upgrader's initial delay fires.
	// The timestamp is set to now so the lock is considered valid (non-expired, TTL=30m).
	lockCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubeadapt-upgrade-lock",
			Namespace: TestNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "kubeadapt",
				"app.kubernetes.io/component": "upgrade-lock",
			},
		},
		Data: map[string]string{
			"holder":       "test-holder-pod",
			"timestamp":    time.Now().UTC().Format(time.RFC3339),
			"from_version": InitialChartVersion,
			"to_version":   TargetChartVersion,
		},
	}
	_, err := tc.Clientset().CoreV1().ConfigMaps(TestNamespace).Create(ctx, lockCM, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create fake lock configmap: %v", err)
	}

	// Wait long enough for: initial delay (5s) + at least one check attempt.
	// The upgrader should detect the lock and back off without creating a Job.
	time.Sleep(20 * time.Second)

	// Assert that no upgrade Job was created while the lock is held.
	if err := helpers.WaitForNoJob(ctx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader", 10*time.Second); err != nil {
		t.Fatalf("upgrader created a job despite lock being held: %v", err)
	}

	// Assert the Helm release is still at the initial version — no upgrade happened.
	version, err := helpers.GetReleaseVersion(kubeConfigFile, TestNamespace, HelmReleaseName)
	if err != nil {
		t.Fatalf("get release version: %v", err)
	}
	if version != InitialChartVersion {
		t.Errorf("expected release at %s while lock held, got %s", InitialChartVersion, version)
	}

	// Release the fake lock by deleting the ConfigMap.
	if err := tc.Clientset().CoreV1().ConfigMaps(TestNamespace).Delete(ctx, "kubeadapt-upgrade-lock", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete fake lock configmap: %v", err)
	}

	// After the lock is released the upgrader should detect the update on the next
	// check cycle (checkInterval=30s) and create an upgrade Job.
	jobName, succeeded, err := helpers.WaitForJob(ctx, tc.Clientset(), TestNamespace, "app.kubernetes.io/managed-by=kubeadapt-upgrader", 5*time.Minute)
	if err != nil {
		t.Fatalf("waiting for upgrade job after lock release: %v", err)
	}
	if !succeeded {
		t.Errorf("upgrade job %q did not succeed", jobName)
	}

	// Assert that the upgrader reported success back to the stub.
	// Wait for reports — "in_progress" may arrive first before "success".
	// Poll until we see a "success" report or timeout.
	if err := stub.WaitForReports(1, 2*time.Minute); err != nil {
		t.Fatalf("waiting for reports: %v", err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	var foundSuccess bool
	for time.Now().Before(deadline) && !foundSuccess {
		reports, err := stub.GetReportRequests()
		if err != nil {
			t.Fatalf("get report requests: %v", err)
		}
		for _, r := range reports {
			if r.Status == "success" {
				foundSuccess = true
				break
			}
		}
		if !foundSuccess {
			time.Sleep(3 * time.Second)
		}
	}
	if !foundSuccess {
		reports, _ := stub.GetReportRequests()
		t.Errorf("expected at least one report with status=success, got reports: %+v", reports)
	}

	// Assert the Helm release has been upgraded to the target version.
	version, err = helpers.GetReleaseVersion(kubeConfigFile, TestNamespace, HelmReleaseName)
	if err != nil {
		t.Fatalf("get release version after upgrade: %v", err)
	}
	if version != TargetChartVersion {
		t.Errorf("expected release at %s after upgrade, got %s", TargetChartVersion, version)
	}

}
