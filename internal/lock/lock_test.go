package lock

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestNewUpgradeLock(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	logger := zap.NewNop()

	lock := NewUpgradeLock(clientset, "test-ns", "test-pod", logger)

	if lock.namespace != "test-ns" {
		t.Errorf("expected namespace=test-ns, got %v", lock.namespace)
	}
	if lock.podName != "test-pod" {
		t.Errorf("expected podName=test-pod, got %v", lock.podName)
	}
}

func TestAcquire_CreateNew(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	logger := zap.NewNop()

	lock := NewUpgradeLock(clientset, "test-ns", "test-pod", logger)
	ctx := context.Background()
	upgradeCtx := &UpgradeContext{
		FromVersion: "1.0.0",
		ToVersion:   "1.1.0",
	}

	acquired, err := lock.Acquire(ctx, upgradeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Error("expected lock to be acquired")
	}

	// Verify ConfigMap was created
	cm, err := clientset.CoreV1().ConfigMaps("test-ns").Get(ctx, LockConfigMapName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get ConfigMap: %v", err)
	}
	if cm.Data[LockKeyHolder] != "test-pod" {
		t.Errorf("expected holder=test-pod, got %v", cm.Data[LockKeyHolder])
	}
	if cm.Data[LockKeyFromVersion] != "1.0.0" {
		t.Errorf("expected from_version=1.0.0, got %v", cm.Data[LockKeyFromVersion])
	}
	if cm.Data[LockKeyToVersion] != "1.1.0" {
		t.Errorf("expected to_version=1.1.0, got %v", cm.Data[LockKeyToVersion])
	}
}

func TestAcquire_AlreadyHeld(t *testing.T) {
	// Pre-create the lock ConfigMap held by us
	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LockConfigMapName,
			Namespace: "test-ns",
		},
		Data: map[string]string{
			LockKeyHolder:    "test-pod",
			LockKeyTimestamp: time.Now().UTC().Format(time.RFC3339),
		},
	}
	clientset := fake.NewSimpleClientset(existingCM)
	logger := zap.NewNop()

	lock := NewUpgradeLock(clientset, "test-ns", "test-pod", logger)
	ctx := context.Background()
	upgradeCtx := &UpgradeContext{
		FromVersion: "1.0.0",
		ToVersion:   "1.1.0",
	}

	acquired, err := lock.Acquire(ctx, upgradeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Error("expected lock to be acquired (already held by us)")
	}
}

func TestAcquire_HeldByAnother(t *testing.T) {
	// Pre-create the lock ConfigMap held by another pod
	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LockConfigMapName,
			Namespace: "test-ns",
		},
		Data: map[string]string{
			LockKeyHolder:    "other-pod",
			LockKeyTimestamp: time.Now().UTC().Format(time.RFC3339),
		},
	}
	clientset := fake.NewSimpleClientset(existingCM)
	logger := zap.NewNop()

	lock := NewUpgradeLock(clientset, "test-ns", "test-pod", logger)
	ctx := context.Background()
	upgradeCtx := &UpgradeContext{
		FromVersion: "1.0.0",
		ToVersion:   "1.1.0",
	}

	acquired, err := lock.Acquire(ctx, upgradeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acquired {
		t.Error("expected lock NOT to be acquired (held by another pod)")
	}
}

func TestAcquire_ExpiredLock(t *testing.T) {
	// Pre-create the lock ConfigMap with an expired timestamp
	expiredTime := time.Now().UTC().Add(-LockTTL - time.Hour)
	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LockConfigMapName,
			Namespace: "test-ns",
		},
		Data: map[string]string{
			LockKeyHolder:    "other-pod",
			LockKeyTimestamp: expiredTime.Format(time.RFC3339),
		},
	}
	clientset := fake.NewSimpleClientset(existingCM)
	logger := zap.NewNop()

	lock := NewUpgradeLock(clientset, "test-ns", "test-pod", logger)
	ctx := context.Background()
	upgradeCtx := &UpgradeContext{
		FromVersion: "1.0.0",
		ToVersion:   "1.1.0",
	}

	acquired, err := lock.Acquire(ctx, upgradeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Error("expected lock to be acquired (previous lock expired)")
	}

	// Verify we're now the holder
	cm, err := clientset.CoreV1().ConfigMaps("test-ns").Get(ctx, LockConfigMapName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get ConfigMap: %v", err)
	}
	if cm.Data[LockKeyHolder] != "test-pod" {
		t.Errorf("expected holder=test-pod, got %v", cm.Data[LockKeyHolder])
	}
}

func TestRelease_Success(t *testing.T) {
	// Pre-create the lock ConfigMap held by us
	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LockConfigMapName,
			Namespace: "test-ns",
		},
		Data: map[string]string{
			LockKeyHolder:    "test-pod",
			LockKeyTimestamp: time.Now().UTC().Format(time.RFC3339),
		},
	}
	clientset := fake.NewSimpleClientset(existingCM)
	logger := zap.NewNop()

	lock := NewUpgradeLock(clientset, "test-ns", "test-pod", logger)
	ctx := context.Background()

	err := lock.Release(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify ConfigMap was deleted
	_, err = clientset.CoreV1().ConfigMaps("test-ns").Get(ctx, LockConfigMapName, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Error("expected ConfigMap to be deleted")
	}
}

func TestRelease_NotHeld(t *testing.T) {
	// Pre-create the lock ConfigMap held by another pod
	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LockConfigMapName,
			Namespace: "test-ns",
		},
		Data: map[string]string{
			LockKeyHolder:    "other-pod",
			LockKeyTimestamp: time.Now().UTC().Format(time.RFC3339),
		},
	}
	clientset := fake.NewSimpleClientset(existingCM)
	logger := zap.NewNop()

	lock := NewUpgradeLock(clientset, "test-ns", "test-pod", logger)
	ctx := context.Background()

	err := lock.Release(ctx)
	if err == nil {
		t.Error("expected error when releasing lock held by another pod")
	}
}

func TestRelease_NotExists(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	logger := zap.NewNop()

	lock := NewUpgradeLock(clientset, "test-ns", "test-pod", logger)
	ctx := context.Background()

	// Should not error if lock doesn't exist
	err := lock.Release(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsHeld_True(t *testing.T) {
	// Pre-create the lock ConfigMap held by us
	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LockConfigMapName,
			Namespace: "test-ns",
		},
		Data: map[string]string{
			LockKeyHolder:    "test-pod",
			LockKeyTimestamp: time.Now().UTC().Format(time.RFC3339),
		},
	}
	clientset := fake.NewSimpleClientset(existingCM)
	logger := zap.NewNop()

	lock := NewUpgradeLock(clientset, "test-ns", "test-pod", logger)
	ctx := context.Background()

	held, err := lock.IsHeld(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !held {
		t.Error("expected IsHeld=true")
	}
}

func TestIsHeld_False(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	logger := zap.NewNop()

	lock := NewUpgradeLock(clientset, "test-ns", "test-pod", logger)
	ctx := context.Background()

	held, err := lock.IsHeld(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if held {
		t.Error("expected IsHeld=false")
	}
}

func TestAcquire_RaceCondition(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	logger := zap.NewNop()

	// Simulate race condition: another pod creates the lock while we're trying
	clientset.PrependReactor("create", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewAlreadyExists(corev1.Resource("configmaps"), LockConfigMapName)
	})

	lock := NewUpgradeLock(clientset, "test-ns", "test-pod", logger)
	ctx := context.Background()
	upgradeCtx := &UpgradeContext{
		FromVersion: "1.0.0",
		ToVersion:   "1.1.0",
	}

	acquired, err := lock.Acquire(ctx, upgradeCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acquired {
		t.Error("expected lock NOT to be acquired due to race condition")
	}
}
