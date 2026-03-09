package freshness

import (
	"testing"
)

func TestDependencyFields(t *testing.T) {
	d := Dependency{Name: "foo", Version: "1.0.0", Language: "go"}
	if d.Name != "foo" {
		t.Errorf("Name = %q, want %q", d.Name, "foo")
	}
	if d.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", d.Version, "1.0.0")
	}
	if d.Language != "go" {
		t.Errorf("Language = %q, want %q", d.Language, "go")
	}
}

func TestManifestInfoDefaults(t *testing.T) {
	m := ManifestInfo{Language: "python"}
	if m.RuntimeVersion != "" {
		t.Errorf("RuntimeVersion = %q, want empty", m.RuntimeVersion)
	}
	if len(m.Dependencies) != 0 {
		t.Errorf("Dependencies len = %d, want 0", len(m.Dependencies))
	}
}

func TestFreshnessResultZeroValue(t *testing.T) {
	r := FreshnessResult{}
	if r.Total != 0 || r.Ratio != 0 {
		t.Errorf("zero value not as expected: %+v", r)
	}
}

func TestOutdatedDepKind(t *testing.T) {
	o := OutdatedDep{Name: "pkg", Current: "1.0", Latest: "2.0", Kind: "major"}
	if o.Kind != "major" {
		t.Errorf("Kind = %q, want %q", o.Kind, "major")
	}
}
