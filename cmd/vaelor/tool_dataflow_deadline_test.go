package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
)

// TestHandleDataflow_TinyDeadline_ReturnsPartialWithFooter verifies #565: under
// a tiny injected soft deadline, dataflow_analyze returns a PARTIAL result with
// a footer naming what was truncated — never a bare error, never a session-
// killing hard timeout. RED-on-revert: remove the SoftDeadline / softCtx.Err()
// guard in handleDataflow and this test gets a hard error instead of a partial
// footer.
func TestHandleDataflow_TinyDeadline_ReturnsPartialWithFooter(t *testing.T) {
	root := writeDataflowFixture(t)
	srv := newMockOxCodesServer(t)
	defer srv.Close()

	deps := analyze.Deps{OxCodes: oxcodes.NewClient(srv.URL)}

	// Inject a 1ms soft deadline via the parent ctx and burn it before
	// calling handleDataflow. resolveRoot (local path, ctx-agnostic) succeeds,
	// then the ox-codes calls bail on the canceled ctx and the handler renders
	// a partial result.
	deadlined, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)

	res, err := handleDataflow(deadlined, DataflowInput{Repo: root}, deps, "")
	if err != nil {
		t.Fatalf("handleDataflow returned error on expired ctx (want partial): %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("want non-error partial result, got %+v", res)
	}
	got := textContentOf(t, res)
	if !strings.Contains(got, "partial: true") {
		t.Fatalf("partial result must contain 'partial: true', got:\n%s", got)
	}
	if !strings.Contains(got, "soft deadline") {
		t.Fatalf("partial footer must name the soft deadline, got:\n%s", got)
	}
}

// TestHandleDataflow_FastPath_NoFooter verifies the fast path (deadline not
// reached) returns the full XML result with NO partial footer — byte-identical
// to the pre-change output.
func TestHandleDataflow_FastPath_NoFooter(t *testing.T) {
	root := writeDataflowFixture(t)
	srv := newMockOxCodesServer(t)
	defer srv.Close()

	deps := analyze.Deps{OxCodes: oxcodes.NewClient(srv.URL)}
	res, err := handleDataflow(context.Background(), DataflowInput{Repo: root, Focus: "security"}, deps, "")
	if err != nil {
		t.Fatalf("handleDataflow: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("want non-error result, got %+v", res)
	}
	got := textContentOf(t, res)
	if strings.Contains(got, "partial: true") {
		t.Fatalf("fast path must NOT contain a partial footer, got:\n%s", got)
	}
	if !strings.Contains(got, "<dataflow") {
		t.Fatalf("fast path must contain dataflow XML, got:\n%s", got)
	}
}

// TestFindOversizedFiles_DetectsLargeFile verifies the #565 file-size cap
// guidance: a file exceeding dataflowMaxFileLines is detected and returned so
// the partial footer can suggest exclude_glob.
func TestFindOversizedFiles_DetectsLargeFile(t *testing.T) {
	root := t.TempDir()
	// Write a file with >dataflowMaxFileLines lines.
	var sb strings.Builder
	sb.WriteString("package main\n")
	for range dataflowMaxFileLines {
		sb.WriteString("// line\n")
	}
	if err := os.WriteFile(filepath.Join(root, "big.go"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	oversized := findOversizedFiles(root, "go", dataflowMaxFileLines)
	if len(oversized) == 0 {
		t.Fatal("findOversizedFiles must detect the large file")
	}
	if oversized[0] != "big.go" {
		t.Fatalf("want big.go, got %s", oversized[0])
	}
}

// TestFindOversizedFiles_SmallFile_NotFlagged verifies files under the
// threshold are not flagged.
func TestFindOversizedFiles_SmallFile_NotFlagged(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "small.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oversized := findOversizedFiles(root, "go", dataflowMaxFileLines)
	if len(oversized) != 0 {
		t.Fatalf("small file must not be flagged, got %v", oversized)
	}
}

func writeDataflowFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\nfunc helper() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// newMockOxCodesServer returns a test HTTP server that responds to ox-codes
// dataflow/taint endpoints with valid (empty-result) JSON.
func newMockOxCodesServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Honor ctx cancellation: the request's ctx is derived from the
		// client's ctx, so a canceled client ctx closes the connection.
		select {
		case <-r.Context().Done():
			return
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		var resp any
		switch r.URL.Path {
		case "/dataflow/analyze":
			resp = oxcodes.DataflowResponse{FilesAnalyzed: 1, DurationMS: 10}
		case "/dataflow/taint":
			resp = oxcodes.TaintResponse{FilesAnalyzed: 1, DurationMS: 10}
		default:
			resp = map[string]any{"ok": true}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}
