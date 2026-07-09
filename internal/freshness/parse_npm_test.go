package freshness

import (
	"testing"
)

func TestParsePackageJSON_Full(t *testing.T) {
	t.Parallel()
	input := `{
  "name": "my-app",
  "engines": {"node": ">=18.0.0"},
  "dependencies": {
    "express": "^4.18.0",
    "lodash": "~4.17.21"
  },
  "devDependencies": {
    "typescript": "^5.3.0",
    "jest": "^29.0.0"
  }
}`
	info := ParsePackageJSON([]byte(input))

	if info.Language != "typescript" {
		t.Errorf("Language = %q, want %q", info.Language, "typescript")
	}
	if info.RuntimeVersion != ">=18.0.0" {
		t.Errorf("RuntimeVersion = %q, want %q", info.RuntimeVersion, ">=18.0.0")
	}

	wantDeps := 4
	if len(info.Dependencies) != wantDeps {
		t.Errorf("Dependencies count = %d, want %d", len(info.Dependencies), wantDeps)
	}
}

func TestParsePackageJSON_NoDeps(t *testing.T) {
	t.Parallel()
	input := `{"name": "empty"}`
	info := ParsePackageJSON([]byte(input))
	if len(info.Dependencies) != 0 {
		t.Errorf("Dependencies count = %d, want 0", len(info.Dependencies))
	}
}

func TestParsePackageJSON_InvalidJSON(t *testing.T) {
	t.Parallel()
	info := ParsePackageJSON([]byte(`{invalid`))
	if info.Language != "typescript" {
		t.Errorf("Language = %q, want %q", info.Language, "typescript")
	}
	if len(info.Dependencies) != 0 {
		t.Errorf("Dependencies count = %d, want 0", len(info.Dependencies))
	}
}

func TestParsePackageJSON_NoEngines(t *testing.T) {
	t.Parallel()
	input := `{"dependencies": {"react": "^18.0.0"}}`
	info := ParsePackageJSON([]byte(input))
	if info.RuntimeVersion != "" {
		t.Errorf("RuntimeVersion = %q, want empty", info.RuntimeVersion)
	}
}
