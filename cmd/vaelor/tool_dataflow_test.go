package main

import (
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
)

// TestDataflowFocus_PathDetection guards #565 (schema half): when an agent
// passes a FILE PATH as `focus` (the field is an enum, not a path), the error
// must point to file_glob. Non-path invalid values keep the plain enum error.
// Reverting the path-detection branch REDS the path-like rows.
func TestDataflowFocus_PathDetection(t *testing.T) {
	deps := analyze.Deps{OxCodes: oxcodes.NewClient("http://dummy:9999")}
	cases := []struct {
		name         string
		focus        string
		wantIsError  bool
		wantSubstr   string
		wantFileGlob bool
	}{
		{
			name:         "file path with slash points to file_glob",
			focus:        "crates/server/src/turn_credentials.rs",
			wantIsError:  true,
			wantSubstr:   "file_glob",
			wantFileGlob: true,
		},
		{
			name:         "file path with dot points to file_glob",
			focus:        "main.go",
			wantIsError:  true,
			wantSubstr:   "file_glob",
			wantFileGlob: true,
		},
		{
			name:         "bare invalid enum no file_glob hint",
			focus:        "performance",
			wantIsError:  true,
			wantSubstr:   "focus must be 'all', 'quality', or 'security'",
			wantFileGlob: false,
		},
		{
			name:        "valid quality accepted",
			focus:       "quality",
			wantIsError: false,
		},
		{
			name:        "valid all accepted",
			focus:       "all",
			wantIsError: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// repo is required to reach the focus check; use a dummy slug.
			// resolveRoot will fail for the dummy, but the focus check runs
			// BEFORE resolveRoot, so the focus error surfaces first for the
			// invalid rows. For valid rows, resolveRoot fails → error, which
			// is fine (we only assert focus acceptance via no-focus-error).
			res, err := handleDataflow(context.Background(), DataflowInput{
				Repo:  "owner/dummy",
				Focus: tc.focus,
			}, deps, "")
			if err != nil {
				t.Fatalf("unexpected go error: %v", err)
			}
			text := resultText(res)
			if tc.wantIsError {
				if res == nil || !res.IsError {
					t.Fatalf("expected error result, got: %s", text)
				}
				if !strings.Contains(text, tc.wantSubstr) {
					t.Errorf("expected %q in error, got: %s", tc.wantSubstr, text)
				}
				hasFileGlob := strings.Contains(text, "file_glob")
				if hasFileGlob != tc.wantFileGlob {
					t.Errorf("file_glob hint presence = %v, want %v; text: %s", hasFileGlob, tc.wantFileGlob, text)
				}
			} else {
				// Valid focus must NOT trip the focus-enum error; it proceeds
				// to resolveRoot (which fails on the dummy slug — that's fine,
				// we only assert the focus check did not fire).
				if strings.Contains(text, "focus must be") {
					t.Errorf("valid focus %q must not trip focus-enum error, got: %s", tc.focus, text)
				}
			}
		})
	}
}
