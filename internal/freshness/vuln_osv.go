package freshness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// osvRequest is the POST body for the OSV.dev query endpoint.
type osvRequest struct {
	Package osvPackage `json:"package"`
	Version string     `json:"version"`
}

// osvPackage identifies a package in the OSV API.
type osvPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

// osvResponse is the response from the OSV.dev query endpoint.
type osvResponse struct {
	Vulns []osvVuln `json:"vulns"`
}

// osvVuln represents a single vulnerability from OSV.
type osvVuln struct {
	ID               string         `json:"id"`
	Summary          string         `json:"summary"`
	Severity         []osvSeverity  `json:"severity"`
	DatabaseSpecific *osvDBSpecific `json:"database_specific"`
}

// osvSeverity holds a severity score from OSV.
type osvSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

// osvDBSpecific holds database-specific metadata from OSV.
type osvDBSpecific struct {
	Severity string `json:"severity"`
}

// queryOSV sends a vulnerability query to OSV.dev and returns matching vulns.
func queryOSV(
	ctx context.Context,
	client *http.Client,
	url, name, version, ecosystem string,
) ([]VulnDep, error) {
	body, err := json.Marshal(osvRequest{
		Package: osvPackage{Name: name, Ecosystem: ecosystem},
		Version: version,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling osv request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating osv request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying osv: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("osv returned %d", resp.StatusCode)
	}

	var osvResp osvResponse
	if err := json.NewDecoder(resp.Body).Decode(&osvResp); err != nil {
		return nil, fmt.Errorf("decoding osv response: %w", err)
	}

	vulns := make([]VulnDep, 0, len(osvResp.Vulns))
	for _, v := range osvResp.Vulns {
		vulns = append(vulns, VulnDep{
			Name:     name,
			Version:  version,
			ID:       v.ID,
			Severity: extractSeverity(v),
			Summary:  v.Summary,
		})
	}
	return vulns, nil
}

// extractSeverity determines severity from an OSV vuln entry.
// Priority: database_specific.severity > CVSS_V3 vector > "MEDIUM" default.
func extractSeverity(v osvVuln) string {
	if v.DatabaseSpecific != nil && v.DatabaseSpecific.Severity != "" {
		return v.DatabaseSpecific.Severity
	}
	for _, s := range v.Severity {
		if s.Type == "CVSS_V3" && s.Score != "" {
			return extractSeverityFromCVSS(s.Score)
		}
	}
	return sevMedium
}

// CVSS high-impact thresholds for severity classification.
const (
	cvssThresholdCritical = 4
	cvssThresholdHigh     = 2
)

// extractSeverityFromCVSS infers severity from a CVSS v3 vector string.
// Counts occurrences of ":H/" and ":H" at end to estimate severity.
func extractSeverityFromCVSS(vector string) string {
	count := strings.Count(vector, ":H/")
	if strings.HasSuffix(vector, ":H") {
		count++
	}

	switch {
	case count >= cvssThresholdCritical:
		return sevCritical
	case count >= cvssThresholdHigh:
		return sevHigh
	case count >= 1:
		return sevMedium
	default:
		return sevLow
	}
}
