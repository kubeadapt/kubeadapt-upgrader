package platform

import (
	"context"
	"io"
	"net/http"
	"time"
)

// Platform represents a cloud or on-premise platform
type Platform string

// Platform constants for supported cloud providers and on-premise deployments
const (
	PlatformEKS       Platform = "eks"
	PlatformAKS       Platform = "aks"
	PlatformGKE       Platform = "gke"
	PlatformOnPremise Platform = "on-premise"
)

// String returns the string representation of the platform
func (p Platform) String() string {
	return string(p)
}

// DetectPlatform detects the cloud platform by checking metadata endpoints
// Returns PlatformOnPremise if no cloud platform is detected
func DetectPlatform(ctx context.Context) Platform {
	// Create HTTP client with short timeout for metadata checks
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	// Check AWS EKS (metadata endpoint)
	if isAWS(ctx, client) {
		return PlatformEKS
	}

	// Check Azure AKS (IMDS endpoint)
	if isAzure(ctx, client) {
		return PlatformAKS
	}

	// Check GCP GKE (metadata endpoint)
	if isGCP(ctx, client) {
		return PlatformGKE
	}

	// Default to on-premise if no cloud platform detected
	return PlatformOnPremise
}

// isAWS checks if running on AWS by querying the EC2 metadata endpoint
func isAWS(ctx context.Context, client *http.Client) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://169.254.169.254/latest/meta-data/", nil)
	if err != nil {
		return false
	}

	// AWS metadata endpoint requires IMDSv2 token in production,
	// but also supports IMDSv1 (no token) for backward compatibility
	// This simple check works for both versions
	//nolint:gosec // intentional: AWS metadata endpoint check with hardcoded IP
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	// AWS metadata endpoint returns 200 OK
	return resp.StatusCode == http.StatusOK
}

// isAzure checks if running on Azure by querying the Azure IMDS endpoint
func isAzure(ctx context.Context, client *http.Client) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://169.254.169.254/metadata/instance?api-version=2021-02-01", nil)
	if err != nil {
		return false
	}

	// Azure requires the Metadata header
	req.Header.Set("Metadata", "true")

	//nolint:gosec // intentional: Azure IMDS endpoint check with hardcoded IP
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	// Azure IMDS returns 200 OK with the Metadata header
	return resp.StatusCode == http.StatusOK
}

// isGCP checks if running on GCP by querying the GCP metadata endpoint
func isGCP(ctx context.Context, client *http.Client) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://metadata.google.internal/computeMetadata/v1/", nil)
	if err != nil {
		return false
	}

	// GCP requires the Metadata-Flavor header
	req.Header.Set("Metadata-Flavor", "Google")

	//nolint:gosec // intentional: GCP metadata endpoint check with hardcoded hostname
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	// GCP metadata endpoint returns 200 OK with the Metadata-Flavor header
	return resp.StatusCode == http.StatusOK
}
