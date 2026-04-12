// internal/designmd/parse_test.go
package designmd

import (
	"os"
	"testing"
)

func TestSplitSections(t *testing.T) {
	input := `# Design System Inspired by TestBrand

## 1. Visual Theme & Atmosphere

Dark mode first. Near-black canvas (#08090a) with violet accent (#5e6ad2).

## 2. Color Palette & Roles

### Primary
- **Background** (#08090a): Main surface
- **Text** (#f7f8f8): Body text
- **Accent** (#5e6ad2): Interactive elements

## 9. Agent Prompt Guide

Best for: developer tools, dark dashboards
`

	sections := SplitSections(input)
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d", len(sections))
	}
	if sections[0].Title != "Visual Theme & Atmosphere" {
		t.Errorf("section 0 title = %q", sections[0].Title)
	}
	if sections[0].StartLine != 3 {
		t.Errorf("section 0 start_line = %d, want 3", sections[0].StartLine)
	}
	if sections[2].Title != "Agent Prompt Guide" {
		t.Errorf("section 2 title = %q", sections[2].Title)
	}
}

func TestExtractMeta(t *testing.T) {
	input := `# Design System Inspired by TestBrand

## 1. Visual Theme & Atmosphere

Dark mode first. Near-black canvas (#08090a) with violet accent (#5e6ad2). Second sentence here.

## 2. Color Palette & Roles

### Primary
- **Background** (#08090a): Main surface
- **Text** (#f7f8f8): Body text
- **Accent** (#5e6ad2): Interactive elements

## 9. Agent Prompt Guide

Best for: developer tools, dark dashboards
`

	meta := ExtractMeta(input)
	if meta.Vibe == "" {
		t.Error("vibe is empty")
	}
	if meta.Vibe != "Dark mode first." {
		t.Errorf("vibe = %q", meta.Vibe)
	}
	if len(meta.Colors) != 3 {
		t.Errorf("expected 3 colors, got %d: %v", len(meta.Colors), meta.Colors)
	}
	if meta.Colors[0] != "#08090a" {
		t.Errorf("color 0 = %q", meta.Colors[0])
	}
	if meta.BestFor == "" {
		t.Error("best_for is empty")
	}
}

func TestRealLinearDesignMD(t *testing.T) {
	path := "/home/krolik/tools/awesome-design-md/design-md/linear.app/DESIGN.md"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("DESIGN.md not found: %v", err)
	}

	content := string(data)
	sections := SplitSections(content)
	if len(sections) < 5 {
		t.Fatalf("expected at least 5 sections, got %d", len(sections))
	}

	meta := ExtractMeta(content)
	if meta.Vibe == "" {
		t.Error("vibe is empty for linear.app")
	}
	if len(meta.Colors) == 0 {
		t.Error("colors is empty for linear.app")
	}
	t.Logf("sections: %d, vibe: %q, colors: %v, best_for: %q",
		len(sections), meta.Vibe, meta.Colors, meta.BestFor)
}
