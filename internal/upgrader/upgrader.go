package upgrader

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kubeadapt/kubeadapt-upgrader/internal/backend"
	"github.com/kubeadapt/kubeadapt-upgrader/internal/config"
	"github.com/kubeadapt/kubeadapt-upgrader/internal/helm"
	"github.com/kubeadapt/kubeadapt-upgrader/internal/lock"
	"github.com/kubeadapt/kubeadapt-upgrader/internal/platform"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
)

// Upgrader manages the auto-upgrade lifecycle for KubeAdapt
type Upgrader struct {
	cfg           *config.Config
	logger        *zap.Logger
	backendClient *backend.Client
	k8sClientset  kubernetes.Interface
	upgradeLock   *lock.UpgradeLock
	platform      platform.Platform

	// Control channels
	stopCh chan struct{}
	doneCh chan struct{}

	// State
	mu              sync.RWMutex
	lastCheck       time.Time
	lastCheckResult *backend.UpdateCheckResponse
	running         bool
}

// New creates a new Upgrader instance
func New(
	cfg *config.Config,
	backendClient *backend.Client,
	k8sClientset kubernetes.Interface,
	logger *zap.Logger,
) *Upgrader {
	logger = logger.With(zap.String("component", "upgrader"))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	detectedPlatform := platform.DetectPlatform(ctx, k8sClientset)

	logger.Info("Platform detected",
		zap.String("platform", detectedPlatform.String()))

	// Create upgrade lock
	upgradeLock := lock.NewUpgradeLock(k8sClientset, cfg.PodNamespace, cfg.PodName, logger)

	return &Upgrader{
		cfg:           cfg,
		logger:        logger,
		backendClient: backendClient,
		k8sClientset:  k8sClientset,
		upgradeLock:   upgradeLock,
		platform:      detectedPlatform,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}
}

// Start begins the upgrade check loop
func (u *Upgrader) Start(ctx context.Context) error {
	u.mu.Lock()
	if u.running {
		u.mu.Unlock()
		return fmt.Errorf("upgrader already running")
	}
	// Reinitialize channels in case this is a restart
	u.stopCh = make(chan struct{})
	u.doneCh = make(chan struct{})
	u.running = true
	u.mu.Unlock()

	u.logger.Info("Starting upgrader",
		zap.Duration("check_interval", u.cfg.UpgradeCheckInterval),
		zap.String("policy", u.cfg.UpgradePolicy),
		zap.String("channel", u.cfg.UpgradeChannel),
		zap.Bool("dry_run", u.cfg.UpgradeDryRun),
		zap.Bool("enabled", u.cfg.UpgradeEnabled))

	// If upgrade is disabled, just return without starting the loop
	if !u.cfg.UpgradeEnabled {
		u.logger.Info("Auto-upgrade is disabled")
		u.mu.Lock()
		u.running = false
		u.mu.Unlock()
		// Don't close doneCh here - it wasn't started so nothing to signal
		return nil
	}

	// Start the check loop in a goroutine
	go u.checkLoop(ctx)

	return nil
}

// Stop stops the upgrader
func (u *Upgrader) Stop() error {
	u.mu.Lock()
	if !u.running {
		u.mu.Unlock()
		return nil
	}
	u.mu.Unlock()

	u.logger.Info("Stopping upgrader")

	// Signal stop and wait for completion
	select {
	case <-u.stopCh:
		// Already closed, skip
	default:
		close(u.stopCh)
	}

	// Wait for the loop to finish with timeout
	select {
	case <-u.doneCh:
		u.logger.Info("Upgrader stopped")
	case <-time.After(10 * time.Second):
		u.logger.Warn("Upgrader stop timeout")
	}

	u.mu.Lock()
	u.running = false
	u.mu.Unlock()

	return nil
}

// checkLoop is the main loop that periodically checks for updates
func (u *Upgrader) checkLoop(ctx context.Context) {
	defer close(u.doneCh)

	// Perform initial check after a short delay
	initialDelay := u.cfg.UpgradeInitialDelay
	u.logger.Info("Scheduling initial upgrade check",
		zap.Duration("delay", initialDelay))

	ticker := time.NewTicker(u.cfg.UpgradeCheckInterval)
	defer ticker.Stop()

	// Initial check timer
	initialTimer := time.NewTimer(initialDelay)
	defer initialTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			u.logger.Info("Context canceled, stopping upgrade check loop")
			return

		case <-u.stopCh:
			u.logger.Info("Stop signal received, stopping upgrade check loop")
			return

		case <-initialTimer.C:
			// Perform initial check
			u.logger.Info("Performing initial upgrade check")
			u.runCheck(ctx)

		case <-ticker.C:
			// Periodic check
			u.logger.Debug("Performing periodic upgrade check")
			u.runCheck(ctx)
		}
	}
}

// runCheck performs a single upgrade check
func (u *Upgrader) runCheck(ctx context.Context) {
	u.logger.Debug("Running upgrade check",
		zap.String("current_version", u.cfg.ChartVersion),
		zap.String("policy", u.cfg.UpgradePolicy),
		zap.String("channel", u.cfg.UpgradeChannel))

	// Check for updates from backend
	req := &backend.UpdateCheckRequest{
		Chart:          helm.ChartName,
		CurrentVersion: u.cfg.ChartVersion,
		Policy:         u.cfg.UpgradePolicy,
		Platform:       u.platform.String(),
		Channel:        u.cfg.UpgradeChannel,
	}

	resp, err := u.backendClient.CheckForUpdates(ctx, req)
	if err != nil {
		u.logger.Error("Failed to check for updates",
			zap.Error(err))
		return
	}

	// Update state
	u.mu.Lock()
	u.lastCheck = time.Now()
	u.lastCheckResult = resp
	u.mu.Unlock()

	// Log the result
	u.logger.Info("Update check completed",
		zap.Bool("update_available", resp.UpdateAvailable),
		zap.String("current_version", resp.CurrentVersion),
		zap.String("latest_version", resp.LatestVersion),
		zap.String("recommended_version", resp.RecommendedVersion),
		zap.String("message", resp.Message))

	// If no update is available, we're done
	if !resp.UpdateAvailable {
		u.logger.Debug("No update available")
		return
	}

	// Determine target version: use first hop from upgrade path if available.
	// This enables sequential multi-hop upgrades -- each pod restart executes one hop.
	targetVersion := selectTargetVersion(resp)

	if len(resp.UpgradePath) > 0 {
		u.logger.Info("Upgrade path computed",
			zap.Strings("path", resp.UpgradePath),
			zap.String("next_hop", targetVersion))
	}

	// Skip if already at target version (e.g. new pod after self-upgrade).
	if targetVersion == u.cfg.ChartVersion {
		u.logger.Info("Already at target version, skipping upgrade",
			zap.String("version", targetVersion))
		return
	}

	u.logger.Info("Update available",
		zap.String("target_version", targetVersion),
		zap.String("change_type", resp.ChangeType))

	// If dry-run mode, just log and return
	if u.cfg.UpgradeDryRun {
		u.logger.Info("Dry-run mode: would upgrade",
			zap.String("from", u.cfg.ChartVersion),
			zap.String("to", targetVersion))
		return
	}

	// Perform the upgrade
	if err := u.performUpgrade(ctx, targetVersion); err != nil {
		u.logger.Error("Upgrade failed",
			zap.String("target_version", targetVersion),
			zap.Error(err))
		return
	}

	u.logger.Info("Upgrade completed successfully",
		zap.String("from", u.cfg.ChartVersion),
		zap.String("to", targetVersion))
}

// performUpgrade executes the upgrade to the target version
func (u *Upgrader) performUpgrade(ctx context.Context, targetVersion string) error {
	u.logger.Info("Starting upgrade",
		zap.String("from", u.cfg.ChartVersion),
		zap.String("to", targetVersion))

	// Try to acquire the upgrade lock
	upgradeCtx := &lock.UpgradeContext{
		FromVersion: u.cfg.ChartVersion,
		ToVersion:   targetVersion,
	}
	acquired, err := u.upgradeLock.Acquire(ctx, upgradeCtx)
	if err != nil {
		return fmt.Errorf("acquire upgrade lock: %w", err)
	}

	if !acquired {
		u.logger.Info("Another instance is performing upgrade, skipping")
		return nil
	}

	// Ensure lock is released when done
	//nolint:contextcheck // intentional: background context survives pod SIGTERM during self-upgrade
	defer func() {
		// Use background context: after self-upgrade SIGTERM cancels ctx,
		// we still must release the lock so subsequent checks can proceed.
		rCtx, rCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer rCancel()
		if err := u.upgradeLock.Release(rCtx); err != nil {
			u.logger.Error("Failed to release upgrade lock",
				zap.Error(err))
		}
	}()

	// Create and run the Helm upgrade job
	job, err := helm.CreateUpgradeJob(
		ctx,
		u.k8sClientset,
		u.cfg.PodNamespace,
		u.cfg.ReleaseName,
		targetVersion,
		u.cfg.ChartRepo,
		u.cfg.UpgradeTimeout,
		u.cfg.UpgradeJobImage,
		u.logger,
	)
	if err != nil {
		_ = u.reportStatus(ctx, targetVersion, "failed", err.Error())
		return fmt.Errorf("create upgrade job: %w", err)
	}

	// Wait for job completion — use context.Background() so SIGTERM (which cancels ctx)
	// doesn't abort the wait. The upgrader pod may be terminated during self-upgrade
	// (Deployment spec change → new pod Ready → old pod SIGTERM). We must wait for
	// the Job regardless of pod lifecycle to send the correct status report.
	waitTimeout := u.cfg.UpgradeTimeout + (5 * time.Minute)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), waitTimeout)
	defer waitCancel()
	//nolint:contextcheck // intentional: background context survives pod SIGTERM during self-upgrade
	success, err := helm.WaitForJob(
		waitCtx,
		u.k8sClientset,
		u.cfg.PodNamespace,
		job.Name,
		waitTimeout,
		u.logger,
	)

	if err != nil {
		rCtx, rCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer rCancel()
		//nolint:contextcheck // intentional: background context survives pod SIGTERM during self-upgrade
		_ = u.reportStatus(rCtx, targetVersion, "failed", err.Error())
		return fmt.Errorf("wait for upgrade job: %w", err)
	}

	if !success {
		// Check if rollback occurred (helm --atomic auto-rollback on failure)
		rollbackInfo, rollbackErr := helm.CheckRollbackOccurred(
			ctx,
			u.cfg.PodNamespace,
			u.cfg.ReleaseName,
			targetVersion,
			u.cfg.ChartVersion,
			u.logger,
		)

		if rollbackErr != nil {
			u.logger.Warn("Failed to check rollback status",
				zap.Error(rollbackErr))
		}

		if rollbackInfo != nil && rollbackInfo.DidRollback {
			// Upgrade failed but rollback succeeded - report with rollback info (API expects "rolled_back")
			errMsg := fmt.Sprintf("upgrade failed, rolled back to %s", rollbackInfo.RolledBackTo)
			rCtx, rCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer rCancel()
			//nolint:contextcheck // intentional: background context survives pod SIGTERM during self-upgrade
			_ = u.reportStatus(rCtx, targetVersion, "rolled_back", errMsg)
			return fmt.Errorf("upgrade job failed: %s", errMsg)
		}

		// Upgrade failed without rollback (or rollback status unknown)
		errMsg := "upgrade job failed"
		rCtx, rCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer rCancel()
		//nolint:contextcheck // intentional: background context survives pod SIGTERM during self-upgrade
		_ = u.reportStatus(rCtx, targetVersion, "failed", errMsg)
		return errors.New(errMsg)
	}

	// Report success — use background ctx so SIGTERM doesn't prevent the report
	{
		rCtx, rCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer rCancel()
		//nolint:contextcheck // intentional: background context survives pod SIGTERM during self-upgrade
		if err := u.reportStatus(rCtx, targetVersion, "success", ""); err != nil {
			u.logger.Warn("Failed to report upgrade completion",
				zap.Error(err))
		}
	}

	return nil
}

// reportStatus reports the upgrade status to the backend
func (u *Upgrader) reportStatus(ctx context.Context, targetVersion, status, errorMsg string) error {
	report := &backend.UpdateStatusReport{
		Chart:        helm.ChartName,
		FromVersion:  u.cfg.ChartVersion,
		ToVersion:    targetVersion,
		Status:       status,
		ErrorMessage: errorMsg,
		Platform:     u.platform.String(),
	}

	return u.backendClient.ReportUpdateStatus(ctx, report)
}

// GetLastCheck returns the last check time and result
func (u *Upgrader) GetLastCheck() (time.Time, *backend.UpdateCheckResponse) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.lastCheck, u.lastCheckResult
}

// IsRunning returns whether the upgrader is running
func (u *Upgrader) IsRunning() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.running
}

// selectTargetVersion picks the next version to upgrade to from an update check response.
// If an upgrade path with >=2 hops exists, the first hop (index 1) is used.
// Otherwise it falls back to RecommendedVersion, then LatestVersion.
func selectTargetVersion(resp *backend.UpdateCheckResponse) string {
	targetVersion := resp.RecommendedVersion
	if len(resp.UpgradePath) >= 2 {
		targetVersion = resp.UpgradePath[1] // execute first hop only
	}
	if targetVersion == "" {
		targetVersion = resp.LatestVersion
	}
	return targetVersion
}
