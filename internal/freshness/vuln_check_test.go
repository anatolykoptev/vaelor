package freshness

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestOSVEcosystems_AllLanguages(t *testing.T) {
	languages := []string{"go", "npm", "typescript", "python", "rust", "java", "ruby", "csharp"}
	for _, lang := range languages {
		if _, ok := osvEcosystems[lang]; !ok {
			t.Errorf("missing ecosystem mapping for language %q", lang)
		}
	}
}

func TestOSVEcosystems_Values(t *testing.T) {
	expected := map[string]string{
		"go":         "Go",
		"npm":        "npm",
		"typescript": "npm",
		"python":     "PyPI",
		"rust":       "crates.io",
		"java":       "Maven",
		"ruby":       "RubyGems",
		"csharp":     "NuGet",
	}
	for lang, want := range expected {
		got := osvEcosystems[lang]
		assertEqual(t, want, got)
	}
}

func TestQueryOSV_Vulnerable(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		var req osvRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		resp := `{"vulns":[{"id":"GHSA-1234","summary":"test vuln","database_specific":{"severity":"HIGH"}}]}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	})
	defer srv.Close()

	vulns, err := queryOSV(context.Background(), srv.Client(), srv.URL, "lodash", "4.17.0", "npm")
	assertNoError(t, err)
	if len(vulns) != 1 {
		t.Fatalf("expected 1 vuln, got %d", len(vulns))
	}
	assertEqual(t, "GHSA-1234", vulns[0].ID)
	assertEqual(t, "HIGH", vulns[0].Severity)
}

func TestQueryOSV_Clean(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"vulns":[]}`))
	})
	defer srv.Close()

	vulns, err := queryOSV(context.Background(), srv.Client(), srv.URL, "safe-pkg", "1.0.0", "npm")
	assertNoError(t, err)
	if len(vulns) != 0 {
		t.Errorf("expected 0 vulns, got %d", len(vulns))
	}
}

func TestQueryOSV_NoVulnsField(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	defer srv.Close()

	vulns, err := queryOSV(context.Background(), srv.Client(), srv.URL, "pkg", "1.0.0", "Go")
	assertNoError(t, err)
	if len(vulns) != 0 {
		t.Errorf("expected 0 vulns, got %d", len(vulns))
	}
}

func TestExtractSeverity_CVSS(t *testing.T) {
	tests := []struct {
		name   string
		vector string
		want   string
	}{
		{"critical", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H/E:H", "CRITICAL"},
		{"high", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:L", "HIGH"},
		{"medium", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:L/A:L", "MEDIUM"},
		{"low", "CVSS:3.1/AV:L/AC:L/PR:N/UI:N/S:U/C:L/I:L/A:L", "LOW"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := osvVuln{Severity: []osvSeverity{{Type: "CVSS_V3", Score: tt.vector}}}
			got := extractSeverity(v)
			assertEqual(t, tt.want, got)
		})
	}
}

func TestExtractSeverity_DatabaseSpecific(t *testing.T) {
	v := osvVuln{
		Severity:         []osvSeverity{{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H/E:H"}},
		DatabaseSpecific: &osvDBSpecific{Severity: "LOW"},
	}
	got := extractSeverity(v)
	assertEqual(t, "LOW", got)
}

func TestCheckVulnerabilities_Mixed(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		var req osvRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if req.Package.Name == "vuln-pkg" {
			_, _ = w.Write([]byte(`{"vulns":[{"id":"CVE-2024-001","summary":"bad","database_specific":{"severity":"HIGH"}}]}`))
		} else {
			_, _ = w.Write([]byte(`{"vulns":[]}`))
		}
	})
	defer srv.Close()

	deps := []Dependency{
		{Name: "vuln-pkg", Version: "1.0.0", Language: "npm"},
		{Name: "safe-a", Version: "2.0.0", Language: "npm"},
		{Name: "safe-b", Version: "3.0.0", Language: "python"},
	}
	result := CheckVulnerabilities(context.Background(), deps, srv.Client(), srv.URL)
	if result.Total != 3 {
		t.Errorf("total: got %d, want 3", result.Total)
	}
	if result.Vulnerable != 1 {
		t.Errorf("vulnerable: got %d, want 1", result.Vulnerable)
	}
	if result.High != 1 {
		t.Errorf("high: got %d, want 1", result.High)
	}
}

func TestCheckVulnerabilities_Empty(t *testing.T) {
	result := CheckVulnerabilities(context.Background(), nil, http.DefaultClient, DefaultOSVURL)
	if result.Ratio != 1.0 {
		t.Errorf("ratio: got %f, want 1.0", result.Ratio)
	}
	if result.Total != 0 {
		t.Errorf("total: got %d, want 0", result.Total)
	}
}

func TestCheckVulnerabilities_UnknownLanguage(t *testing.T) {
	deps := []Dependency{
		{Name: "cobol-pkg", Version: "1.0.0", Language: "cobol"},
	}
	result := CheckVulnerabilities(context.Background(), deps, http.DefaultClient, DefaultOSVURL)
	if result.Total != 0 {
		t.Errorf("total: got %d, want 0 (unknown language should be skipped)", result.Total)
	}
}

func TestCheckVulnerabilities_AllClean(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"vulns":[]}`))
	})
	defer srv.Close()

	deps := []Dependency{
		{Name: "pkg-a", Version: "1.0.0", Language: "go"},
		{Name: "pkg-b", Version: "2.0.0", Language: "rust"},
	}
	result := CheckVulnerabilities(context.Background(), deps, srv.Client(), srv.URL)
	if result.Vulnerable != 0 {
		t.Errorf("vulnerable: got %d, want 0", result.Vulnerable)
	}
	if result.Ratio != 1.0 {
		t.Errorf("ratio: got %f, want 1.0", result.Ratio)
	}
}
