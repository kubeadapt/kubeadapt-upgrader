package backend

// UpdateCheckRequest is the request payload for checking available updates.
type UpdateCheckRequest struct {
	Chart          string `json:"chart"`
	CurrentVersion string `json:"current_version"`
	Policy         string `json:"policy"`
	Platform       string `json:"platform,omitempty"`
	Channel        string `json:"channel"`
}

// BlockedVersionInfo describes a version that is blocked from upgrading.
type BlockedVersionInfo struct {
	Version    string   `json:"version"`
	Reason     string   `json:"reason"`
	Name       *string  `json:"name,omitempty"`
	URL        *string  `json:"url,omitempty"`
	Platforms  []string `json:"platforms,omitempty"`
	RedirectTo *string  `json:"redirect_to,omitempty"`
}

// UpdateCheckResponse is the response payload from checking for available updates.
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

// UpdateStatusReport is the request payload for reporting upgrade status.
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
