package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// UpdateStatusReport mirrors backend.UpdateStatusReport for test assertions.
// Kept independent of internal/backend/types.go to avoid coupling helpers to production code.
type UpdateStatusReport struct {
	Chart           string         `json:"chart"`
	FromVersion     string         `json:"from_version"`
	ToVersion       string         `json:"to_version"`
	Status          string         `json:"status"`
	Platform        string         `json:"platform,omitempty"`
	JobName         string         `json:"job_name,omitempty"`
	JobNamespace    string         `json:"job_namespace,omitempty"`
	ErrorMessage    string         `json:"error_message,omitempty"`
	ErrorDetails    map[string]any `json:"error_details,omitempty"`
	DurationSeconds *int           `json:"duration_seconds,omitempty"`
}

// StubClient provides methods to interact with the stub backend from E2E tests.
type StubClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewStubClient creates a StubClient pointing at the given stub backend URL.
func NewStubClient(baseURL string) *StubClient {
	return &StubClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// SetMode sets the stub response mode: "update_available", "no_update", "blocked", "error_500".
func (c *StubClient) SetMode(mode string) error {
	body := map[string]string{"mode": mode}
	return c.postJSON("/stub/updates/config", body, nil)
}

// GetCheckRequests returns all check requests received by the stub.
func (c *StubClient) GetCheckRequests() ([]json.RawMessage, error) {
	var result []json.RawMessage
	if err := c.getJSON("/stub/updates/checks", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetReportRequests returns all report requests received by the stub.
func (c *StubClient) GetReportRequests() ([]UpdateStatusReport, error) {
	var result []UpdateStatusReport
	if err := c.getJSON("/stub/updates/reports", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// WaitForReports polls until at least minCount reports are received or timeout.
func (c *StubClient) WaitForReports(minCount int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		reports, err := c.GetReportRequests()
		if err == nil && len(reports) >= minCount {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("expected at least %d reports, timeout after %v", minCount, timeout)
}

// Flush clears all stored requests in the stub.
func (c *StubClient) Flush() error {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/stub/flush", nil)
	if err != nil {
		return fmt.Errorf("create flush request: %w", err)
	}
	//nolint:gosec // intentional: E2E test stub client making HTTP calls
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("flush stub: %w", err)
	}
	resp.Body.Close()
	return nil
}

func (c *StubClient) postJSON(path string, body, out interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}
	resp, err := c.httpClient.Post(c.baseURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("post to %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d from %s", resp.StatusCode, path)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *StubClient) getJSON(path string, out interface{}) error {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("get %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d from %s", resp.StatusCode, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
