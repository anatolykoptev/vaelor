// Package scip provides utilities for reading and processing SCIP code intelligence indexes.
// Import alias: gocodescip "github.com/anatolykoptev/go-code/internal/scip"
package scip

import (
	"context"
	"fmt"
	"os"

	sciplib "github.com/sourcegraph/scip/bindings/go/scip"
)

// Index holds the parsed contents of a SCIP index file.
type Index struct {
	Documents []*sciplib.Document
	Metadata  *sciplib.Metadata
}

// ReadIndex opens and streams-parses a SCIP index file at the given path.
// An empty file returns an empty Index without error.
// A missing file returns a non-nil error.
func ReadIndex(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open scip index: %w", err)
	}
	defer f.Close()

	idx := &Index{}

	visitor := &sciplib.IndexVisitor{
		VisitMetadata: func(_ context.Context, m *sciplib.Metadata) error {
			idx.Metadata = m
			return nil
		},
		VisitDocument: func(_ context.Context, d *sciplib.Document) error {
			idx.Documents = append(idx.Documents, d)
			return nil
		},
	}

	if err := visitor.ParseStreaming(context.Background(), f); err != nil {
		return nil, fmt.Errorf("parse scip index: %w", err)
	}

	return idx, nil
}

// DocumentCount returns the number of documents in the index.
func (idx *Index) DocumentCount() int {
	return len(idx.Documents)
}
