package helm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// UpgradeJobPrefix is the prefix for upgrade job names
	UpgradeJobPrefix = "kubeadapt-upgrade"

	// UpgradeServiceAccountName is the name of the service account for upgrade jobs
	UpgradeServiceAccountName = "kubeadapt-upgrader"

	// DefaultChartRepo is the default Helm chart repository
	DefaultChartRepo = "oci://ghcr.io/kubeadapt/kubeadapt-helm/kubeadapt"

	// ChartName is the name of the KubeAdapt Helm chart
	ChartName = "kubeadapt"
)

// CreateUpgradeJob creates a Kubernetes Job to perform a Helm upgrade
func CreateUpgradeJob(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace string,
	releaseName string,
	targetVersion string,
	chartRepo string,
	upgradeTimeout time.Duration,
	jobImage string,
	logger *zap.Logger,
) (*batchv1.Job, error) {
	jobName := fmt.Sprintf("%s-%s", UpgradeJobPrefix, time.Now().Format("20060102-150405"))

	logger.Info("Creating Helm upgrade job",
		zap.String("job_name", jobName),
		zap.String("release_name", releaseName),
		zap.String("target_version", targetVersion),
		zap.Duration("timeout", upgradeTimeout))

	// Prepare Helm upgrade command
	// --atomic: if upgrade fails, rollback changes
	// --wait: wait for resources to be ready
	// --timeout: maximum time to wait for the upgrade
	// --install: install if release doesn't exist (shouldn't happen, but safe)
	// Base command args (common to both OCI and HTTP repos)
	helmCommand := []string{
		"helm",
		"upgrade",
		"--install",
		"--atomic",
		"--wait",
		"--reuse-values",
		"--timeout", upgradeTimeout.String(),
		"--version", targetVersion,
	}
	// Smart repo detection: OCI uses chart repo as last arg, HTTP uses --repo flag
	if strings.HasPrefix(chartRepo, "oci://") {
		helmCommand = append(helmCommand, releaseName, chartRepo)
	} else {
		// HTTP/HTTPS: use chart name + --repo flag
		helmCommand = append(helmCommand, releaseName, ChartName, "--repo", chartRepo)
	}

	// Job TTL: clean up completed/failed jobs after 1 hour
	ttlSecondsAfterFinished := int32(3600)

	// Backoff limit: don't retry on failure (atomic flag handles rollback)
	backoffLimit := int32(0)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "kubeadapt",
				"app.kubernetes.io/component":  "upgrader",
				"app.kubernetes.io/managed-by": "kubeadapt-upgrader",
				"kubeadapt.io/upgrade-version": targetVersion,
			},
			Annotations: map[string]string{
				"kubeadapt.io/upgrade-from": releaseName,
				"kubeadapt.io/upgrade-to":   targetVersion,
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttlSecondsAfterFinished,
			BackoffLimit:            &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":      "kubeadapt",
						"app.kubernetes.io/component": "upgrader",
						"job-name":                    jobName,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: UpgradeServiceAccountName,
					RestartPolicy:      corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "helm-upgrade",
							Image:   jobImage,
							Command: helmCommand,
							Env: []corev1.EnvVar{
								{
									Name:  "HELM_NAMESPACE",
									Value: namespace,
								},
								{
									// Set HOME to /tmp so helm can write its cache (/.cache is not writable for non-root)
									Name:  "HOME",
									Value: "/tmp",
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    mustParseQuantity("100m"),
									corev1.ResourceMemory: mustParseQuantity("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    mustParseQuantity("500m"),
									corev1.ResourceMemory: mustParseQuantity("256Mi"),
								},
							},
						},
					},
					// Security context for the pod
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: boolPtr(true),
						RunAsUser:    int64Ptr(65534), // nobody user
						FSGroup:      int64Ptr(65534),
					},
				},
			},
		},
	}

	// Create the job
	createdJob, err := clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create upgrade job: %w", err)
	}

	logger.Info("Helm upgrade job created",
		zap.String("job_name", createdJob.Name),
		zap.String("namespace", namespace))

	return createdJob, nil
}

// WaitForJob waits for a Kubernetes Job to complete (success or failure)
func WaitForJob(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace string,
	jobName string,
	timeout time.Duration,
	logger *zap.Logger,
) (bool, error) {
	logger.Info("Waiting for job to complete",
		zap.String("job_name", jobName),
		zap.Duration("timeout", timeout))

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Poll the job status
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			logger.Error("Job completion timeout",
				zap.String("job_name", jobName),
				zap.Duration("timeout", timeout))
			return false, fmt.Errorf("job timeout after %v", timeout)

		case <-ticker.C:
			job, err := clientset.BatchV1().Jobs(namespace).Get(timeoutCtx, jobName, metav1.GetOptions{})
			if err != nil {
				logger.Error("Failed to get job status",
					zap.String("job_name", jobName),
					zap.Error(err))
				return false, fmt.Errorf("get job status: %w", err)
			}

			// Check for completion
			if job.Status.Succeeded > 0 {
				logger.Info("Job completed successfully",
					zap.String("job_name", jobName),
					zap.Int32("succeeded", job.Status.Succeeded))
				return true, nil
			}

			// Check for failure
			if job.Status.Failed > 0 {
				logger.Error("Job failed",
					zap.String("job_name", jobName),
					zap.Int32("failed", job.Status.Failed))

				// Get pod logs for debugging
				podLogs := getJobPodLogs(ctx, clientset, namespace, jobName, logger)
				if podLogs != "" {
					logger.Error("Job pod logs", zap.String("logs", podLogs))
				}

				return false, fmt.Errorf("job failed")
			}

			// Log progress
			logger.Debug("Job still running",
				zap.String("job_name", jobName),
				zap.Int32("active", job.Status.Active),
				zap.Int32("succeeded", job.Status.Succeeded),
				zap.Int32("failed", job.Status.Failed))
		}
	}
}

// getJobPodLogs retrieves logs from the job's pod for debugging
func getJobPodLogs(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace string,
	jobName string,
	logger *zap.Logger,
) string {
	// List pods for this job
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		logger.Warn("Failed to list job pods", zap.Error(err))
		return ""
	}

	if len(pods.Items) == 0 {
		return ""
	}

	// Get logs from the first pod
	pod := pods.Items[0]
	logOptions := &corev1.PodLogOptions{
		Container: "helm-upgrade",
		TailLines: int64Ptr(100), // Last 100 lines
	}

	req := clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, logOptions)
	logs, err := req.DoRaw(ctx)
	if err != nil {
		logger.Warn("Failed to get pod logs", zap.Error(err))
		return ""
	}

	return string(logs)
}

// Helper functions for pointer values
func boolPtr(b bool) *bool {
	return &b
}

func int64Ptr(i int64) *int64 {
	return &i
}

// mustParseQuantity is a helper to parse resource quantities
func mustParseQuantity(s string) resource.Quantity {
	q, err := resource.ParseQuantity(s)
	if err != nil {
		panic(fmt.Sprintf("invalid resource quantity: %s", s))
	}
	return q
}
