package lock

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// LockConfigMapName is the name of the ConfigMap used for distributed locking
	LockConfigMapName = "kubeadapt-upgrade-lock"

	// LockTTL is the time-to-live for the lock
	// If a lock holder crashes, the lock will be considered expired after this duration
	LockTTL = 30 * time.Minute

	// LockKeyHolder is the ConfigMap key for the lock holder
	LockKeyHolder = "holder"

	// LockKeyTimestamp is the ConfigMap key for the lock timestamp
	LockKeyTimestamp = "timestamp"

	// LockKeyFromVersion is the ConfigMap key for the source version being upgraded from
	LockKeyFromVersion = "from_version"

	// LockKeyToVersion is the ConfigMap key for the target version being upgraded to
	LockKeyToVersion = "to_version"
)

// UpgradeLock provides distributed locking for upgrade operations
// using a Kubernetes ConfigMap as the coordination mechanism
type UpgradeLock struct {
	clientset kubernetes.Interface
	namespace string
	podName   string
	logger    *zap.Logger
}

// NewUpgradeLock creates a new upgrade lock
func NewUpgradeLock(clientset kubernetes.Interface, namespace, podName string, logger *zap.Logger) *UpgradeLock {
	return &UpgradeLock{
		clientset: clientset,
		namespace: namespace,
		podName:   podName,
		logger:    logger.With(zap.String("component", "upgrade_lock")),
	}
}

// UpgradeContext contains version information for the upgrade operation
type UpgradeContext struct {
	FromVersion string
	ToVersion   string
}

// Acquire attempts to acquire the upgrade lock
// Returns true if the lock was acquired, false if another instance holds it
func (l *UpgradeLock) Acquire(ctx context.Context, upgradeCtx *UpgradeContext) (bool, error) {
	l.logger.Debug("Attempting to acquire upgrade lock",
		zap.String("pod_name", l.podName),
		zap.String("namespace", l.namespace),
		zap.String("from_version", upgradeCtx.FromVersion),
		zap.String("to_version", upgradeCtx.ToVersion))

	// Try to get existing lock
	cm, err := l.clientset.CoreV1().ConfigMaps(l.namespace).Get(ctx, LockConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("get lock configmap: %w", err)
		}

		// ConfigMap doesn't exist, create it
		return l.createLock(ctx, upgradeCtx)
	}

	// ConfigMap exists, check if it's expired or owned by us
	holder := cm.Data[LockKeyHolder]
	timestampStr := cm.Data[LockKeyTimestamp]

	// If we already hold the lock, consider it acquired
	if holder == l.podName {
		l.logger.Info("Already holding upgrade lock")
		return true, nil
	}

	// Check if lock is expired
	if timestampStr != "" {
		timestamp, err := time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			l.logger.Warn("Failed to parse lock timestamp, treating as expired",
				zap.String("timestamp", timestampStr),
				zap.Error(err))
		} else if time.Since(timestamp) < LockTTL {
			// Lock is still valid and held by another instance
			l.logger.Info("Upgrade lock is held by another instance",
				zap.String("holder", holder),
				zap.Time("acquired_at", timestamp),
				zap.Duration("age", time.Since(timestamp)))
			return false, nil
		}
	}

	// Lock is expired, try to take it over
	l.logger.Info("Upgrade lock expired, attempting takeover",
		zap.String("previous_holder", holder),
		zap.String("timestamp", timestampStr))

	return l.updateLock(ctx, cm, upgradeCtx)
}

// Release releases the upgrade lock if held by this instance
func (l *UpgradeLock) Release(ctx context.Context) error {
	l.logger.Debug("Releasing upgrade lock",
		zap.String("pod_name", l.podName))

	// Get the lock ConfigMap
	cm, err := l.clientset.CoreV1().ConfigMaps(l.namespace).Get(ctx, LockConfigMapName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Lock doesn't exist, nothing to release
			l.logger.Debug("Lock ConfigMap not found, nothing to release")
			return nil
		}
		return fmt.Errorf("get lock configmap: %w", err)
	}

	// Check if we hold the lock
	holder := cm.Data[LockKeyHolder]
	if holder != l.podName {
		l.logger.Warn("Cannot release lock held by another instance",
			zap.String("holder", holder))
		return fmt.Errorf("lock is held by %s, not %s", holder, l.podName)
	}

	// Delete the ConfigMap to release the lock
	err = l.clientset.CoreV1().ConfigMaps(l.namespace).Delete(ctx, LockConfigMapName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete lock configmap: %w", err)
	}

	l.logger.Info("Upgrade lock released")
	return nil
}

// IsHeld checks if this instance holds the upgrade lock
func (l *UpgradeLock) IsHeld(ctx context.Context) (bool, error) {
	cm, err := l.clientset.CoreV1().ConfigMaps(l.namespace).Get(ctx, LockConfigMapName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get lock configmap: %w", err)
	}

	holder := cm.Data[LockKeyHolder]
	return holder == l.podName, nil
}

// createLock creates a new lock ConfigMap
func (l *UpgradeLock) createLock(ctx context.Context, upgradeCtx *UpgradeContext) (bool, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LockConfigMapName,
			Namespace: l.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "kubeadapt",
				"app.kubernetes.io/component": "upgrade-lock",
			},
		},
		Data: map[string]string{
			LockKeyHolder:      l.podName,
			LockKeyTimestamp:   time.Now().UTC().Format(time.RFC3339),
			LockKeyFromVersion: upgradeCtx.FromVersion,
			LockKeyToVersion:   upgradeCtx.ToVersion,
		},
	}

	_, err := l.clientset.CoreV1().ConfigMaps(l.namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Another instance created it first, race condition
			l.logger.Debug("Lock was created by another instance (race condition)")
			return false, nil
		}
		return false, fmt.Errorf("create lock configmap: %w", err)
	}

	l.logger.Info("Acquired upgrade lock")
	return true, nil
}

// updateLock updates an existing lock ConfigMap to take ownership
func (l *UpgradeLock) updateLock(ctx context.Context, cm *corev1.ConfigMap, upgradeCtx *UpgradeContext) (bool, error) {
	// Update the lock data
	cm.Data[LockKeyHolder] = l.podName
	cm.Data[LockKeyTimestamp] = time.Now().UTC().Format(time.RFC3339)
	cm.Data[LockKeyFromVersion] = upgradeCtx.FromVersion
	cm.Data[LockKeyToVersion] = upgradeCtx.ToVersion

	_, err := l.clientset.CoreV1().ConfigMaps(l.namespace).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		if apierrors.IsConflict(err) {
			// Another instance updated it first, race condition
			l.logger.Debug("Lock was updated by another instance (race condition)")
			return false, nil
		}
		return false, fmt.Errorf("update lock configmap: %w", err)
	}

	l.logger.Info("Acquired upgrade lock (takeover)")
	return true, nil
}
