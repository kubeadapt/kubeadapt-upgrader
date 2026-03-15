package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestNewClient(t *testing.T) {
	logger := zap.NewNop()
	client := NewClient("https://api.test.io", "test-token", logger)

	if client.baseURL != "https://api.test.io" {
		t.Errorf("expected baseURL=https://api.test.io, got %v", client.baseURL)
	}
	if client.token != "test-token" {
		t.Errorf("expected token=test-token, got %v", client.token)
	}
	if client.httpClient == nil {
		t.Error("expected httpClient to be initialized")
	}
}

func TestCheckForUpdates_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %v", r.Method)
		}
		if r.URL.Path != "/api/v1/updates/check" {
			t.Errorf("expected path /api/v1/updates/check, got %v", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Authorization header with Bearer token")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json")
		}

		// Return mock response
		resp := UpdateCheckResponse{
			UpdateAvailable:    true,
			CurrentVersion:     "1.0.0",
			LatestVersion:      "1.1.0",
			RecommendedVersion: "1.1.0",
			ChangeType:         "minor",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	logger := zap.NewNop()
	client := NewClient(server.URL, "test-token", logger)

	req := &UpdateCheckRequest{
		Chart:          "kubeadapt",
		CurrentVersion: "1.0.0",
		Policy:         "minor",
		Channel:        "stable",
	}

	resp, err := client.CheckForUpdates(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.UpdateAvailable {
		t.Error("expected UpdateAvailable=true")
	}
	if resp.CurrentVersion != "1.0.0" {
		t.Errorf("expected CurrentVersion=1.0.0, got %v", resp.CurrentVersion)
	}
	if resp.LatestVersion != "1.1.0" {
		t.Errorf("expected LatestVersion=1.1.0, got %v", resp.LatestVersion)
	}
}

func TestCheckForUpdates_Error(t *testing.T) {
	// Create mock server that returns a non-retryable client error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer server.Close()

	logger := zap.NewNop()
	client := NewClient(server.URL, "test-token", logger)

	req := &UpdateCheckRequest{
		Chart:          "kubeadapt",
		CurrentVersion: "1.0.0",
	}

	_, err := client.CheckForUpdates(context.Background(), req)
	if err == nil {
		t.Error("expected error for 400 response")
	}
}

func TestReportUpdateStatus_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %v", r.Method)
		}
		if r.URL.Path != "/api/v1/updates/report" {
			t.Errorf("expected path /api/v1/updates/report, got %v", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Authorization header")
		}

		// Verify request body
		var report UpdateStatusReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if report.Status != "success" {
			t.Errorf("expected Status=success, got %v", report.Status)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := zap.NewNop()
	client := NewClient(server.URL, "test-token", logger)

	report := &UpdateStatusReport{
		Chart:       "kubeadapt",
		FromVersion: "1.0.0",
		ToVersion:   "1.1.0",
		Status:      "success",
		Platform:    "eks",
	}

	err := client.ReportUpdateStatus(context.Background(), report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReportUpdateStatus_Accepted(t *testing.T) {
	// Create mock server that returns 202 Accepted
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	logger := zap.NewNop()
	client := NewClient(server.URL, "test-token", logger)

	report := &UpdateStatusReport{
		Status: "in_progress",
	}

	err := client.ReportUpdateStatus(context.Background(), report)
	if err != nil {
		t.Fatalf("unexpected error for 202 response: %v", err)
	}
}

func TestReportUpdateStatus_Error(t *testing.T) {
	// Create mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer server.Close()

	logger := zap.NewNop()
	client := NewClient(server.URL, "test-token", logger)

	report := &UpdateStatusReport{
		Status: "invalid",
	}

	err := client.ReportUpdateStatus(context.Background(), report)
	if err == nil {
		t.Error("expected error for 400 response")
	}
}
