package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/policy"
	"github.com/anatolykoptev/go-code/internal/review"
)

func TestRenderReview(t *testing.T) {
	r := &review.DeltaResult{
		Risk: review.RiskGuidance{RiskLevel: "medium", RiskScore: 0.5, Flags: []string{"touches api"}},
		UntestedSymbols: []string{"Svc.Foo"},
	}
	findings := []policy.Finding{
		{Path: "main.go", Line: 10, Rule: "forbidden_import", Message: "use stdlib"},
	}
	body, comments := renderReview(r, findings)
	if !strings.Contains(body, "medium") {
		t.Fatal("body missing risk")
	}
	if len(comments) != 1 || comments[0].Path != "main.go" {
		t.Fatalf("bad comments: %+v", comments)
	}
	// Check the type of the comment
	_ = forge.InlineComment{}
}
