package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ReadOutputInput is the input schema for the read_output tool.
type ReadOutputInput struct {
	Name   string `json:"name" jsonschema:"File name as returned in the spill notice (e.g. code_graph_1234567890.txt). Basename only — no directories."`
	Offset int    `json:"offset,omitempty" jsonschema:"Byte offset to resume from, taken from the previous chunk's truncated footer (default: 0)"`
}

func registerReadOutput(server *mcp.Server, cfg Config) {
	if cfg.OutputDir == "" {
		return
	}
	addTool(server, &mcp.Tool{
		Name: "read_output",
		Description: "Read a large tool output that was spilled to a file on the server " +
			"(the \"saved to: …\" notice). Returns one chunk per call; when the chunk " +
			"carries a truncated footer, pass the indicated offset to continue. " +
			"This is the remote-client path — the spilled file itself lives on the MCP " +
			"server host and is not readable by off-host agents.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input ReadOutputInput) (*mcp.CallToolResult, error) {
		return handleReadOutput(input, cfg.OutputDir), nil
	})
}

// readOutputWindow is the payload budget per read_output call. The header and
// continuation footer ride on top; the total stays under MaxBudget (9 KB), which
// is the client-side hard-cut ceiling (see mcpmeta.MaxBudget).
const readOutputWindow = mcpmeta.MaxBudget - 256

func handleReadOutput(input ReadOutputInput, outputDir string) *mcp.CallToolResult {
	path, err := resolveSpillPath(outputDir, input.Name)
	if err != nil {
		return errResult(err.Error())
	}
	f, err := os.Open(path) //nolint:gosec // confined under outputDir by resolveSpillPath
	if err != nil {
		return errResult(fmt.Sprintf("read_output: %v — the spill file may already be cleaned up", err))
	}
	defer f.Close() //nolint:errcheck // read-only

	info, err := f.Stat()
	if err != nil {
		return errResult(fmt.Sprintf("read_output: stat %q: %v", input.Name, err))
	}
	total := int(info.Size())
	if total == 0 {
		return textResult(mcpmeta.MarkBudgetApplied(
			fmt.Sprintf("%s — 0 bytes\n", input.Name)))
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return errResult(fmt.Sprintf("read_output: offset %d is beyond end of %q (%d bytes)", offset, input.Name, total))
	}

	window := readOutputWindow
	if rem := total - offset; rem < window {
		window = rem
	}
	buf := make([]byte, window)
	n, err := f.ReadAt(buf, int64(offset))
	if err != nil && n == 0 {
		return errResult(fmt.Sprintf("read_output: read %q: %v", input.Name, err))
	}
	// Never split a multi-byte rune across pages — a straddling rune would be
	// replaced by U+FFFD on BOTH sides of the seam during JSON encoding. A
	// UTF-8 sequence spans ≤4 bytes, so ≤3 decrements suffice for well-formed
	// content; the cap keeps genuinely-invalid (binary) content bounded.
	if offset+n < total {
		for i := 0; i < utf8.UTFMax-1 && n > 0 && !utf8.Valid(buf[:n]); i++ {
			n--
		}
	}
	if n == 0 {
		// The file shrank between Stat and ReadAt and the trim consumed the
		// remainder — error rather than echo the same offset forever.
		return errResult(fmt.Sprintf("read_output: %q changed while reading — retry the same offset", input.Name))
	}
	chunk := string(buf[:n])

	header := fmt.Sprintf("%s — bytes %d..%d of %d\n", input.Name, offset, offset+n, total)
	body := header + chunk
	if offset+n < total {
		body += fmt.Sprintf("\n[truncated: %d more chars — read_output(name=%q, offset=%d)]",
			total-(offset+n), input.Name, offset+n)
	}
	return textResult(mcpmeta.MarkBudgetApplied(body))
}

// resolveSpillPath confines name to a plain file directly inside outputDir.
// Basename-only (rejects separators, "..", absolute) and rejects symlinks that
// resolve outside the directory — a spilled file is server-generated output and
// this tool must never become a general file-read primitive.
func resolveSpillPath(outputDir, name string) (string, error) {
	if name == "" || filepath.Base(name) != name || !filepath.IsLocal(name) ||
		strings.IndexFunc(name, unicode.IsControl) >= 0 {
		// Control chars in the name would also echo unquoted into the header
		// line, breaking its format / enabling fake-footer injection.
		return "", fmt.Errorf("read_output: invalid file name %q — pass the basename from the spill notice", name)
	}
	base, err := filepath.EvalSymlinks(outputDir)
	if err != nil {
		return "", fmt.Errorf("read_output: output dir unavailable: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(base, name))
	if err != nil {
		return "", fmt.Errorf("read_output: %q not found in output dir", name)
	}
	if !strings.HasPrefix(resolved, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("read_output: %q resolves outside the output dir", name)
	}
	// Reject non-regular files BEFORE os.Open — POSIX open on a FIFO blocks
	// until a writer appears and no context can interrupt it; IsRegular after
	// Open would be too late. Residual check-then-open TOCTOU is accepted:
	// planting anything in OUTPUT_DIR needs write access to the dir itself.
	if info, err := os.Lstat(resolved); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("read_output: %q is not a regular file", name)
	}
	return resolved, nil
}
