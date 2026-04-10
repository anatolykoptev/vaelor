package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/anatolykoptev/go-kit/llm"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// xmlCDATA wraps content in a CDATA section to prevent XML escaping of
// source code, tree diagrams, and other content with special characters.
// Use with `xml:",innerxml"` tag on struct fields.
type xmlCDATA struct {
	Inner string `xml:",innerxml"`
}

// wrapCDATA wraps a string in an XML CDATA section, escaping embedded "]]>" sequences.
func wrapCDATA(s string) string {
	s = strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
	return "<![CDATA[" + s + "]]>"
}

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

// xmlMarshalFileResult marshals v as indented XML and always saves to file.
// Falls back to inline when outputDir is empty.
func xmlMarshalFileResult(v any, toolName, outputDir string) *mcp.CallToolResult {
	data, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err))
	}
	text := xml.Header + string(data)
	if outputDir == "" {
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

// xmlMarshalResult marshals v as indented XML and returns it via largeTextResult.
func xmlMarshalResult(v any, toolName, outputDir string) *mcp.CallToolResult {
	data, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err))
	}
	return largeTextResult(xml.Header+string(data), toolName, outputDir)
}

// generateNarrative produces an LLM narrative from structured data.
// Returns empty string on nil client or any error (non-fatal).
func generateNarrative(ctx context.Context, client *llm.Client, systemPrompt string, data any, promptPrefix string) string {
	if client == nil {
		return ""
	}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	narrative, err := client.Complete(ctx, systemPrompt, promptPrefix+string(dataJSON))
	if err != nil {
		return ""
	}
	return narrative
}

// escapeXML escapes special XML characters in a string.
func escapeXML(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s // fallback to raw string on error
	}
	return b.String()
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
