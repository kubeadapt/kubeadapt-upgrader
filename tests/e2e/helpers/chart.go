package helpers

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// PackageChart copies the chart from chartDir to a temp directory, patches the version field
// in Chart.yaml, runs `helm package`, and returns the path to the generated .tgz file.
func PackageChart(chartDir, outputDir, version string) (string, error) {
	// Create temp directory for chart copy
	tempDir, err := os.MkdirTemp("", "chart-package-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Copy chart directory to temp location
	chartCopyDir := filepath.Join(tempDir, "kubeadapt")
	if err := copyDir(chartDir, chartCopyDir); err != nil {
		return "", fmt.Errorf("copying chart: %w", err)
	}

	// Read Chart.yaml
	chartYamlPath := filepath.Join(chartCopyDir, "Chart.yaml")
	chartContent, err := os.ReadFile(chartYamlPath)
	if err != nil {
		return "", fmt.Errorf("reading Chart.yaml: %w", err)
	}

	// Parse and update version
	var chartData map[string]interface{}
	if err := yaml.Unmarshal(chartContent, &chartData); err != nil {
		return "", fmt.Errorf("parsing Chart.yaml: %w", err)
	}

	chartData["version"] = version

	// Marshal back to YAML
	updatedContent, err := yaml.Marshal(chartData)
	if err != nil {
		return "", fmt.Errorf("marshaling Chart.yaml: %w", err)
	}

	// Write updated Chart.yaml
	if err := os.WriteFile(chartYamlPath, updatedContent, 0644); err != nil {
		return "", fmt.Errorf("writing Chart.yaml: %w", err)
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("creating output dir: %w", err)
	}

	// Run helm package
	cmd := exec.Command("helm", "package", chartCopyDir, "--version", version, "--destination", outputDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("helm package failed: %w\noutput: %s", err, string(output))
	}

	// Return path to packaged chart
	chartPath := filepath.Join(outputDir, fmt.Sprintf("kubeadapt-%s.tgz", version))
	return chartPath, nil
}

// UploadToChartmuseum uploads a packaged chart (.tgz file) to a chartmuseum instance.
// It POSTs the file as multipart form data to museumURL/api/charts and expects HTTP 201.
func UploadToChartmuseum(chartPath, museumURL string) error {
	// Open chart file
	file, err := os.Open(chartPath)
	if err != nil {
		return fmt.Errorf("opening chart file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add chart file to form
	part, err := writer.CreateFormFile("chart", filepath.Base(chartPath))
	if err != nil {
		return fmt.Errorf("creating form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("copying file to form: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("closing multipart writer: %w", err)
	}

	// POST to chartmuseum
	uploadURL := museumURL + "/api/charts"
	resp, err := http.Post(uploadURL, writer.FormDataContentType(), body)
	if err != nil {
		return fmt.Errorf("posting to chartmuseum: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chartmuseum upload failed: expected 201, got %d\nresponse: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// copyDir recursively copies a directory from src to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}

		// Copy file
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = srcFile.Close() }()

		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer func() { _ = dstFile.Close() }()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}
