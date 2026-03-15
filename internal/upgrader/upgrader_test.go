package upgrader

import (
	"testing"

	"github.com/kubeadapt/kubeadapt-upgrader/internal/backend"
)

// TestTargetVersionSelection verifies the logic for selecting the target version
// based on upgrade path, recommended version, and latest version.
// This test directly validates the hop selection logic without mocking.
func TestTargetVersionSelection(t *testing.T) {
	tests := []struct {
		name               string
		upgradePath        []string
		recommendedVersion string
		latestVersion      string
		expectedTarget     string
		description        string
	}{
		{
			name:               "multi-hop path uses first hop",
			upgradePath:        []string{"0.35.0", "0.35.2", "0.36.0"},
			recommendedVersion: "0.36.0",
			latestVersion:      "0.36.0",
			expectedTarget:     "0.35.2",
			description:        "When UpgradePath has >=2 elements, UpgradePath[1] is used",
		},
		{
			name:               "two-element path uses second element",
			upgradePath:        []string{"0.35.0", "0.36.0"},
			recommendedVersion: "0.36.0",
			latestVersion:      "0.36.0",
			expectedTarget:     "0.36.0",
			description:        "Two-element path: use UpgradePath[1]",
		},
		{
			name:               "single-element path falls back to recommended",
			upgradePath:        []string{"0.36.0"},
			recommendedVersion: "0.36.0",
			latestVersion:      "0.36.0",
			expectedTarget:     "0.36.0",
			description:        "Single-element path: use RecommendedVersion",
		},
		{
			name:               "empty path falls back to recommended",
			upgradePath:        []string{},
			recommendedVersion: "0.36.0",
			latestVersion:      "0.36.0",
			expectedTarget:     "0.36.0",
			description:        "Empty path: use RecommendedVersion",
		},
		{
			name:               "nil path falls back to recommended",
			upgradePath:        nil,
			recommendedVersion: "0.36.0",
			latestVersion:      "0.36.0",
			expectedTarget:     "0.36.0",
			description:        "Nil path: use RecommendedVersion",
		},
		{
			name:               "empty recommended falls back to latest",
			upgradePath:        []string{},
			recommendedVersion: "",
			latestVersion:      "0.36.0",
			expectedTarget:     "0.36.0",
			description:        "Empty recommended: use LatestVersion",
		},
		{
			name:               "multi-hop with empty recommended uses first hop",
			upgradePath:        []string{"0.35.0", "0.35.2", "0.36.0"},
			recommendedVersion: "",
			latestVersion:      "0.36.0",
			expectedTarget:     "0.35.2",
			description:        "Multi-hop path takes precedence over empty recommended",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the target version selection logic from runCheck()
			resp := &backend.UpdateCheckResponse{
				UpgradePath:        tt.upgradePath,
				RecommendedVersion: tt.recommendedVersion,
				LatestVersion:      tt.latestVersion,
			}

			// Call the real production function
			targetVersion := selectTargetVersion(resp)

			// Verify the correct target version was selected
			if targetVersion != tt.expectedTarget {
				t.Errorf("expected target version %q, got %q (test: %s)",
					tt.expectedTarget, targetVersion, tt.description)
			}
		})
	}
}
