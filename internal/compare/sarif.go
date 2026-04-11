package compare

import "fmt"

const (
	sarifSchema  = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json"
	sarifVersion = "2.1.0"
	sarifTool    = "go-code"
	sarifToolVer = "1.0.0"
)

// SARIFReport is a minimal SARIF v2.1.0 report.
type SARIFReport struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []SARIFRun `json:"runs"`
}

// SARIFRun is a single analysis run.
type SARIFRun struct {
	Tool    SARIFTool     `json:"tool"`
	Results []SARIFResult `json:"results"`
}

// SARIFTool describes the analysis tool.
type SARIFTool struct {
	Driver SARIFDriver `json:"driver"`
}

// SARIFDriver is the tool driver metadata.
type SARIFDriver struct {
	Name    string      `json:"name"`
	Version string      `json:"version"`
	Rules   []SARIFRule `json:"rules,omitempty"`
}

// SARIFRule describes a rule that can produce results.
type SARIFRule struct {
	ID               string           `json:"id"`
	ShortDescription SARIFMessage     `json:"shortDescription"`
	DefaultConfig    *SARIFRuleConfig `json:"defaultConfiguration,omitempty"`
}

// SARIFRuleConfig holds the default severity for a rule.
type SARIFRuleConfig struct {
	Level string `json:"level"` // "error", "warning", "note"
}

// SARIFResult is a single finding produced by the tool.
type SARIFResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"` // "error", "warning", "note"
	Message   SARIFMessage    `json:"message"`
	Locations []SARIFLocation `json:"locations,omitempty"`
}

// SARIFMessage holds a plain-text message.
type SARIFMessage struct {
	Text string `json:"text"`
}

// SARIFLocation identifies where a result was found.
type SARIFLocation struct {
	PhysicalLocation SARIFPhysical `json:"physicalLocation"`
}

// SARIFPhysical identifies a file and optional region.
type SARIFPhysical struct {
	ArtifactLocation SARIFArtifact `json:"artifactLocation"`
	Region           *SARIFRegion  `json:"region,omitempty"`
}

// SARIFArtifact holds the URI of the artifact.
type SARIFArtifact struct {
	URI string `json:"uri"`
}

// SARIFRegion specifies a location within an artifact.
type SARIFRegion struct {
	StartLine int `json:"startLine"`
}

// BuildSARIF converts code health findings into a SARIF v2.1.0 report.
func BuildSARIF(
	repoName string,
	metrics RepoMetrics,
	quality *QualityIndicators,
	dataflow *DataflowStats,
	hotspots []HotspotFile,
	outliers Outliers,
) *SARIFReport {
	report := &SARIFReport{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []SARIFRun{{
			Tool: SARIFTool{Driver: SARIFDriver{
				Name:    sarifTool,
				Version: sarifToolVer,
				Rules:   sarifRules(),
			}},
		}},
	}

	run := &report.Runs[0]

	// Grade as a finding.
	run.Results = append(run.Results, SARIFResult{
		RuleID:  "code-health/grade",
		Level:   gradeToLevel(metrics.Grade),
		Message: SARIFMessage{Text: fmt.Sprintf("Code health grade: %s (score: %.0f/100)", metrics.Grade, metrics.Score)},
	})

	// Hotspot findings — only critical and high risk.
	for _, h := range hotspots {
		if h.Risk != "critical" && h.Risk != "high" {
			continue
		}
		run.Results = append(run.Results, SARIFResult{
			RuleID:  "code-health/hotspot",
			Level:   "warning",
			Message: SARIFMessage{Text: fmt.Sprintf("Maintenance hotspot: risk=%s, churn=%d, complexity=%.1f", h.Risk, h.Churn, h.Complexity)},
			Locations: []SARIFLocation{{
				PhysicalLocation: SARIFPhysical{ArtifactLocation: SARIFArtifact{URI: h.File}},
			}},
		})
	}

	// Quality findings from ox-codes.
	if quality != nil {
		if quality.PanicCount > 0 {
			run.Results = append(run.Results, SARIFResult{
				RuleID:  "code-health/panic",
				Level:   "error",
				Message: SARIFMessage{Text: fmt.Sprintf("%d panic() calls found", quality.PanicCount)},
			})
		}
		if quality.TodoCount > 0 {
			run.Results = append(run.Results, SARIFResult{
				RuleID:  "code-health/todo",
				Level:   "note",
				Message: SARIFMessage{Text: fmt.Sprintf("%d TODO/FIXME comments", quality.TodoCount)},
			})
		}
	}

	// Dataflow findings.
	if dataflow != nil && dataflow.TotalFindings > 0 {
		run.Results = append(run.Results, SARIFResult{
			RuleID:  "code-health/dead-code",
			Level:   "warning",
			Message: SARIFMessage{Text: fmt.Sprintf("%d dead stores, %d unused variables", dataflow.DeadStores, dataflow.UnusedVars)},
		})
	}

	// Outlier findings — worst-offending functions.
	appendOutlierResult(run, outliers.MaxCyclomatic, "cyclomatic complexity")
	appendOutlierResult(run, outliers.MaxCognitive, "cognitive complexity")
	appendOutlierResult(run, outliers.MaxFuncLines, "function length")
	appendOutlierResult(run, outliers.MaxNesting, "nesting depth")

	return report
}

// appendOutlierResult adds a SARIF result for an outlier function if it is set.
func appendOutlierResult(run *SARIFRun, o OutlierFunc, metric string) {
	if o.Name == "" {
		return
	}
	result := SARIFResult{
		RuleID:  "code-health/outlier",
		Level:   "warning",
		Message: SARIFMessage{Text: fmt.Sprintf("Outlier %s: %s (value=%d)", metric, o.Name, o.Value)},
	}
	if o.File != "" {
		result.Locations = []SARIFLocation{{
			PhysicalLocation: SARIFPhysical{
				ArtifactLocation: SARIFArtifact{URI: o.File},
				Region:           &SARIFRegion{StartLine: o.Line},
			},
		}}
	}
	run.Results = append(run.Results, result)
}

// gradeToLevel maps a code health grade to a SARIF severity level.
func gradeToLevel(grade string) string {
	switch grade {
	case "A", "B":
		return "note"
	case "C":
		return "warning"
	default:
		return "error"
	}
}

// sarifRules returns the static set of rules emitted by go-code.
func sarifRules() []SARIFRule {
	return []SARIFRule{
		{
			ID:               "code-health/grade",
			ShortDescription: SARIFMessage{Text: "Overall code health grade"},
		},
		{
			ID:               "code-health/hotspot",
			ShortDescription: SARIFMessage{Text: "Maintenance hotspot (high churn + complexity)"},
			DefaultConfig:    &SARIFRuleConfig{Level: "warning"},
		},
		{
			ID:               "code-health/panic",
			ShortDescription: SARIFMessage{Text: "panic() calls in production code"},
			DefaultConfig:    &SARIFRuleConfig{Level: "error"},
		},
		{
			ID:               "code-health/todo",
			ShortDescription: SARIFMessage{Text: "TODO/FIXME comments"},
			DefaultConfig:    &SARIFRuleConfig{Level: "note"},
		},
		{
			ID:               "code-health/dead-code",
			ShortDescription: SARIFMessage{Text: "Dead stores and unused variables"},
			DefaultConfig:    &SARIFRuleConfig{Level: "warning"},
		},
		{
			ID:               "code-health/outlier",
			ShortDescription: SARIFMessage{Text: "Code quality outlier (worst-offending function)"},
			DefaultConfig:    &SARIFRuleConfig{Level: "warning"},
		},
	}
}
