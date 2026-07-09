package parser_test

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestRustVisibilityAndAttributes(t *testing.T) {
	t.Parallel()
	source := []byte(`
use std::io;

#[derive(Debug, Clone)]
pub struct Config {
    host: String,
}

pub fn public_function() {}

fn private_function() {}

#[test]
fn test_something() {}

#[tokio::test]
async fn test_async() {}

impl Config {
    pub fn new() -> Self { Config { host: String::new() } }
    fn secret() {}
}

pub trait Handler {
    fn handle(&self);
}

impl Handler for Config {
    fn handle(&self) {}
}
`)
	result, err := parser.ParseFile("test.rs", source, parser.ParseOpts{
		IncludeBody: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	byName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		byName[sym.Name] = sym
	}

	// Visibility checks.
	visCases := []struct {
		name     string
		isPublic bool
	}{
		{"Config", true},
		{"public_function", true},
		{"private_function", false},
		{"new", true},
		{"secret", false},
		{"Handler", true},
	}
	for _, tc := range visCases {
		sym, ok := byName[tc.name]
		if !ok {
			t.Errorf("symbol %q not found", tc.name)
			continue
		}
		if sym.IsPublic != tc.isPublic {
			t.Errorf("%q: IsPublic = %v, want %v", tc.name, sym.IsPublic, tc.isPublic)
		}
	}

	// Attribute checks.
	if sym, ok := byName["Config"]; ok {
		found := false
		for _, attr := range sym.Attributes {
			if attr == "#[derive(Debug, Clone)]" {
				found = true
			}
		}
		if !found {
			t.Errorf("Config missing #[derive(Debug, Clone)] attribute; got %v", sym.Attributes)
		}
	}

	if sym, ok := byName["test_something"]; ok {
		found := false
		for _, attr := range sym.Attributes {
			if attr == "#[test]" {
				found = true
			}
		}
		if !found {
			t.Errorf("test_something missing #[test] attribute; got %v", sym.Attributes)
		}
	}

	// Receiver checks.
	if sym, ok := byName["new"]; ok {
		if sym.Receiver != "Config" {
			t.Errorf("new: Receiver = %q, want %q", sym.Receiver, "Config")
		}
	}
	if sym, ok := byName["handle"]; ok {
		if sym.Receiver != "Handler for Config" {
			t.Errorf("handle: Receiver = %q, want %q", sym.Receiver, "Handler for Config")
		}
	}
}
