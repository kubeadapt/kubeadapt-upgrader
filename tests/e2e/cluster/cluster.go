package cluster

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// TestCluster wraps a Kind cluster with a Kubernetes client for E2E testing.
type TestCluster struct {
	Name         string
	KubeConfig   string
	clientset    kubernetes.Interface
	portForwards []*exec.Cmd
}

// NewTestCluster creates a Kind cluster with the given name and config, then initializes
// a Kubernetes client pointed at it. The kubeconfig is written to the OS temp directory.
func NewTestCluster(name, kindConfigPath string) (*TestCluster, error) {
	kubeconfigPath := filepath.Join(os.TempDir(), name+"-kubeconfig.yaml")

	// Remove any stale kubeconfig from a previous run to prevent port/context conflicts.
	_ = os.Remove(kubeconfigPath)

	cmd := exec.Command("kind", "create", "cluster",
		"--name", name,
		"--config", kindConfigPath,
		"--kubeconfig", kubeconfigPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("create kind cluster: %w", err)
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("build k8s config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create k8s client: %w", err)
	}

	return &TestCluster{Name: name, KubeConfig: kubeconfigPath, clientset: clientset}, nil
}

// StartPortForward starts a background kubectl port-forward process that maps
// localhost:localPort to resource:remotePort inside the cluster.
//
// On macOS Docker Desktop (Apple Silicon), NodePort extraPortMappings are unreliable
// after initial setup — the VPNKit tunnel flickers. kubectl port-forward uses the
// K8s API server as a relay, which is stable for the entire test run.
func (c *TestCluster) StartPortForward(namespace, resource string, localPort, remotePort int) error {
	cmd := exec.Command("kubectl",
		"--kubeconfig", c.KubeConfig,
		"--namespace", namespace,
		"port-forward",
		resource,
		fmt.Sprintf("%d:%d", localPort, remotePort),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start port-forward %s %d:%d: %w", resource, localPort, remotePort, err)
	}
	c.portForwards = append(c.portForwards, cmd)

	// Allow the port-forward process a moment to bind the local port before callers try it.
	time.Sleep(500 * time.Millisecond)
	return nil
}

// StopPortForwards kills all background kubectl port-forward processes.
func (c *TestCluster) StopPortForwards() {
	for _, cmd := range c.portForwards {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}
	c.portForwards = nil
}

// Teardown deletes the Kind cluster and stops any port-forward processes.
func (c *TestCluster) Teardown() error {
	c.StopPortForwards()
	cmd := exec.Command("kind", "delete", "cluster", "--name", c.Name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// LoadImage loads a local Docker image into the Kind cluster.
func (c *TestCluster) LoadImage(imageName string) error {
	cmd := exec.Command("kind", "load", "docker-image", imageName, "--name", c.Name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ExportLogs exports cluster logs to the given directory.
func (c *TestCluster) ExportLogs(dir string) error {
	cmd := exec.Command("kind", "export", "logs", dir, "--name", c.Name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Clientset returns the Kubernetes client for the cluster.
func (c *TestCluster) Clientset() kubernetes.Interface {
	return c.clientset
}

// ApplyManifest runs `kubectl apply -f <path>` against the cluster.
func (c *TestCluster) ApplyManifest(path string) error {
	cmd := exec.Command("kubectl", "--kubeconfig", c.KubeConfig, "apply", "-f", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
