package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

// --- Types (redefined from internal/backend/types.go to keep stub independent) ---

type UpdateCheckRequest struct {
	Chart          string `json:"chart"`
	CurrentVersion string `json:"current_version"`
	Policy         string `json:"policy"`
	Platform       string `json:"platform,omitempty"`
	Channel        string `json:"channel"`
}

type BlockedVersionInfo struct {
	Version   string   `json:"version"`
	Reason    string   `json:"reason"`
	Name      *string  `json:"name,omitempty"`
	URL       *string  `json:"url,omitempty"`
	Platforms []string `json:"platforms,omitempty"`
}

type UpdateCheckResponse struct {
	UpdateAvailable    bool                 `json:"update_available"`
	CurrentVersion     string               `json:"current_version"`
	LatestVersion      string               `json:"latest_version"`
	RecommendedVersion string               `json:"recommended_version,omitempty"`
	BlockedVersions    []BlockedVersionInfo `json:"blocked_versions"`
	UpgradePath        []string             `json:"upgrade_path,omitempty"`
	ChangeType         string               `json:"change_type,omitempty"`
	ReleaseNotesURL    string               `json:"release_notes_url,omitempty"`
	Message            string               `json:"message,omitempty"`
}

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

// --- Stub config endpoint types ---

type ConfigRequest struct {
	Mode     string          `json:"mode"`
	Response json.RawMessage `json:"response,omitempty"`
}

// --- Stub server ---

type StubServer struct {
	mu         sync.RWMutex
	mode       string
	customResp json.RawMessage
	checkReqs  []json.RawMessage
	reportReqs []json.RawMessage
}

func main() {
	s := &StubServer{mode: "update_available"}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/updates/check", s.handleCheck)
	mux.HandleFunc("/api/v1/updates/report", s.handleReport)
	mux.HandleFunc("/stub/updates/config", s.handleConfig)
	mux.HandleFunc("/stub/updates/checks", s.handleGetChecks)
	mux.HandleFunc("/stub/updates/reports", s.handleGetReports)
	mux.HandleFunc("/stub/flush", s.handleFlush)
	mux.HandleFunc("/healthz", s.handleHealthz)

	port := getEnv("STUB_PORT", "8080")
	log.Printf("Stub server starting on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// --- API handlers (mimics ingestion-api) ---

func (s *StubServer) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// Warn if Authorization header missing, but don't reject
	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		log.Printf("WARNING: missing or invalid Authorization header: %q", auth)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}
	defer r.Body.Close()

	// Parse-verify the body is a valid UpdateCheckRequest
	var req UpdateCheckRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	// Store raw JSON
	s.mu.Lock()
	s.checkReqs = append(s.checkReqs, json.RawMessage(body))
	mode := s.mode
	customResp := s.customResp
	s.mu.Unlock()

	log.Printf("POST /api/v1/updates/check \u2014 mode=%s chart=%s version=%s", mode, req.Chart, req.CurrentVersion)

	// Respond based on current mode
	if mode == "sequential_upgrade" {
		resp, statusCode := s.buildSequentialUpgradeResponse(req.CurrentVersion)
		writeJSON(w, statusCode, resp)
		return
	}
	resp, statusCode := s.buildCheckResponse(mode, customResp, req.Channel)
	writeJSON(w, statusCode, resp)
}

func (s *StubServer) handleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		log.Printf("WARNING: missing or invalid Authorization header: %q", auth)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}
	defer r.Body.Close()

	// Parse-verify the body is a valid UpdateStatusReport
	var report UpdateStatusReport
	if err := json.Unmarshal(body, &report); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	// Store raw JSON
	s.mu.Lock()
	s.reportReqs = append(s.reportReqs, json.RawMessage(body))
	s.mu.Unlock()

	log.Printf("POST /api/v1/updates/report \u2014 chart=%s %s\u2192%s status=%s", report.Chart, report.FromVersion, report.ToVersion, report.Status)

	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// --- Stub control endpoints ---

func (s *StubServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}
	defer r.Body.Close()

	var cfg ConfigRequest
	if err := json.Unmarshal(body, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid config: %v", err)})
		return
	}

	validModes := map[string]bool{
		"update_available":   true,
		"no_update":          true,
		"blocked":            true,
		"error_500":          true,
		"custom":             true,
		"channel_aware":      true,
		"upgrade_path":       true,
		"sequential_upgrade": true,
	}
	if !validModes[cfg.Mode] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid mode: %q, valid: update_available, no_update, blocked, error_500, custom, channel_aware, upgrade_path, sequential_upgrade", cfg.Mode)})
		return
	}

	if cfg.Mode == "custom" && len(cfg.Response) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "custom mode requires 'response' field"})
		return
	}

	s.mu.Lock()
	s.mode = cfg.Mode
	s.customResp = cfg.Response
	s.mu.Unlock()

	log.Printf("POST /stub/updates/config — mode set to %q", cfg.Mode)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "mode": cfg.Mode})
}

func (s *StubServer) handleGetChecks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	s.mu.RLock()
	reqs := s.checkReqs
	s.mu.RUnlock()

	if reqs == nil {
		reqs = []json.RawMessage{}
	}

	writeJSON(w, http.StatusOK, reqs)
}

func (s *StubServer) handleGetReports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	s.mu.RLock()
	reqs := s.reportReqs
	s.mu.RUnlock()

	if reqs == nil {
		reqs = []json.RawMessage{}
	}

	writeJSON(w, http.StatusOK, reqs)
}

func (s *StubServer) handleFlush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	s.mu.Lock()
	s.checkReqs = nil
	s.reportReqs = nil
	s.mu.Unlock()

	log.Printf("POST /stub/flush — all stored requests cleared")

	writeJSON(w, http.StatusOK, map[string]string{"status": "flushed"})
}

func (s *StubServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Response builders ---

func (s *StubServer) buildCheckResponse(mode string, customResp json.RawMessage, channel string) (any, int) {
	switch mode {
	case "update_available":
		return UpdateCheckResponse{
			UpdateAvailable:    true,
			CurrentVersion:     "0.17.0",
			LatestVersion:      "0.18.0",
			RecommendedVersion: "0.18.0",
			ChangeType:         "minor",
			BlockedVersions:    []BlockedVersionInfo{},
		}, http.StatusOK

	case "no_update":
		return UpdateCheckResponse{
			UpdateAvailable: false,
			CurrentVersion:  "0.17.0",
			LatestVersion:   "0.17.0",
			BlockedVersions: []BlockedVersionInfo{},
		}, http.StatusOK

	case "blocked":
		return UpdateCheckResponse{
			UpdateAvailable: false,
			CurrentVersion:  "0.17.0",
			LatestVersion:   "0.18.0",
			BlockedVersions: []BlockedVersionInfo{
				{
					Version: "0.18.0",
					Reason:  "Security vulnerability",
				},
			},
		}, http.StatusOK

	case "error_500":
		return map[string]string{"error": "internal server error"}, http.StatusInternalServerError

	case "custom":
		var resp any
		if err := json.Unmarshal(customResp, &resp); err != nil {
			return map[string]string{"error": "failed to unmarshal custom response"}, http.StatusInternalServerError
		}
		return resp, http.StatusOK
	case "channel_aware":
		switch channel {
		case "fast":
			return UpdateCheckResponse{
				UpdateAvailable:    true,
				CurrentVersion:     "0.17.0",
				LatestVersion:      "0.18.0",
				RecommendedVersion: "0.18.0",
				ChangeType:         "minor",
				BlockedVersions:    []BlockedVersionInfo{},
			}, http.StatusOK
		case "stable":
			msg := "No stable release available yet"
			return UpdateCheckResponse{
				UpdateAvailable: false,
				CurrentVersion:  "0.17.0",
				LatestVersion:   "0.17.0",
				Message:         msg,
				BlockedVersions: []BlockedVersionInfo{},
			}, http.StatusOK
		default:
			// Backward compatible: empty channel behaves like update_available
			return UpdateCheckResponse{
				UpdateAvailable:    true,
				CurrentVersion:     "0.17.0",
				LatestVersion:      "0.18.0",
				RecommendedVersion: "0.18.0",
				ChangeType:         "minor",
				BlockedVersions:    []BlockedVersionInfo{},
			}, http.StatusOK
		}

	case "upgrade_path":
		return UpdateCheckResponse{
			UpdateAvailable:    true,
			CurrentVersion:     "0.17.0",
			LatestVersion:      "0.18.0",
			RecommendedVersion: "0.18.0",
			UpgradePath:        []string{"0.17.0", "0.18.0"},
			ChangeType:         "minor",
			BlockedVersions:    []BlockedVersionInfo{},
		}, http.StatusOK

	default:
		return map[string]string{"error": fmt.Sprintf("unknown mode: %s", mode)}, http.StatusInternalServerError
	}
}

// buildSequentialUpgradeResponse returns a dynamic response based on the agent's current version.
// It simulates the ingestion-api's sequential path computation for the 0.17.0 → 0.17.1 → 0.18.0 path.
func (s *StubServer) buildSequentialUpgradeResponse(currentVersion string) (any, int) {
	switch currentVersion {
	case "0.17.0":
		return UpdateCheckResponse{
			UpdateAvailable:    true,
			CurrentVersion:     "0.17.0",
			LatestVersion:      "0.18.0",
			RecommendedVersion: "0.17.1",
			UpgradePath:        []string{"0.17.0", "0.17.1", "0.18.0"},
			ChangeType:         "patch",
			BlockedVersions:    []BlockedVersionInfo{},
		}, http.StatusOK
	case "0.17.1":
		return UpdateCheckResponse{
			UpdateAvailable:    true,
			CurrentVersion:     "0.17.1",
			LatestVersion:      "0.18.0",
			RecommendedVersion: "0.18.0",
			UpgradePath:        []string{"0.17.1", "0.18.0"},
			ChangeType:         "minor",
			BlockedVersions:    []BlockedVersionInfo{},
		}, http.StatusOK
	default:
		return UpdateCheckResponse{
			UpdateAvailable: false,
			CurrentVersion:  currentVersion,
			LatestVersion:   "0.18.0",
			BlockedVersions: []BlockedVersionInfo{},
		}, http.StatusOK
	}
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("ERROR: failed to encode JSON response: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
