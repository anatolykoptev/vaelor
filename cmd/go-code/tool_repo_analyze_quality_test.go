package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
)

// TestApplyExtras_QualityStatsRendered asserts that QualityStats set on
// extras propagates to the rendered XML. The wiring matters because we
// share the formatter path with the empty-arch_central case (PR #16/#19)
// and the omitempty guard depends on a non-nil pointer.
func TestApplyExtras_QualityStatsRendered(t *testing.T) {
	r := fixtureResult()
	extras := &repoAnalysisExtras{
		QualityStats: &xmlQualitySummary{
			QualityFindings:  17,
			SecurityFindings: 3,
			FilesAnalyzed:    532,
			Language:         "go",
		},
	}

	out := formatAnalysisXML(r, analyze.DepthDeep, extras)

	for _, want := range []string{
		`qualityFindings="17"`,
		`securityFindings="3"`,
		`filesAnalyzed="532"`,
		`language="go"`,
		`<quality `,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
}

// TestApplyExtras_QualityStatsOmittedWhenNil asserts that the <quality>
// element is absent when QualityStats is nil — the same pattern as the
// arch_central omitempty fix in PR #16. Agents read absence as "no
// dataflow signal collected" without needing an empty tag.
func TestApplyExtras_QualityStatsOmittedWhenNil(t *testing.T) {
	out := formatAnalysisXML(fixtureResult(), analyze.DepthDeep, &repoAnalysisExtras{})
	if strings.Contains(out, "<quality") {
		t.Fatalf("nil QualityStats must not emit <quality> element:\n%s", out)
	}
}
