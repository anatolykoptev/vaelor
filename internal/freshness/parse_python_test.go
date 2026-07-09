package freshness

import (
	"testing"
)

func TestParsePyProject_Full(t *testing.T) {
	t.Parallel()
	input := `[project]
name = "my-project"
requires-python = ">=3.11"
dependencies = [
    "flask>=3.0.0",
    "requests==2.31.0",
]

[project.optional-dependencies]
dev = ["pytest>=7.0"]
`
	info := ParsePyProject([]byte(input))

	if info.Language != "python" {
		t.Errorf("Language = %q, want %q", info.Language, "python")
	}
	if info.RuntimeVersion != ">=3.11" {
		t.Errorf("RuntimeVersion = %q, want %q", info.RuntimeVersion, ">=3.11")
	}
	if len(info.Dependencies) != 2 {
		t.Errorf("Dependencies count = %d, want 2", len(info.Dependencies))
	}
}

func TestParsePyProject_InlineArray(t *testing.T) {
	t.Parallel()
	input := `[project]
dependencies = ["click>=8.0", "rich>=13.0"]
`
	info := ParsePyProject([]byte(input))
	if len(info.Dependencies) != 2 {
		t.Errorf("Dependencies count = %d, want 2", len(info.Dependencies))
	}
}

func TestParseRequirementsTxt(t *testing.T) {
	t.Parallel()
	input := `# This is a comment
flask==3.0.0
requests>=2.28.0
gunicorn~=21.2.0

-e git+https://github.com/foo/bar.git#egg=bar
-r other-requirements.txt
--index-url https://pypi.org/simple
click
`
	info := ParseRequirementsTxt([]byte(input))

	if info.Language != "python" {
		t.Errorf("Language = %q, want %q", info.Language, "python")
	}

	// flask, requests, gunicorn, click = 4 deps.
	wantDeps := 4
	if len(info.Dependencies) != wantDeps {
		t.Fatalf("Dependencies count = %d, want %d", len(info.Dependencies), wantDeps)
	}

	// Verify versions are parsed correctly.
	for _, dep := range info.Dependencies {
		switch dep.Name {
		case "flask":
			if dep.Version != "3.0.0" {
				t.Errorf("flask version = %q, want %q", dep.Version, "3.0.0")
			}
		case "click":
			if dep.Version != "" {
				t.Errorf("click version = %q, want empty", dep.Version)
			}
		}
	}
}

func TestParseRequirementsTxt_Empty(t *testing.T) {
	t.Parallel()
	info := ParseRequirementsTxt([]byte(""))
	if len(info.Dependencies) != 0 {
		t.Errorf("Dependencies count = %d, want 0", len(info.Dependencies))
	}
}

func TestParsePythonRequirement_Extras(t *testing.T) {
	t.Parallel()
	dep := parsePythonRequirement("requests[security]>=2.28.0")
	if dep.Name != "requests" {
		t.Errorf("Name = %q, want %q", dep.Name, "requests")
	}
	if dep.Version != "2.28.0" {
		t.Errorf("Version = %q, want %q", dep.Version, "2.28.0")
	}
}
