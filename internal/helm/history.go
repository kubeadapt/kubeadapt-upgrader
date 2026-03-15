package helm

import (
	"context"
	"fmt"
	"os"
	"sort"

	"go.uber.org/zap"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// RollbackInfo contains information about a rollback that occurred
type RollbackInfo struct {
	// DidRollback indicates whether a rollback occurred after upgrade failure
	DidRollback bool
	// RolledBackTo is the version that was rolled back to
	RolledBackTo string
	// FailedVersion is the version that failed to deploy
	FailedVersion string
	// FailedRevision is the revision number of the failed deployment
	FailedRevision int
	// CurrentRevision is the current deployed revision
	CurrentRevision int
}

// CheckRollbackOccurred checks if a rollback occurred after an upgrade failure
// by examining the helm release history
func CheckRollbackOccurred(
	ctx context.Context,
	namespace string,
	releaseName string,
	targetVersion string,
	originalVersion string,
	logger *zap.Logger,
) (*RollbackInfo, error) {
	logger.Debug("Checking for rollback",
		zap.String("release", releaseName),
		zap.String("namespace", namespace),
		zap.String("target_version", targetVersion),
		zap.String("original_version", originalVersion))

	// Initialize Helm action configuration
	actionConfig := new(action.Configuration)

	// Use generic CLI options for in-cluster config
	getter := genericclioptions.NewConfigFlags(true)
	getter.Namespace = &namespace

	// Initialize the action configuration
	// helmDriver can be "secrets", "configmaps", or "memory"
	helmDriver := os.Getenv("HELM_DRIVER")
	if helmDriver == "" {
		helmDriver = "secrets" // Default Helm storage
	}

	if err := actionConfig.Init(getter, namespace, helmDriver, func(format string, v ...interface{}) {
		logger.Debug(fmt.Sprintf(format, v...))
	}); err != nil {
		return nil, fmt.Errorf("init helm action config: %w", err)
	}

	// Create and run history action
	histAction := action.NewHistory(actionConfig)
	histAction.Max = 10 // Get last 10 revisions

	releases, err := histAction.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("get release history: %w", err)
	}

	if len(releases) == 0 {
		return nil, fmt.Errorf("no release history found for %s", releaseName)
	}

	// Sort releases by revision (descending - newest first)
	sort.Slice(releases, func(i, j int) bool {
		return releases[i].Version > releases[j].Version
	})

	// Find the current deployed release and any failed releases
	var currentDeployed *release.Release
	var failedRelease *release.Release

	for _, rel := range releases {
		logger.Debug("Release history entry",
			zap.Int("revision", rel.Version),
			zap.String("status", rel.Info.Status.String()),
			zap.String("chart_version", rel.Chart.Metadata.Version),
			zap.String("description", rel.Info.Description))

		// Find current deployed release
		if rel.Info.Status == release.StatusDeployed && currentDeployed == nil {
			currentDeployed = rel
		}

		// Find the most recent failed release that matches target version
		if rel.Info.Status == release.StatusFailed &&
			rel.Chart.Metadata.Version == targetVersion &&
			failedRelease == nil {
			failedRelease = rel
		}
	}

	if currentDeployed == nil {
		return nil, fmt.Errorf("no deployed release found for %s", releaseName)
	}

	info := &RollbackInfo{
		CurrentRevision: currentDeployed.Version,
	}

	// Determine if rollback occurred:
	// 1. There's a failed release for the target version
	// 2. Current deployed version is NOT the target version (rolled back to original)
	if failedRelease != nil {
		info.FailedVersion = failedRelease.Chart.Metadata.Version
		info.FailedRevision = failedRelease.Version

		if currentDeployed.Chart.Metadata.Version != targetVersion {
			// Rollback occurred - current version is different from target
			info.DidRollback = true
			info.RolledBackTo = currentDeployed.Chart.Metadata.Version

			logger.Info("Rollback detected",
				zap.String("failed_version", info.FailedVersion),
				zap.String("rolled_back_to", info.RolledBackTo),
				zap.Int("failed_revision", info.FailedRevision),
				zap.Int("current_revision", info.CurrentRevision))
		}
	}

	return info, nil
}
