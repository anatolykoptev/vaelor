package polyglot

import "testing"

func TestParseCargoToml(t *testing.T) {
	content := []byte(`
[package]
name = "ox-browser"
version = "0.1.0"
edition = "2024"

[dependencies]
reqwest = { version = "0.12", features = ["cookies"] }
tokio = { version = "1", features = ["full"] }
serde = "1.0"

[dev-dependencies]
mockall = "0.13"
`)

	info := ParseCargoToml(content)

	if info.Name != "ox-browser" {
		t.Errorf("Name = %q, want %q", info.Name, "ox-browser")
	}
	if info.Edition != "2024" {
		t.Errorf("Edition = %q, want %q", info.Edition, "2024")
	}
	if len(info.Dependencies) != 3 {
		t.Errorf("Dependencies count = %d, want 3; got %v", len(info.Dependencies), info.Dependencies)
	}
	if len(info.DevDependencies) != 1 {
		t.Errorf("DevDependencies count = %d, want 1", len(info.DevDependencies))
	}
}

func TestParseCargoToml_Workspace(t *testing.T) {
	content := []byte(`
[workspace]
members = ["crates/core", "crates/http", "crates/mcp"]
resolver = "2"
`)

	info := ParseCargoToml(content)

	if len(info.WorkspaceMembers) != 3 {
		t.Errorf("WorkspaceMembers = %d, want 3; got %v", len(info.WorkspaceMembers), info.WorkspaceMembers)
	}
}

func TestParseCargoToml_Empty(t *testing.T) {
	info := ParseCargoToml([]byte(""))
	if info.Name != "" {
		t.Errorf("expected empty name for empty input")
	}
}
