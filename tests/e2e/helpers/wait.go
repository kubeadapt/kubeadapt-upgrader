package helpers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// WaitForJob waits until a Job matching the labelSelector appears AND completes (success or failure).
// Returns the job name and whether it succeeded.
func WaitForJob(ctx context.Context, clientset kubernetes.Interface, namespace, labelSelector string, timeout time.Duration) (string, bool, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		jobs, err := clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		for _, job := range jobs.Items {
			if job.Status.Succeeded > 0 {
				return job.Name, true, nil
			}
			if job.Status.Failed > 0 {
				return job.Name, false, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", false, fmt.Errorf("context canceled waiting for job")
		case <-time.After(5 * time.Second):
		}
	}
	return "", false, fmt.Errorf("no completed job matching %q found after %v", labelSelector, timeout)
}

// WaitForNoJob asserts that NO Job matching labelSelector appears within the timeout.
func WaitForNoJob(ctx context.Context, clientset kubernetes.Interface, namespace, labelSelector string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		jobs, err := clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err == nil && len(jobs.Items) > 0 {
			return fmt.Errorf("expected no job matching %q, but found %d jobs", labelSelector, len(jobs.Items))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(3 * time.Second):
		}
	}
	return nil // No job appeared within timeout — expected behavior
}

// CleanupJobs deletes all Jobs matching labelSelector in the namespace.
// Call this at the start of each test to ensure no stale jobs from previous tests.
func CleanupJobs(ctx context.Context, clientset kubernetes.Interface, namespace, labelSelector string) error {
	jobs, err := clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}
	propagation := metav1.DeletePropagationBackground
	for _, job := range jobs.Items {
		if err := clientset.BatchV1().Jobs(namespace).Delete(ctx, job.Name, metav1.DeleteOptions{
			PropagationPolicy: &propagation,
		}); err != nil {
			return fmt.Errorf("delete job %s: %w", job.Name, err)
		}
	}
	return nil
}

// WaitForPodsGone waits until no pods matching the labelSelector exist in the namespace.
// Use this at the start of a test to ensure pods from a previous test have fully terminated.
func WaitForPodsGone(ctx context.Context, clientset kubernetes.Interface, namespace, labelSelector string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return fmt.Errorf("listing pods: %w", err)
		}
		if len(pods.Items) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("pods with selector %q still exist after %v", labelSelector, timeout)
}

// WaitForHTTPReady polls url until it returns HTTP 200 or timeout elapses.
// Use this to verify that a service (e.g. the stub) is reachable before a test starts.
func WaitForHTTPReady(url string, timeout time.Duration) error {
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("endpoint %s not reachable after %v", url, timeout)
}
