package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPlatformString(t *testing.T) {
	testCases := []struct {
		platform Platform
		expected string
	}{
		{PlatformEKS, "eks"},
		{PlatformAKS, "aks"},
		{PlatformGKE, "gke"},
		{PlatformOnPremise, "on-premise"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			if tc.platform.String() != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, tc.platform.String())
			}
		})
	}
}

func TestDetectPlatform_OnPremise(t *testing.T) {
	// Without any mock servers, all checks will fail, returning on-premise
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	platform := DetectPlatform(ctx)

	if platform != PlatformOnPremise {
		t.Errorf("expected on-premise, got %v", platform)
	}
}

func TestIsAWS(t *testing.T) {
	// Create mock AWS metadata server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest/meta-data/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// We can't easily test isAWS directly since it uses hardcoded IP
	// But we can verify the function logic via DetectPlatform with mocks

	// Instead, test that on-premise is returned when server is unavailable
	client := &http.Client{Timeout: 100 * time.Millisecond}
	ctx := context.Background()

	// Test with unavailable endpoint
	result := isAWS(ctx, client)
	if result {
		t.Error("expected isAWS=false when endpoint is unavailable")
	}
}

func TestIsAzure(t *testing.T) {
	// Test with unavailable endpoint
	client := &http.Client{Timeout: 100 * time.Millisecond}
	ctx := context.Background()

	result := isAzure(ctx, client)
	if result {
		t.Error("expected isAzure=false when endpoint is unavailable")
	}
}

func TestIsGCP(t *testing.T) {
	// Test with unavailable endpoint
	client := &http.Client{Timeout: 100 * time.Millisecond}
	ctx := context.Background()

	result := isGCP(ctx, client)
	if result {
		t.Error("expected isGCP=false when endpoint is unavailable")
	}
}

func TestDetectPlatform_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	platform := DetectPlatform(ctx)

	// Should return on-premise since all checks fail
	if platform != PlatformOnPremise {
		t.Errorf("expected on-premise for canceled context, got %v", platform)
	}
}
