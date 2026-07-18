package embeddings

import (
	"bytes"
	"encoding/gob"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// cachedFileSymbols is the gob payload stored under each per-file key.
//
// ModTime and Size feed the validator on every L1 hit. Entries carry the
// minimum data needed to rebuild a []symbolEntry without re-parsing.
type cachedFileSymbols struct {
	ModTime int64 // file modtime in unix nanos at the moment of caching
	Size    int64 // file size in bytes at the moment of caching
	Entries []wireSymbolEntry
}

// wireSymbolEntry is the gob-friendly projection of a per-symbol cache row.
//
// Mirrors the subset of parser.Symbol fields actually consumed by the
// downstream indexer (embedAndUpsert + buildEmbedText output). Gob can encode
// pointers transparently but we flatten to plain structs to keep the wire
// format obvious in code review and stable across parser.Symbol additions.
type wireSymbolEntry struct {
	// Symbol fields
	Name       string
	Kind       string // parser.NodeKind
	Language   string
	File       string
	StartLine  uint32
	EndLine    uint32
	Signature  string
	Body       string
	DocComment string
	Complexity int
	BodyHash   uint64
	Receiver   string
	IsPublic   bool
	Attributes []string
	RuneKind   string

	// Precomputed pipeline output (avoids buildEmbedText + textHash on hit).
	Hash      uint64
	EmbedText string
}

// encodePayload encodes the full cachedFileSymbols struct via gob.
func encodePayload(p cachedFileSymbols) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodePayload inverts encodePayload.
func decodePayload(b []byte) (cachedFileSymbols, error) {
	var p cachedFileSymbols
	if err := gob.NewDecoder(bytes.NewReader(b)).Decode(&p); err != nil {
		return cachedFileSymbols{}, err
	}
	return p, nil
}

// deflateEntries projects []symbolEntry into the wire format.
func deflateEntries(entries []symbolEntry) []wireSymbolEntry {
	out := make([]wireSymbolEntry, len(entries))
	for i, e := range entries {
		out[i] = wireSymbolEntry{
			Name:       e.sym.Name,
			Kind:       string(e.sym.Kind),
			Language:   e.sym.Language,
			File:       e.sym.File,
			StartLine:  e.sym.StartLine,
			EndLine:    e.sym.EndLine,
			Signature:  e.sym.Signature,
			Body:       e.sym.Body,
			DocComment: e.sym.DocComment,
			Complexity: e.sym.Complexity,
			BodyHash:   e.sym.BodyHash,
			Receiver:   e.sym.Receiver,
			IsPublic:   e.sym.IsPublic,
			Attributes: e.sym.Attributes,
			RuneKind:   e.sym.RuneKind,
			Hash:       e.hash,
			EmbedText:  e.embedText,
		}
	}
	return out
}

// inflateEntries inverts deflateEntries, reusing the live ingest.File pointer
// (its absolute path drives downstream embed_text composition and we trust
// the validator to have proven freshness).
func inflateEntries(p cachedFileSymbols, f *ingest.File) []symbolEntry {
	out := make([]symbolEntry, len(p.Entries))
	for i, w := range p.Entries {
		sym := &parser.Symbol{
			Name:       w.Name,
			Kind:       parser.NodeKind(w.Kind),
			Language:   w.Language,
			File:       w.File,
			StartLine:  w.StartLine,
			EndLine:    w.EndLine,
			Signature:  w.Signature,
			Body:       w.Body,
			DocComment: w.DocComment,
			Complexity: w.Complexity,
			BodyHash:   w.BodyHash,
			Receiver:   w.Receiver,
			IsPublic:   w.IsPublic,
			Attributes: w.Attributes,
			RuneKind:   w.RuneKind,
		}
		out[i] = symbolEntry{
			sym:       sym,
			file:      f,
			hash:      w.Hash,
			embedText: w.EmbedText,
		}
	}
	return out
}
