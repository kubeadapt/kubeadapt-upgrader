package config

import (
	"os"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.UpgradeEnabled != false {
		t.Errorf("expected UpgradeEnabled=false, got %v", cfg.UpgradeEnabled)
	}
	if cfg.UpgradeCheckInterval != 6*time.Hour {
		t.Errorf("expected UpgradeCheckInterval=6h, got %v", cfg.UpgradeCheckInterval)
	}
	if cfg.UpgradePolicy != "minor" {
		t.Errorf("expected UpgradePolicy=minor, got %v", cfg.UpgradePolicy)
	}
	if cfg.UpgradeChannel != "stable" {
		t.Errorf("expected UpgradeChannel=stable, got %v", cfg.UpgradeChannel)
	}
	if cfg.UpgradeDryRun != false {
		t.Errorf("expected UpgradeDryRun=false, got %v", cfg.UpgradeDryRun)
	}
	if cfg.UpgradeTimeout != 15*time.Minute {
		t.Errorf("expected UpgradeTimeout=15m, got %v", cfg.UpgradeTimeout)
	}
	if cfg.UpgradeJobImage != "alpine/helm:3.14.3" {
		t.Errorf("expected UpgradeJobImage=alpine/helm:3.14.3, got %v", cfg.UpgradeJobImage)
	}
	if cfg.ChartRepo != "oci://ghcr.io/kubeadapt/kubeadapt-helm/kubeadapt" {
		t.Errorf("expected ChartRepo=oci://ghcr.io/kubeadapt/kubeadapt-helm/kubeadapt, got %v", cfg.ChartRepo)
	}
	if cfg.UpgradeInitialDelay != 1*time.Minute {
		t.Errorf("expected UpgradeInitialDelay=1m, got %v", cfg.UpgradeInitialDelay)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected LogLevel=info, got %v", cfg.LogLevel)
	}
}

func TestLoadFromEnv_RequiredFields(t *testing.T) {
	// Clear all env vars first
	os.Clearenv()

	_, err := LoadFromEnv()
	if err == nil {
		t.Error("expected error for missing KUBEADAPT_BACKEND_URL")
	}

	os.Setenv("KUBEADAPT_BACKEND_URL", "https://api.kubeadapt.io")

	// Test missing KUBEADAPT_AGENT_TOKEN
	_, err = LoadFromEnv()
	if err == nil {
		t.Error("expected error for missing KUBEADAPT_AGENT_TOKEN")
	}

	// Set KUBEADAPT_AGENT_TOKEN
	os.Setenv("KUBEADAPT_AGENT_TOKEN", "test-token")

	// Test missing POD_NAME
	_, err = LoadFromEnv()
	if err == nil {
		t.Error("expected error for missing POD_NAME")
	}

	// Set POD_NAME
	os.Setenv("POD_NAME", "test-pod")

	// Test missing POD_NAMESPACE
	_, err = LoadFromEnv()
	if err == nil {
		t.Error("expected error for missing POD_NAMESPACE")
	}

	// Set POD_NAMESPACE - now should succeed
	os.Setenv("POD_NAMESPACE", "kubeadapt")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if cfg.BackendAPIEndpoint != "https://api.kubeadapt.io" {
		t.Errorf("expected BackendAPIEndpoint=https://api.kubeadapt.io, got %v", cfg.BackendAPIEndpoint)
	}
	if cfg.AgentToken != "test-token" {
		t.Errorf("expected AgentToken=test-token, got %v", cfg.AgentToken)
	}
	if cfg.PodName != "test-pod" {
		t.Errorf("expected PodName=test-pod, got %v", cfg.PodName)
	}
	if cfg.PodNamespace != "kubeadapt" {
		t.Errorf("expected PodNamespace=kubeadapt, got %v", cfg.PodNamespace)
	}
}

func TestLoadFromEnv_AllFields(t *testing.T) {
	// Set up all environment variables
	os.Clearenv()
	os.Setenv("KUBEADAPT_BACKEND_URL", "https://api.test.io")
	os.Setenv("KUBEADAPT_AGENT_TOKEN", "token123")
	os.Setenv("POD_NAME", "upgrader-pod")
	os.Setenv("POD_NAMESPACE", "test-ns")
	os.Setenv("KUBEADAPT_CHART_VERSION", "1.2.3")
	os.Setenv("HELM_RELEASE_NAME", "my-release")
	os.Setenv("KUBEADAPT_UPGRADE_ENABLED", "true")
	os.Setenv("KUBEADAPT_UPGRADE_CHECK_INTERVAL", "1h")
	os.Setenv("KUBEADAPT_UPGRADE_POLICY", "patch")
	os.Setenv("KUBEADAPT_UPGRADE_CHANNEL", "fast")
	os.Setenv("KUBEADAPT_UPGRADE_DRY_RUN", "true")
	os.Setenv("KUBEADAPT_UPGRADE_TIMEOUT", "30m")
	os.Setenv("KUBEADAPT_UPGRADE_JOB_IMAGE", "alpine/helm:3.15.0")
	os.Setenv("KUBEADAPT_UPGRADE_CHART_REPO", "http://chartmuseum:8080")
	os.Setenv("KUBEADAPT_UPGRADE_INITIAL_DELAY", "5s")
	os.Setenv("LOG_LEVEL", "debug")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all fields
	if cfg.BackendAPIEndpoint != "https://api.test.io" {
		t.Errorf("BackendAPIEndpoint mismatch: got %v", cfg.BackendAPIEndpoint)
	}
	if cfg.AgentToken != "token123" {
		t.Errorf("AgentToken mismatch: got %v", cfg.AgentToken)
	}
	if cfg.ChartVersion != "1.2.3" {
		t.Errorf("ChartVersion mismatch: got %v", cfg.ChartVersion)
	}
	if cfg.ReleaseName != "my-release" {
		t.Errorf("ReleaseName mismatch: got %v", cfg.ReleaseName)
	}
	if cfg.UpgradeEnabled != true {
		t.Errorf("UpgradeEnabled mismatch: got %v", cfg.UpgradeEnabled)
	}
	if cfg.UpgradeCheckInterval != 1*time.Hour {
		t.Errorf("UpgradeCheckInterval mismatch: got %v", cfg.UpgradeCheckInterval)
	}
	if cfg.UpgradePolicy != "patch" {
		t.Errorf("UpgradePolicy mismatch: got %v", cfg.UpgradePolicy)
	}
	if cfg.UpgradeChannel != "fast" {
		t.Errorf("UpgradeChannel mismatch: got %v", cfg.UpgradeChannel)
	}
	if cfg.UpgradeDryRun != true {
		t.Errorf("UpgradeDryRun mismatch: got %v", cfg.UpgradeDryRun)
	}
	if cfg.UpgradeTimeout != 30*time.Minute {
		t.Errorf("UpgradeTimeout mismatch: got %v", cfg.UpgradeTimeout)
	}
	if cfg.UpgradeJobImage != "alpine/helm:3.15.0" {
		t.Errorf("UpgradeJobImage mismatch: got %v", cfg.UpgradeJobImage)
	}
	if cfg.ChartRepo != "http://chartmuseum:8080" {
		t.Errorf("ChartRepo mismatch: got %v", cfg.ChartRepo)
	}
	if cfg.UpgradeInitialDelay != 5*time.Second {
		t.Errorf("UpgradeInitialDelay mismatch: got %v", cfg.UpgradeInitialDelay)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel mismatch: got %v", cfg.LogLevel)
	}
}

func TestGetEnvBool(t *testing.T) {
	testCases := []struct {
		name         string
		value        string
		defaultValue bool
		expected     bool
	}{
		{"empty uses default true", "", true, true},
		{"empty uses default false", "", false, false},
		{"true string", "true", false, true},
		{"false string", "false", true, false},
		{"1 string", "1", false, true},
		{"0 string", "0", true, false},
		{"invalid uses default", "invalid", true, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("TEST_BOOL", tc.value)
			result := getEnvBool("TEST_BOOL", tc.defaultValue)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
			os.Unsetenv("TEST_BOOL")
		})
	}
}

func TestGetEnvDuration(t *testing.T) {
	testCases := []struct {
		name         string
		value        string
		defaultValue time.Duration
		expected     time.Duration
	}{
		{"empty uses default", "", 5 * time.Minute, 5 * time.Minute},
		{"valid duration", "10m", 5 * time.Minute, 10 * time.Minute},
		{"hours", "2h", 5 * time.Minute, 2 * time.Hour},
		{"seconds", "30s", 5 * time.Minute, 30 * time.Second},
		{"invalid uses default", "invalid", 5 * time.Minute, 5 * time.Minute},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("TEST_DURATION", tc.value)
			result := getEnvDuration("TEST_DURATION", tc.defaultValue)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
			os.Unsetenv("TEST_DURATION")
		})
	}
}
