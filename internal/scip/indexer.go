package scip

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
)

// RunIndexer executes the SCIP indexer described by cfg inside dir.
// It returns the path to the generated index.scip file.
// stdout and stderr from the subprocess are discarded.
// Returns an error if the binary is not found or the process exits non-zero.
func RunIndexer(ctx context.Context, cfg IndexerConfig, dir string) (string, error) {
	binPath, err := exec.LookPath(cfg.Name)
	if err != nil {
		return "", fmt.Errorf("scip indexer %q not found in PATH: %w", cfg.Name, err)
	}

	//nolint:gosec // cfg.Name and cfg.Args are from a controlled registry, not user input.
	cmd := exec.CommandContext(ctx, binPath, cfg.Args...)
	cmd.Dir = dir
	// Discard stdout and stderr — leave them as nil (default).

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("scip indexer %q failed: %w", cfg.Name, err)
	}

	return filepath.Join(dir, "index.scip"), nil
}
