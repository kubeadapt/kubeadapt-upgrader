package cluster

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeployInfrastructure applies namespace, chartmuseum, and stub manifests in order,
// waiting for each deployment to become ready before proceeding.
//
// Port-forward processes are started for services that need to be reachable from the
// host (macOS). This replaces Kind extraPortMappings which are unreliable on macOS
// Docker Desktop (Apple Silicon) due to VPNKit tunnel instability.
func DeployInfrastructure(tc *TestCluster, testdataDir string) error {
	manifests := []struct {
		path       string
		waitHTTP   string // empty = no HTTP readiness check
		deployName string // empty = no deployment readiness check
		namespace  string
		localPort  int // 0 = no port-forward; host port to bind
		remotePort int // container port to forward to
	}{
		{filepath.Join(testdataDir, "namespace.yaml"), "", "", "", 0, 0},
		{filepath.Join(testdataDir, "chartmuseum.yaml"), "http://localhost:30083/health", "chartmuseum", "kubeadapt-system", 30083, 8080},
		{filepath.Join(testdataDir, "stub.yaml"), "http://localhost:30082/healthz", "upgrade-stub", "kubeadapt-system", 30082, 8080},
	}

	for _, m := range manifests {
		if err := tc.ApplyManifest(m.path); err != nil {
			return fmt.Errorf("apply %s: %w", m.path, err)
		}
		if m.deployName != "" {
			ctx := context.Background()
			if err := WaitForDeploymentReady(ctx, tc, m.namespace, m.deployName, 3*time.Minute); err != nil {
				return fmt.Errorf("wait for %s: %w", m.deployName, err)
			}
		}
		// Start port-forward BEFORE the HTTP readiness check so that localhost:localPort
		// is reachable. On macOS Docker Desktop, NodePort extraPortMappings are flaky;
		// kubectl port-forward is the reliable alternative.
		if m.localPort != 0 {
			resource := "svc/" + m.deployName
			if err := tc.StartPortForward(m.namespace, resource, m.localPort, m.remotePort); err != nil {
				return fmt.Errorf("start port-forward for %s: %w", m.deployName, err)
			}
		}
		if m.waitHTTP != "" {
			if err := WaitForHTTPEndpoint(m.waitHTTP, http.StatusOK, 2*time.Minute); err != nil {
				return fmt.Errorf("wait for HTTP %s: %w", m.waitHTTP, err)
			}
		}
	}
	return nil
}

// WaitForDeploymentReady polls until the named deployment has at least one ready replica
// or the context/timeout is exceeded.
func WaitForDeploymentReady(ctx context.Context, tc *TestCluster, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		dep, err := tc.Clientset().AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil && dep.Status.ReadyReplicas >= 1 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled waiting for deployment %s/%s", namespace, name)
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("deployment %s/%s not ready after %v", namespace, name, timeout)
}

// WaitForHTTPEndpoint polls the given URL until it returns the expected HTTP status code
// or the timeout is exceeded.
func WaitForHTTPEndpoint(url string, expectedStatus int, timeout time.Duration) error {
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == expectedStatus {
				return nil
			}
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("endpoint %s not reachable with status %d after %v", url, expectedStatus, timeout)
}
