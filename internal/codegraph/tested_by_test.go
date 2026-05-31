package codegraph

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestExtractTestedByEdges_Go(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "ProcessOrder", Kind: parser.KindFunction, File: "order.go", Language: "go"},
		{Name: "TestProcessOrder", Kind: parser.KindFunction, File: "order_test.go", Language: "go"},
		{Name: "Test_ProcessOrder_empty", Kind: parser.KindFunction, File: "order_test.go", Language: "go"},
		{Name: "BenchmarkProcessOrder", Kind: parser.KindFunction, File: "order_test.go", Language: "go"},
		{Name: "Helper", Kind: parser.KindFunction, File: "order.go", Language: "go"},
	}

	edges := ExtractTestedByEdges("", symbols)

	found := map[string]bool{}
	for _, e := range edges {
		found[e.FromKey+"->"+e.ToKey] = true
	}
	wantTP := "TestProcessOrder" + compositeKeyDelim + "order_test.go" +
		"->" + "ProcessOrder" + compositeKeyDelim + "order.go"
	if !found[wantTP] {
		t.Error("missing TestProcessOrder -> ProcessOrder")
	}
	wantTE := "Test_ProcessOrder_empty" + compositeKeyDelim + "order_test.go" +
		"->" + "ProcessOrder" + compositeKeyDelim + "order.go"
	if !found[wantTE] {
		t.Error("missing Test_ProcessOrder_empty -> ProcessOrder")
	}
	if len(edges) < 2 {
		t.Errorf("expected at least 2 edges, got %d", len(edges))
	}
}

func TestExtractTestedByEdges_Python(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "process_order", Kind: parser.KindFunction, File: "order.py", Language: "python"},
		{Name: "test_process_order", Kind: parser.KindFunction, File: "test_order.py", Language: "python"},
		{Name: "TestOrder", Kind: parser.KindType, File: "test_order.py", Language: "python"},
		{Name: "Order", Kind: parser.KindType, File: "order.py", Language: "python"},
	}

	edges := ExtractTestedByEdges("", symbols)
	if len(edges) < 1 {
		t.Fatal("expected at least 1 edge")
	}
}

// TestExtractTestedByEdges_Swift verifies that the Swift XCTest stem heuristic
// maps FooTests.swift → Foo.swift and FooTest.swift → Foo.swift via guessSourceFile,
// and that XCTest class-level (KindClass) and function-level (KindMethod) symbols
// are both recognized as test symbols.
// Relies on production isTestSymbol + guessSourceFile (internal/codegraph/tested_by.go).
func TestExtractTestedByEdges_Swift(t *testing.T) {
	cases := []struct {
		srcFile  string
		testFile string
		srcName  string
		testName string
		kind     parser.NodeKind
	}{
		// XCTestCase class convention: FooTests.swift → Foo.swift
		{"User.swift", "UserTests.swift", "User", "UserTests", parser.KindClass},
		// Alternate suffix: FooTest.swift → Foo.swift
		{"Repo.swift", "RepoTest.swift", "Repo", "RepoTest", parser.KindClass},
		// Function-level: func testFoo() in a test file
		{"Order.swift", "OrderTests.swift", "Order", "testFetchOrder", parser.KindMethod},
	}

	for _, c := range cases {
		t.Run(c.testFile, func(t *testing.T) {
			symbols := []*parser.Symbol{
				{Name: c.srcName, Kind: parser.KindClass, File: c.srcFile, Language: "swift"},
				{Name: c.testName, Kind: c.kind, File: c.testFile, Language: "swift"},
			}
			edges := ExtractTestedByEdges("", symbols)
			if len(edges) == 0 {
				t.Errorf("expected at least 1 edge from %s → %s, got none", c.testFile, c.srcFile)
			}
		})
	}
}

// TestExtractTestedByEdges_Kotlin verifies that the Kotlin stem-based heuristic
// maps FooTest.kt → Foo.kt and FooTests.kt → Foo.kt via guessSourceFile.
// Relies on production guessSourceFile (internal/codegraph/tested_by.go).
func TestExtractTestedByEdges_Kotlin(t *testing.T) {
	cases := []struct {
		srcFile  string
		testFile string
		srcName  string
		testName string
	}{
		{"User.kt", "UserTest.kt", "User", "UserTest"},
		{"Repo.kt", "RepoTests.kt", "Repo", "RepoTests"},
	}

	for _, c := range cases {
		t.Run(c.testFile, func(t *testing.T) {
			symbols := []*parser.Symbol{
				{Name: c.srcName, Kind: parser.KindClass, File: c.srcFile, Language: "kotlin"},
				{Name: c.testName, Kind: parser.KindClass, File: c.testFile, Language: "kotlin"},
			}
			edges := ExtractTestedByEdges("", symbols)
			if len(edges) == 0 {
				t.Errorf("expected at least 1 edge from %s → %s, got none", c.testFile, c.srcFile)
			}
		})
	}
}
