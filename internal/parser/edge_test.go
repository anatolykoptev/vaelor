package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestEdgeCases_DumpSymbols parses edge case files and prints all discovered symbols.
// This is a diagnostic test — it never fails, just reports what was found.
func TestEdgeCases_DumpSymbols(t *testing.T) {
	t.Parallel()
	files := []string{
		"edge_ruby.rb",
		"edge_java.java",
		"edge_cpp.cpp",
		"edge_rust.rs",
		"edge_csharp.cs",
		"edge_c.c",
	}

	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("testdata", f))
			if err != nil {
				t.Fatalf("read testdata/%s: %v", f, err)
			}

			result, err := parser.ParseFile("testdata/"+f, source, parser.ParseOpts{
				IncludeImports: true,
			})
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}

			t.Logf("=== %s (language=%s) ===", f, result.Language)
			t.Logf("Imports (%d): %v", len(result.Imports), result.Imports)
			t.Logf("Symbols (%d):", len(result.Symbols))
			for i, sym := range result.Symbols {
				t.Logf("  [%d] %-12s %-20s L%d-%d  sig=%q",
					i, sym.Kind, sym.Name, sym.StartLine, sym.EndLine, truncate(sym.Signature, 60))
			}
		})
	}
}

// TestEdge_RubyMethodVsFunction verifies Ruby method/function distinction.
func TestEdge_RubyMethodVsFunction(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "edge_ruby.rb"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	result, err := parser.ParseFile("testdata/edge_ruby.rb", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	byName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		byName[sym.Name] = sym
	}

	// Top-level functions SHOULD be KindFunction
	topLevel := []string{"helper_function", "create_server"}
	for _, name := range topLevel {
		sym, ok := byName[name]
		if !ok {
			t.Errorf("MISSING: top-level function %q not found", name)
			continue
		}
		if sym.Kind != parser.KindFunction {
			t.Errorf("top-level %q: Kind=%q, want %q", name, sym.Kind, parser.KindFunction)
		}
	}

	// Instance methods inside class MUST be KindMethod
	instanceMethods := []string{"initialize", "start", "internal_check"}
	for _, name := range instanceMethods {
		sym, ok := byName[name]
		if !ok {
			t.Errorf("MISSING: instance method %q not found", name)
			continue
		}
		if sym.Kind != parser.KindMethod {
			t.Errorf("instance method %q: Kind=%q, want %q", name, sym.Kind, parser.KindMethod)
		}
	}

	// Singleton methods (def self.create) MUST be KindMethod
	if sym, ok := byName["create"]; ok {
		if sym.Kind != parser.KindMethod {
			t.Errorf("singleton method 'create': Kind=%q, want %q", sym.Kind, parser.KindMethod)
		}
	} else {
		t.Errorf("MISSING: singleton method 'create' not found")
	}

	// Module nesting
	if sym, ok := byName["Outer"]; ok {
		t.Logf("Module 'Outer': Kind=%q", sym.Kind)
	} else {
		t.Errorf("MISSING: module 'Outer' not found")
	}
	if sym, ok := byName["Inner"]; ok {
		t.Logf("Module 'Inner': Kind=%q", sym.Kind)
	} else {
		t.Logf("NOTE: nested module 'Inner' not captured (query may not reach nested modules)")
	}

	// Nested class inside module
	if _, ok := byName["DeepConfig"]; !ok {
		t.Logf("NOTE: nested class 'DeepConfig' not captured")
	}

	// Methods inside nested class
	if _, ok := byName["address"]; !ok {
		t.Logf("NOTE: method 'address' inside nested class not captured")
	}

	// Multiple constants
	if _, ok := byName["MAX_RETRIES"]; !ok {
		t.Errorf("MISSING: constant MAX_RETRIES")
	}
	if _, ok := byName["DEFAULT_HOST"]; !ok {
		t.Errorf("MISSING: constant DEFAULT_HOST")
	}
}

// TestEdge_JavaEnumMethods tests whether Java enum methods are captured.
func TestEdge_JavaEnumMethods(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "edge_java.java"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	result, err := parser.ParseFile("testdata/edge_java.java", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	byKindName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		key := string(sym.Kind) + ":" + sym.Name
		byKindName[key] = sym
	}

	// Enum type should be captured
	if _, ok := byKindName["type:Status"]; !ok {
		t.Errorf("MISSING: enum 'Status' not captured")
	}

	// Method inside enum — must be captured
	if _, ok := byKindName["method:display"]; !ok {
		t.Errorf("MISSING: Java enum method 'display' not captured")
	}

	// Nested class methods
	if _, ok := byKindName["method:doWork"]; !ok {
		t.Logf("NOTE: method 'doWork' inside nested class 'Inner' not captured")
	}

	// Nested interface
	if _, ok := byKindName["interface:Callback"]; !ok {
		t.Logf("NOTE: nested interface 'Callback' not captured")
	}

	// Nested class
	if _, ok := byKindName["class:Inner"]; !ok {
		t.Logf("NOTE: nested class 'Inner' not captured")
	}

	// Outer class methods
	if _, ok := byKindName["method:getValue"]; !ok {
		t.Errorf("MISSING: method 'getValue' in Outer class")
	}
}

// TestEdge_CppTemplatesAndNamespaces tests C++ template and namespace handling.
func TestEdge_CppTemplatesAndNamespaces(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "edge_cpp.cpp"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	result, err := parser.ParseFile("testdata/edge_cpp.cpp", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	byKindName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		key := string(sym.Kind) + ":" + sym.Name
		byKindName[key] = sym
	}

	// Class inside namespace
	if _, ok := byKindName["class:Server"]; !ok {
		t.Errorf("MISSING: class 'Server' inside namespace")
	}

	// Function inside namespace
	if _, ok := byKindName["function:log_message"]; !ok {
		t.Logf("NOTE: function 'log_message' inside namespace may be captured differently")
		// Check if it was captured as something else
		for k := range byKindName {
			if k == "function:log_message" || k == "method:log_message" {
				t.Logf("  found as: %s", k)
			}
		}
	}

	// Out-of-line qualified method definitions
	// net::Server::Server is a qualified constructor
	found := false
	for k := range byKindName {
		t.Logf("  checking: %s", k)
		if k == "method:net::Server::Server" || k == "method:Server::Server" {
			found = true
			t.Logf("  out-of-line constructor found as: %s", k)
		}
	}
	if !found {
		t.Logf("NOTE: out-of-line constructor 'net::Server::Server' not captured or captured differently")
	}

	// Template function
	if _, ok := byKindName["function:max_value"]; !ok {
		t.Logf("NOTE: template function 'max_value' not captured (may need template_declaration query)")
	}

	// Template class
	if _, ok := byKindName["class:Container"]; !ok {
		t.Logf("NOTE: template class 'Container' not captured")
	}

	// Main function
	if _, ok := byKindName["function:main"]; !ok {
		t.Errorf("MISSING: function 'main'")
	}
}

// TestEdge_RustNestedAndGenerics tests Rust nested modules and generic functions.
func TestEdge_RustNestedAndGenerics(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "edge_rust.rs"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	result, err := parser.ParseFile("testdata/edge_rust.rs", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	byKindName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		key := string(sym.Kind) + ":" + sym.Name
		byKindName[key] = sym
	}

	// Function inside nested module — must be captured
	if _, ok := byKindName["function:inner_function"]; !ok {
		t.Errorf("MISSING: function 'inner_function' inside mod block not captured")
	}

	// Multiple impl blocks - methods from both should be captured
	if _, ok := byKindName["method:new"]; !ok {
		t.Errorf("MISSING: method 'new' from first impl block")
	}
	if _, ok := byKindName["method:address"]; !ok {
		t.Errorf("MISSING: method 'address' from first impl block")
	}
	if _, ok := byKindName["method:fmt"]; !ok {
		t.Logf("NOTE: method 'fmt' from Display impl not captured (check impl_item query)")
	}

	// Trait with default method body
	if _, ok := byKindName["interface:Handler"]; !ok {
		t.Errorf("MISSING: trait 'Handler'")
	}

	// Generic function
	if _, ok := byKindName["function:process"]; !ok {
		t.Logf("NOTE: generic function 'process' not captured (query scoped to source_file only)")
	}

	// Enum with methods
	if _, ok := byKindName["method:is_active"]; !ok {
		t.Logf("NOTE: method 'is_active' inside Status enum impl not captured")
	}

	// Constants and statics
	if _, ok := byKindName["const:MAX_CONNECTIONS"]; !ok {
		t.Errorf("MISSING: const MAX_CONNECTIONS")
	}
	if _, ok := byKindName["var:GLOBAL_CONFIG"]; !ok {
		t.Errorf("MISSING: static GLOBAL_CONFIG")
	}
}

// TestEdge_CSharpAdvanced tests C# edge cases.
func TestEdge_CSharpAdvanced(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "edge_csharp.cs"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	result, err := parser.ParseFile("testdata/edge_csharp.cs", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	byKindName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		key := string(sym.Kind) + ":" + sym.Name
		byKindName[key] = sym
	}

	// Generic interface
	if _, ok := byKindName["interface:IRepository"]; !ok {
		t.Logf("NOTE: generic interface 'IRepository<T>' may not be captured correctly")
	}

	// Abstract class
	if _, ok := byKindName["class:BaseEntity"]; !ok {
		t.Errorf("MISSING: abstract class 'BaseEntity'")
	}

	// Abstract method
	if _, ok := byKindName["method:Validate"]; !ok {
		t.Logf("NOTE: abstract method 'Validate' not captured")
	}

	// Virtual method
	if _, ok := byKindName["method:Display"]; !ok {
		t.Logf("NOTE: virtual method 'Display' not captured")
	}

	// Override method
	// Note: there will be two 'Validate' methods (abstract in BaseEntity, override in User)
	validateCount := 0
	for _, sym := range result.Symbols {
		if sym.Name == "Validate" && sym.Kind == parser.KindMethod {
			validateCount++
		}
	}
	t.Logf("'Validate' method count: %d (expect 2: abstract + override)", validateCount)

	// Static class
	if _, ok := byKindName["class:Helper"]; !ok {
		t.Errorf("MISSING: static class 'Helper'")
	}

	// Static method
	if _, ok := byKindName["method:Format"]; !ok {
		t.Errorf("MISSING: static method 'Format'")
	}

	// Struct with constructor and method
	if _, ok := byKindName["struct:Point"]; !ok {
		t.Logf("NOTE: struct 'Point' missing or detected as wrong kind")
	}
	if _, ok := byKindName["method:Distance"]; !ok {
		t.Logf("NOTE: method 'Distance' inside struct not captured")
	}

	// Imports
	for _, want := range []string{"System", "System.Collections.Generic", "System.Linq"} {
		found := false
		for _, imp := range result.Imports {
			if imp == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("MISSING import: %q", want)
		}
	}
}

// TestEdge_CAdvanced tests C edge cases with macros, function pointers, etc.
func TestEdge_CAdvanced(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("testdata", "edge_c.c"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	result, err := parser.ParseFile("testdata/edge_c.c", source, parser.ParseOpts{
		IncludeImports: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	byKindName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		key := string(sym.Kind) + ":" + sym.Name
		byKindName[key] = sym
	}

	// Typedef struct
	if _, ok := byKindName["type:Handler"]; !ok {
		t.Errorf("MISSING: typedef struct 'Handler'")
	}

	// Named struct
	if _, ok := byKindName["struct:Node"]; !ok {
		t.Errorf("MISSING: struct 'Node'")
	}

	// Enum
	if _, ok := byKindName["type:LogLevel"]; !ok {
		t.Errorf("MISSING: enum 'LogLevel'")
	}

	// Static function
	if _, ok := byKindName["function:internal_helper"]; !ok {
		t.Errorf("MISSING: static function 'internal_helper'")
	}

	// Function returning struct pointer
	if _, ok := byKindName["function:create_node"]; !ok {
		t.Errorf("MISSING: function 'create_node' (returns struct Node*)")
	}

	// Variadic function
	if _, ok := byKindName["function:log_message"]; !ok {
		t.Errorf("MISSING: variadic function 'log_message'")
	}

	// Function with callback parameter
	if _, ok := byKindName["function:register_callback"]; !ok {
		t.Errorf("MISSING: function 'register_callback'")
	}

	// Function pointer typedef should NOT be captured as function
	if _, ok := byKindName["function:Callback"]; ok {
		t.Errorf("FALSE POSITIVE: typedef function pointer 'Callback' captured as function")
	}

	// Macro should NOT be captured
	if _, ok := byKindName["function:MAX"]; ok {
		t.Errorf("FALSE POSITIVE: macro 'MAX' captured as function")
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// Ensure fmt is used
var _ = fmt.Sprintf
