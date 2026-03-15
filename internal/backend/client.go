package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"go.uber.org/zap"
)

const (
	maxRetries     = 3
	baseRetryDelay = 2 * time.Second
)

// Client is an HTTP client for communicating with the Kubeadapt backend API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewClient creates a new backend API client.
func NewClient(baseURL, token string, logger *zap.Logger) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger.With(zap.String("component", "backend_client")),
	}
}

// CheckForUpdates queries the backend for available chart updates.
func (c *Client) CheckForUpdates(ctx context.Context, req *UpdateCheckRequest) (*UpdateCheckResponse, error) {
	endpoint := fmt.Sprintf("%s/api/v1/updates/check", c.baseURL)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var result UpdateCheckResponse
	err = c.doWithRetry(ctx, http.MethodPost, endpoint, body, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// ReportUpdateStatus sends an upgrade status report to the backend.
func (c *Client) ReportUpdateStatus(ctx context.Context, report *UpdateStatusReport) error {
	endpoint := fmt.Sprintf("%s/api/v1/updates/report", c.baseURL)

	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	return c.doWithRetry(ctx, http.MethodPost, endpoint, body, nil)
}

func (c *Client) doWithRetry(ctx context.Context, method, endpoint string, body []byte, dest any) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := baseRetryDelay * time.Duration(math.Pow(2, float64(attempt-1)))
			c.logger.Debug("Retrying request",
				zap.Int("attempt", attempt),
				zap.Duration("delay", delay),
				zap.String("endpoint", endpoint))

			select {
			case <-ctx.Done():
				return fmt.Errorf("context canceled during retry: %w", ctx.Err())
			case <-time.After(delay):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.token)

		//nolint:gosec // intentional: HTTP client making API calls to configured backend endpoint
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("execute request: %w", err)
			continue
		}

		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if readErr != nil {
			lastErr = fmt.Errorf("read response body: %w", readErr)
			continue
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error %d: %s", resp.StatusCode, string(respBody))
			continue
		}

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(respBody))
		}

		if dest != nil {
			if err := json.Unmarshal(respBody, dest); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
		}

		return nil
	}

	return fmt.Errorf("request failed after %d retries: %w", maxRetries, lastErr)
}
