package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// outputFormatAST is the output_format value that returns the raw syntax tree.
// The default (empty or any other value) returns the symbols table.
const outputFormatAST = "ast"

// FileParseInput is the input schema for the file_parse tool.
type FileParseInput struct {
	// Path is the absolute or relative path to the source file.
	Path string `json:"path" jsonschema_description:"Absolute or relative path to the source file"`

	// Language overrides auto-detection (e.g. go, python, typescript, rust, java).
	Language string `json:"language,omitempty" jsonschema_description:"Language override (go/python/typescript/rust/java/c/cpp). Auto-detected if omitted."`

	// OutputFormat controls what is returned: ast (raw tree) or symbols (functions/types/vars).
	OutputFormat string `json:"output_format,omitempty" jsonschema_description:"Output format: ast (raw tree) | symbols (functions types vars) (default: symbols)"`
}

// registerFileParse registers the file_parse MCP tool.
// Parses a single source file with tree-sitter and extracts the AST or symbol table.
func registerFileParse(server *mcp.Server, cfg Config) {
	maxBytes := cfg.MaxFileBytes
	mcp.AddTool(server, &mcp.Tool{
		Name: "file_parse",
		Description: "Parse a single source file using tree-sitter and return its AST or symbol table. " +
			"Supports Go, Python, TypeScript, JavaScript, Rust, Java, C, C++. " +
			"Use output_format=symbols to get a structured list of functions, types, and variables. " +
			"Use output_format=ast to get the raw syntax tree for deep analysis.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input FileParseInput) (*mcp.CallToolResult, any, error) {
		if input.Path == "" {
			return errResult("path is required"), nil, nil
		}

		input.Path = rewritePath(input.Path, cfg.PathMappings)
		fi, err := os.Stat(input.Path)
		if err != nil {
			return errResult(fmt.Sprintf("stat file: %s", err)), nil, nil
		}
		if fi.Size() > maxBytes {
			return errResult(fmt.Sprintf("file too large: %d bytes (max %d)", fi.Size(), maxBytes)), nil, nil
		}

		source, err := os.ReadFile(input.Path)
		if err != nil {
			return errResult(fmt.Sprintf("read file: %s", err)), nil, nil
		}

		includeBody := input.OutputFormat == outputFormatAST
		pr, err := parser.ParseFile(input.Path, source, parser.ParseOpts{
			Language:       input.Language,
			IncludeBody:    includeBody,
			IncludeImports: true,
		})
		if err != nil {
			return errResult(fmt.Sprintf("parse file: %s", err)), nil, nil
		}

		return textResult(formatParseResult(pr, input.OutputFormat)), nil, nil
	})
}

// formatParseResult formats a ParseResult according to the requested output format.
func formatParseResult(pr *parser.ParseResult, format string) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "File: %s\nLanguage: %s\n\n", pr.File, pr.Language)

	if len(pr.Imports) > 0 {
		fmt.Fprintf(&sb, "## Imports (%d)\n", len(pr.Imports))
		for _, imp := range pr.Imports {
			fmt.Fprintf(&sb, "  - %s\n", imp)
		}
		sb.WriteString("\n")
	}

	if len(pr.Symbols) == 0 {
		sb.WriteString("No symbols found.\n")
		return sb.String()
	}

	fmt.Fprintf(&sb, "## Symbols (%d)\n", len(pr.Symbols))

	if format == outputFormatAST {
		formatSymbolsAST(&sb, pr.Symbols)
	} else {
		formatSymbolsTable(&sb, pr.Symbols)
	}

	return sb.String()
}

// formatSymbolsTable formats symbols as a compact list with kind, name, and line numbers.
func formatSymbolsTable(sb *strings.Builder, symbols []*parser.Symbol) {
	for _, sym := range symbols {
		if sym.Signature != "" {
			fmt.Fprintf(sb, "  [%s] %s\n    Signature: %s\n    Lines: %d-%d\n",
				sym.Kind, sym.Name, sym.Signature, sym.StartLine, sym.EndLine)
		} else {
			fmt.Fprintf(sb, "  [%s] %s\n    Lines: %d-%d\n",
				sym.Kind, sym.Name, sym.StartLine, sym.EndLine)
		}
		if sym.Complexity > 0 {
			fmt.Fprintf(sb, "    Complexity: %d\n", sym.Complexity)
		}
		if sym.DocComment != "" {
			fmt.Fprintf(sb, "    Doc: %s\n", sym.DocComment)
		}
	}
}

// formatSymbolsAST formats symbols with their full source body included.
func formatSymbolsAST(sb *strings.Builder, symbols []*parser.Symbol) {
	for _, sym := range symbols {
		if sym.Complexity > 0 {
			fmt.Fprintf(sb, "### [%s] %s (lines %d-%d, complexity %d)\n", sym.Kind, sym.Name, sym.StartLine, sym.EndLine, sym.Complexity)
		} else {
			fmt.Fprintf(sb, "### [%s] %s (lines %d-%d)\n", sym.Kind, sym.Name, sym.StartLine, sym.EndLine)
		}
		if sym.DocComment != "" {
			fmt.Fprintf(sb, "Doc: %s\n", sym.DocComment)
		}
		if sym.Body != "" {
			fmt.Fprintf(sb, "```\n%s\n```\n\n", sym.Body)
		} else {
			sb.WriteString("\n")
		}
	}
}
