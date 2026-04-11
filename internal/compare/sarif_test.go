package compare

import "testing"

func TestBuildSARIF(t *testing.T) {
	metrics := RepoMetrics{Grade: "C", Score: 65}
	hotspots := []HotspotFile{
		{File: "hot.go", Risk: "critical", Churn: 50, Complexity: 12.0},
	}
	report := BuildSARIF("test-repo", metrics, nil, nil, hotspots, Outliers{})
	if report.Version != "2.1.0" {
		t.Errorf("wrong version: %s", report.Version)
	}
	if len(report.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(report.Runs))
	}
	// Should have grade + hotspot = 2 results minimum.
	if len(report.Runs[0].Results) < 2 {
		t.Errorf("expected at least 2 results, got %d", len(report.Runs[0].Results))
	}
	// Grade should be warning (grade C).
	if report.Runs[0].Results[0].Level != "warning" {
		t.Errorf("expected warning for grade C, got %s", report.Runs[0].Results[0].Level)
	}
}

func TestBuildSARIF_GradeLevels(t *testing.T) {
	cases := []struct {
		grade string
		level string
	}{
		{"A", "note"},
		{"B", "note"},
		{"C", "warning"},
		{"D", "error"},
		{"F", "error"},
	}
	for _, tc := range cases {
		t.Run(tc.grade, func(t *testing.T) {
			m := RepoMetrics{Grade: tc.grade, Score: 50}
			r := BuildSARIF("repo", m, nil, nil, nil, Outliers{})
			if got := r.Runs[0].Results[0].Level; got != tc.level {
				t.Errorf("grade %s: expected level %s, got %s", tc.grade, tc.level, got)
			}
		})
	}
}

func TestBuildSARIF_QualityIndicators(t *testing.T) {
	m := RepoMetrics{Grade: "A", Score: 95}
	q := &QualityIndicators{PanicCount: 3, TodoCount: 7}
	r := BuildSARIF("repo", m, q, nil, nil, Outliers{})
	results := r.Runs[0].Results
	// grade + panic + todo = 3
	if len(results) < 3 {
		t.Fatalf("expected at least 3 results, got %d", len(results))
	}
	var hasPanic, hasTodo bool
	for _, res := range results {
		if res.RuleID == "code-health/panic" {
			hasPanic = true
		}
		if res.RuleID == "code-health/todo" {
			hasTodo = true
		}
	}
	if !hasPanic {
		t.Error("expected code-health/panic result")
	}
	if !hasTodo {
		t.Error("expected code-health/todo result")
	}
}

func TestBuildSARIF_Hotspot_SkipsModerate(t *testing.T) {
	m := RepoMetrics{Grade: "B", Score: 80}
	hotspots := []HotspotFile{
		{File: "moderate.go", Risk: "moderate", Churn: 5, Complexity: 3.0},
		{File: "high.go", Risk: "high", Churn: 20, Complexity: 8.0},
	}
	r := BuildSARIF("repo", m, nil, nil, hotspots, Outliers{})
	// Only grade + high hotspot; moderate is skipped.
	if len(r.Runs[0].Results) != 2 {
		t.Errorf("expected 2 results (grade + high hotspot), got %d", len(r.Runs[0].Results))
	}
}

func TestBuildSARIF_Outliers(t *testing.T) {
	m := RepoMetrics{Grade: "D", Score: 40}
	out := Outliers{
		MaxCyclomatic: OutlierFunc{Name: "BigFunc", File: "big.go", Line: 42, Value: 25},
	}
	r := BuildSARIF("repo", m, nil, nil, nil, out)
	var hasOutlier bool
	for _, res := range r.Runs[0].Results {
		if res.RuleID == "code-health/outlier" {
			hasOutlier = true
			if len(res.Locations) == 0 {
				t.Error("outlier result should have a location")
			}
		}
	}
	if !hasOutlier {
		t.Error("expected code-health/outlier result")
	}
}

func TestBuildSARIF_Schema(t *testing.T) {
	m := RepoMetrics{Grade: "A", Score: 100}
	r := BuildSARIF("repo", m, nil, nil, nil, Outliers{})
	if r.Schema == "" {
		t.Error("SARIF schema URI must not be empty")
	}
	if r.Runs[0].Tool.Driver.Name != "go-code" {
		t.Errorf("unexpected tool name: %s", r.Runs[0].Tool.Driver.Name)
	}
	if len(r.Runs[0].Tool.Driver.Rules) == 0 {
		t.Error("driver should declare rules")
	}
}
