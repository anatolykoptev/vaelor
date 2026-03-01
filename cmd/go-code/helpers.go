package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxInlineCharsDefault is the threshold above which output is saved to a file.
const maxInlineCharsDefault = 50_000

// errResult returns a CallToolResult representing a tool-level error.
func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

// textResult returns a CallToolResult with text content.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

// largeTextResult returns a text result, saving to file if content exceeds maxInlineCharsDefault.
// When outputDir is empty or content is small, returns inline text.
// When saved to file, returns a short summary with the file path.
func largeTextResult(text, toolName, outputDir string) *mcp.CallToolResult {
	if outputDir == "" || len(text) <= maxInlineCharsDefault {
		return textResult(text)
	}
	path, ok := saveToFile(text, toolName, outputDir)
	if !ok {
		return textResult(text)
	}
	summary := fmt.Sprintf("%s: output %d chars saved to: %s\n\nUse Read tool to access the file.",
		toolName, len(text), path)
	return textResult(summary)
}

// saveToFile writes content to a timestamped file in outputDir.
// Returns the file path and true on success, or empty string and false on error.
func saveToFile(content, toolName, outputDir string) (string, bool) {
	if err := os.MkdirAll(outputDir, 0o750); err != nil { //nolint:mnd
		return "", false
	}

	filename := fmt.Sprintf("%s_%d.txt", toolName, time.Now().UnixMilli())
	path := filepath.Join(outputDir, filename)

	// File must be world-readable so the consuming agent (running as a different user) can access it.
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:mnd,gosec
		return "", false
	}

	return path, true
}

// capitalizeFirst returns s with the first Unicode letter uppercased.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
