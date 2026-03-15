package helpers

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// InstallChart installs a Helm chart using the helm CLI.
func InstallChart(kubeconfig, namespace, releaseName, chartmuseumURL, version string, values map[string]string) error {
	args := make([]string, 0, 14+2*len(values))
	args = append(args,
		"upgrade", "--install",
		"--kubeconfig", kubeconfig,
		"--namespace", namespace,
		"--create-namespace",
		"--version", version,
		"--repo", chartmuseumURL,
		"--wait", "--timeout", "3m",
	)
	for k, v := range values {
		args = append(args, "--set", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, releaseName, "kubeadapt")
	cmd := exec.Command("helm", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("helm install: %w\nOutput: %s", err, string(out))
	}
	return nil
}

// UninstallChart removes a Helm release.
func UninstallChart(kubeconfig, namespace, releaseName string) error {
	cmd := exec.Command("helm", "uninstall",
		"--kubeconfig", kubeconfig,
		"--namespace", namespace,
		"--wait",
		releaseName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Don't fail if release doesn't exist
		if strings.Contains(string(out), "not found") {
			return nil
		}
		return fmt.Errorf("helm uninstall: %w\nOutput: %s", err, string(out))
	}
	return nil
}

// helmListEntry represents a single entry from `helm list --output json`.
type helmListEntry struct {
	Chart string `json:"chart"` // e.g., "kubeadapt-0.18.0"
}

// GetReleaseVersion returns the deployed chart version for a Helm release.
// Uses `helm list` which returns [{"chart":"kubeadapt-0.18.0",...}].
func GetReleaseVersion(kubeconfig, namespace, releaseName string) (string, error) {
	cmd := exec.Command("helm", "list",
		"--kubeconfig", kubeconfig,
		"--namespace", namespace,
		"--filter", releaseName,
		"--output", "json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("helm list: %w\nOutput: %s", err, string(out))
	}

	var entries []helmListEntry
	if err := parseJSON(out, &entries); err != nil {
		return "", fmt.Errorf("parse helm list output: %w", err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("release %q not found in namespace %q", releaseName, namespace)
	}

	// Extract version from chart name using last hyphen (e.g., "kubeadapt-0.18.0" → "0.18.0")
	chart := entries[0].Chart
	idx := strings.LastIndex(chart, "-")
	if idx < 0 || idx == len(chart)-1 {
		return chart, nil
	}
	return chart[idx+1:], nil
}

func parseJSON(data []byte, out interface{}) error {
	return json.Unmarshal(data, out)
}
