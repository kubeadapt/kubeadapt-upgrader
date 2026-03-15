package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the upgrader service
type Config struct {
	// Backend connection
	BackendAPIEndpoint string
	AgentToken         string

	// Version info
	ChartVersion string
	ReleaseName  string

	// Pod identity (from Downward API)
	PodName      string
	PodNamespace string

	// Upgrade settings
	UpgradeEnabled       bool
	UpgradeCheckInterval time.Duration
	UpgradePolicy        string // "minor", "patch", "all"
	UpgradeChannel       string // "stable", "fast"
	UpgradeDryRun        bool
	UpgradeTimeout       time.Duration
	UpgradeJobImage      string
	ChartRepo            string
	UpgradeInitialDelay  time.Duration

	// Logging
	LogLevel string
}

// DefaultConfig returns a Config with default values
func DefaultConfig() *Config {
	return &Config{
		UpgradeEnabled:       false,
		UpgradeCheckInterval: 6 * time.Hour,
		UpgradePolicy:        "minor",
		UpgradeChannel:       "stable",
		UpgradeDryRun:        false,
		UpgradeTimeout:       15 * time.Minute,
		UpgradeJobImage:      "alpine/helm:3.14.3",
		ChartRepo:            "oci://ghcr.io/kubeadapt/kubeadapt-helm/kubeadapt",
		UpgradeInitialDelay:  1 * time.Minute,
		LogLevel:             "info",
	}
}

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() (*Config, error) {
	cfg := DefaultConfig()

	// Required fields
	cfg.BackendAPIEndpoint = getEnvOrDefault("KUBEADAPT_BACKEND_URL", "https://agent.kubeadapt.io")

	cfg.AgentToken = os.Getenv("KUBEADAPT_AGENT_TOKEN")
	if cfg.AgentToken == "" {
		return nil, fmt.Errorf("KUBEADAPT_AGENT_TOKEN is required")
	}

	cfg.PodName = os.Getenv("POD_NAME")
	if cfg.PodName == "" {
		return nil, fmt.Errorf("POD_NAME is required")
	}

	cfg.PodNamespace = os.Getenv("POD_NAMESPACE")
	if cfg.PodNamespace == "" {
		return nil, fmt.Errorf("POD_NAMESPACE is required")
	}

	// Version info
	cfg.ChartVersion = os.Getenv("KUBEADAPT_CHART_VERSION")
	cfg.ReleaseName = getEnvOrDefault("HELM_RELEASE_NAME", "kubeadapt")

	// Upgrade settings
	cfg.UpgradeEnabled = getEnvBool("KUBEADAPT_UPGRADE_ENABLED", false)
	cfg.UpgradeCheckInterval = getEnvDuration("KUBEADAPT_UPGRADE_CHECK_INTERVAL", 6*time.Hour)
	cfg.UpgradePolicy = getEnvOrDefault("KUBEADAPT_UPGRADE_POLICY", "minor")
	cfg.UpgradeChannel = getEnvOrDefault("KUBEADAPT_UPGRADE_CHANNEL", "stable")
	cfg.UpgradeDryRun = getEnvBool("KUBEADAPT_UPGRADE_DRY_RUN", false)
	cfg.UpgradeTimeout = getEnvDuration("KUBEADAPT_UPGRADE_TIMEOUT", 15*time.Minute)
	cfg.UpgradeJobImage = getEnvOrDefault("KUBEADAPT_UPGRADE_JOB_IMAGE", "alpine/helm:3.14.3")
	cfg.ChartRepo = getEnvOrDefault("KUBEADAPT_UPGRADE_CHART_REPO", "oci://ghcr.io/kubeadapt/kubeadapt-helm/kubeadapt")
	cfg.UpgradeInitialDelay = getEnvDuration("KUBEADAPT_UPGRADE_INITIAL_DELAY", 1*time.Minute)

	// Logging
	cfg.LogLevel = getEnvOrDefault("LOG_LEVEL", "info")

	return cfg, nil
}

// getEnvOrDefault returns the environment variable value or a default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvBool parses a boolean from environment variable
func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

// getEnvDuration parses a duration from environment variable
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}
