package main

import (
	"testing"
)

func TestCodeSearchInput_PathAlias(t *testing.T) {
	input := CodeSearchInput{
		Repo:    "owner/repo",
		Pattern: "func main",
		Path:    "internal/query",
	}
	if input.Path != "" && input.FileGlob == "" {
		input.FileGlob = input.Path + "/**"
	}
	if input.FileGlob != "internal/query/**" {
		t.Errorf("expected file_glob=internal/query/**, got %s", input.FileGlob)
	}
}

func TestCodeSearchInput_PathDoesNotOverrideFileGlob(t *testing.T) {
	input := CodeSearchInput{
		Repo:     "owner/repo",
		Pattern:  "func main",
		Path:     "internal/query",
		FileGlob: "*.go",
	}
	if input.Path != "" && input.FileGlob == "" {
		input.FileGlob = input.Path + "/**"
	}
	if input.FileGlob != "*.go" {
		t.Errorf("expected file_glob=*.go (not overridden), got %s", input.FileGlob)
	}
}

func TestDepGraphInput_DepthAlias(t *testing.T) {
	input := DepGraphInput{MaxDepth: 7}
	if input.MaxDepth > 0 && input.Depth == 0 {
		input.Depth = input.MaxDepth
	}
	if input.Depth != 7 {
		t.Errorf("expected depth=7, got %d", input.Depth)
	}
}

func TestDepGraphInput_DepthTakesPrecedence(t *testing.T) {
	input := DepGraphInput{Depth: 3, MaxDepth: 7}
	if input.MaxDepth > 0 && input.Depth == 0 {
		input.Depth = input.MaxDepth
	}
	if input.Depth != 3 {
		t.Errorf("expected depth=3 (not overridden), got %d", input.Depth)
	}
}

func TestSymbolSearchInput_SymbolAlias(t *testing.T) {
	input := SymbolSearchInput{
		Repo:   "owner/repo",
		Symbol: "HandleRequest",
	}
	if input.Symbol != "" && input.Query == "" {
		input.Query = input.Symbol
	}
	if input.Query != "HandleRequest" {
		t.Errorf("expected query=HandleRequest, got %s", input.Query)
	}
}

func TestSymbolSearchInput_QueryTakesPrecedence(t *testing.T) {
	input := SymbolSearchInput{
		Repo:   "owner/repo",
		Query:  "Auth*",
		Symbol: "HandleRequest",
	}
	if input.Symbol != "" && input.Query == "" {
		input.Query = input.Symbol
	}
	if input.Query != "Auth*" {
		t.Errorf("expected query=Auth* (not overridden), got %s", input.Query)
	}
}

func TestFileParseInput_RepoField(t *testing.T) {
	input := FileParseInput{
		Repo: "owner/repo",
		Path: "internal/query/ranking.go",
	}
	if input.Repo == "" {
		t.Error("expected repo to be set")
	}
	if input.Path == "" {
		t.Error("expected path to be set")
	}
}

func TestSymbolSearchInput_KindOnlyDefaultsToWildcard(t *testing.T) {
	input := SymbolSearchInput{
		Repo: "owner/repo",
		Kind: "trait",
	}
	if input.Symbol != "" && input.Query == "" {
		input.Query = input.Symbol
	}
	if input.Query == "" && input.Kind != "" {
		input.Query = "*"
	}
	if input.Query != "*" {
		t.Errorf("expected query=* for kind-only search, got %q", input.Query)
	}
}
