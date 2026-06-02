// Package codegraph sparse retrieval — Phase P1 stub.
//
// This file vendors go-kit/sparse and declares the SparseEmbedder interface
// alias used by the SPLADE RRF arm (Phase P1). Implementation is in Phase P1;
// this stub exists so go mod vendor includes the sparse package.
package codegraph

import "github.com/anatolykoptev/go-kit/sparse"

// SparseEmbedder is the interface satisfied by *sparse.Client (go-kit/sparse).
// Declared here so Phase P1 can wire it without importing the concrete type
// in every call site.
type SparseEmbedder = sparse.SparseEmbedder
