package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

const (
	defaultTTLLocal  = 3600
	defaultTTLRemote = 86400
	defaultBatchSize = 500
	maxIndexFileBytes = 512 * 1024
)

// IndexConfig controls caching behaviour for IndexRepo.
type IndexConfig struct {
	TTLLocal  int // seconds, default 3600
	TTLRemote int // seconds, default 86400
	BatchSize int // vertices/edges per Cypher batch, default 500
}

// GraphMeta describes a built code graph stored in code_graph_meta.
type GraphMeta struct {
	RepoKey     string    `json:"repo_key"`
	RepoPath    string    `json:"repo_path"`
	GraphName   string    `json:"graph_name"`
	FileCount   int       `json:"file_count"`
	SymbolCount int       `json:"symbol_count"`
	EdgeCount   int       `json:"edge_count"`
	BuiltAt     time.Time `json:"built_at"`
	TTLSeconds  int       `json:"ttl_seconds"`
}

// vertexData holds label and properties for one graph vertex.
type vertexData struct {
	Label string
	Props map[string]string
}

// edgeData holds all fields needed to express one directed graph edge.
type edgeData struct {
	FromLabel string
	FromKey   string
	ToLabel   string
	ToKey     string
	EdgeLabel string
	Props     map[string]string
}

// indexParseResult holds symbols and calls parsed from one file.
type indexParseResult struct {
	file    *ingest.File
	symbols []*parser.Symbol
	calls   []parser.CallSite
}

// IndexRepo builds (or returns cached) a code graph for the repo at root.
//
// If the graph exists and is not stale it returns the cached GraphMeta immediately.
// Otherwise it drops any stale graph, rebuilds it from scratch, and persists GraphMeta.
func IndexRepo(ctx context.Context, store *Store, root string, isRemote bool, cfg IndexConfig) (*GraphMeta, error) {
	cfg = applyConfigDefaults(cfg)

	repoKey := graphName(root)
	gname := repoKey

	if cached, err := checkCache(ctx, store, repoKey, gname); err != nil {
		return nil, err
	} else if cached != nil {
		return cached, nil
	}

	if err := store.EnsureGraph(ctx, gname); err != nil {
		return nil, fmt.Errorf("ensure graph: %w", err)
	}

	allFiles, allSymbols, allCalls, err := ingestAndParse(ctx, root)
	if err != nil {
		return nil, err
	}

	cg := callgraph.BuildCallGraph(allSymbols, allCalls)
	vertices, edges := buildGraph(root, allFiles, allSymbols, cg)

	if err := insertBatches(ctx, store, gname, cfg.BatchSize, vertices, buildVertexBatch); err != nil {
		return nil, fmt.Errorf("insert vertices: %w", err)
	}

	if err := insertEdgeBatches(ctx, store, gname, cfg.BatchSize, edges); err != nil {
		return nil, fmt.Errorf("insert edges: %w", err)
	}

	ttl := cfg.TTLLocal
	if isRemote {
		ttl = cfg.TTLRemote
	}

	meta := &GraphMeta{
		RepoKey:     repoKey,
		RepoPath:    root,
		GraphName:   gname,
		FileCount:   len(allFiles),
		SymbolCount: len(allSymbols),
		EdgeCount:   len(edges),
		BuiltAt:     time.Now().UTC(),
		TTLSeconds:  ttl,
	}

	if err := upsertMeta(ctx, store, meta); err != nil {
		return nil, fmt.Errorf("upsert meta: %w", err)
	}

	return meta, nil
}

// applyConfigDefaults fills in zero-value fields with sensible defaults.
func applyConfigDefaults(cfg IndexConfig) IndexConfig {
	if cfg.TTLLocal <= 0 {
		cfg.TTLLocal = defaultTTLLocal
	}
	if cfg.TTLRemote <= 0 {
		cfg.TTLRemote = defaultTTLRemote
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBatchSize
	}
	return cfg
}

// checkCache returns existing fresh meta, nil+nil when rebuild is needed, or
// nil+err on a hard failure (e.g. drop failed).
func checkCache(ctx context.Context, store *Store, repoKey, gname string) (*GraphMeta, error) {
	existing, err := getMeta(ctx, store, repoKey)
	if err != nil || existing == nil {
		return nil, nil //nolint:nilerr // missing meta = no cache, not an error
	}
	if isFresh(existing.BuiltAt, existing.TTLSeconds) {
		return existing, nil
	}
	if dropErr := store.DropGraph(ctx, gname, repoKey); dropErr != nil {
		return nil, fmt.Errorf("drop stale graph: %w", dropErr)
	}
	return nil, nil
}

// ingestAndParse ingests a repository and parses all files in parallel.
func ingestAndParse(ctx context.Context, root string) ([]*ingest.File, []*parser.Symbol, []parser.CallSite, error) {
	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         root,
		MaxFileBytes: maxIndexFileBytes,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ingest repo: %w", err)
	}

	results := indexParseParallel(ctx, ir.Files)

	var allFiles []*ingest.File
	var allSymbols []*parser.Symbol
	var allCalls []parser.CallSite

	for _, r := range results {
		if r.file == nil {
			continue
		}
		allFiles = append(allFiles, r.file)
		allSymbols = append(allSymbols, r.symbols...)
		allCalls = append(allCalls, r.calls...)
	}

	return allFiles, allSymbols, allCalls, nil
}

// buildGraph constructs vertices and edges from ingested files and parsed symbols.
func buildGraph(root string, files []*ingest.File, symbols []*parser.Symbol, cg *callgraph.CallGraph) ([]vertexData, []edgeData) {
	// Collect unique packages (directories).
	pkgDirs := make(map[string]struct{})
	for _, f := range files {
		dir := filepath.Dir(f.RelPath)
		pkgDirs[dir] = struct{}{}
	}

	var vertices []vertexData
	var edges []edgeData

	// Package vertices.
	for dir := range pkgDirs {
		vertices = append(vertices, vertexData{
			Label: "Package",
			Props: map[string]string{
				"name": filepath.Base(dir),
				"path": dir,
				"repo": root,
			},
		})
	}

	// File vertices + CONTAINS (pkg→file) edges.
	for _, f := range files {
		lineCount := estimateLines(f)
		vertices = append(vertices, vertexData{
			Label: "File",
			Props: map[string]string{
				"path":     f.RelPath,
				"language": f.Language,
				"lines":    strconv.Itoa(lineCount),
			},
		})

		pkgDir := filepath.Dir(f.RelPath)
		edges = append(edges, edgeData{
			FromLabel: "Package",
			FromKey:   pkgDir,
			ToLabel:   "File",
			ToKey:     f.RelPath,
			EdgeLabel: "CONTAINS",
			Props:     map[string]string{},
		})
	}

	// Build a set of file RelPaths for fast lookup.
	fileRelPaths := make(map[string]string) // absPath → relPath
	for _, f := range files {
		fileRelPaths[f.Path] = f.RelPath
	}

	// Symbol vertices + CONTAINS (file→symbol) edges.
	for _, sym := range symbols {
		relFile := relPath(sym.File, root)
		symKey := sym.Name + ":" + relFile
		vertices = append(vertices, vertexData{
			Label: "Symbol",
			Props: map[string]string{
				"name":       sym.Name,
				"kind":       string(sym.Kind),
				"signature":  sym.Signature,
				"file":       relFile,
				"start_line": strconv.Itoa(int(sym.StartLine)),
				"end_line":   strconv.Itoa(int(sym.EndLine)),
			},
		})

		edges = append(edges, edgeData{
			FromLabel: "File",
			FromKey:   relFile,
			ToLabel:   "Symbol",
			ToKey:     symKey,
			EdgeLabel: "CONTAINS",
			Props:     map[string]string{},
		})
	}

	// IMPORTS edges (File→Package).
	// We need to re-parse imports; they are stored in ParseResult which is not
	// available here. We rely on the call graph's original parse results being
	// built elsewhere. The import edges are best-effort: if a file was parsed
	// we have ParseResult.Imports. Since we don't store ParseResults in the
	// parallel results, we skip IMPORTS for now — they can be added in a
	// follow-up pass. (The schema supports them via templates.go queries.)

	// CALLS edges (Symbol→Symbol).
	for _, ce := range cg.Edges {
		if ce.Caller == nil || ce.Callee == nil {
			continue
		}
		callerRelFile := relPath(ce.Caller.File, root)
		calleeRelFile := relPath(ce.Callee.File, root)
		edges = append(edges, edgeData{
			FromLabel: "Symbol",
			FromKey:   ce.Caller.Name + ":" + callerRelFile,
			ToLabel:   "Symbol",
			ToKey:     ce.Callee.Name + ":" + calleeRelFile,
			EdgeLabel: "CALLS",
			Props: map[string]string{
				"line": strconv.Itoa(int(ce.Line)),
			},
		})
	}

	return vertices, edges
}

// indexParseParallel parses all files concurrently and returns results.
func indexParseParallel(ctx context.Context, files []*ingest.File) []indexParseResult {
	results := make([]indexParseResult, len(files))

	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}

	work := make(chan int, len(files))
	for i := range files {
		work <- i
	}
	close(work)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				if ctx.Err() != nil {
					return
				}
				results[idx] = indexParseFile(files[idx])
			}
		}()
	}

	wg.Wait()
	return results
}

func indexParseFile(f *ingest.File) indexParseResult {
	source, err := os.ReadFile(f.Path)
	if err != nil {
		return indexParseResult{}
	}

	opts := parser.ParseOpts{
		Language:       f.Language,
		IncludeImports: true,
	}

	pr, err := parser.ParseFile(f.Path, source, opts)
	if err != nil {
		return indexParseResult{file: f}
	}

	calls, _ := parser.ExtractCalls(f.Path, source, opts)

	return indexParseResult{
		file:    f,
		symbols: pr.Symbols,
		calls:   calls,
	}
}

// estimateLines returns an estimate of line count based on file size.
// Actual line counting requires reading the file; we use a heuristic.
func estimateLines(f *ingest.File) int {
	const avgBytesPerLine = 40
	if f.Size <= 0 {
		return 0
	}
	n := int(f.Size) / avgBytesPerLine
	if n < 1 {
		n = 1
	}
	return n
}

// getMeta retrieves the stored GraphMeta for repoKey, or returns nil if none exists.
func getMeta(ctx context.Context, store *Store, repoKey string) (*GraphMeta, error) {
	conn, err := store.Pool().Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	var m GraphMeta
	err = conn.QueryRow(ctx, `
		SELECT repo_key, repo_path, graph_name,
		       file_count, symbol_count, edge_count,
		       built_at, ttl_seconds
		FROM code_graph_meta
		WHERE repo_key = $1`, repoKey,
	).Scan(
		&m.RepoKey, &m.RepoPath, &m.GraphName,
		&m.FileCount, &m.SymbolCount, &m.EdgeCount,
		&m.BuiltAt, &m.TTLSeconds,
	)
	if err != nil {
		return nil, fmt.Errorf("query meta: %w", err)
	}
	return &m, nil
}

// upsertMeta inserts or updates the GraphMeta row.
func upsertMeta(ctx context.Context, store *Store, meta *GraphMeta) error {
	conn, err := store.Pool().Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	_, err = conn.Exec(ctx, `
		INSERT INTO code_graph_meta
		    (repo_key, repo_path, graph_name, file_count, symbol_count, edge_count, built_at, ttl_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (repo_key) DO UPDATE SET
		    repo_path    = EXCLUDED.repo_path,
		    graph_name   = EXCLUDED.graph_name,
		    file_count   = EXCLUDED.file_count,
		    symbol_count = EXCLUDED.symbol_count,
		    edge_count   = EXCLUDED.edge_count,
		    built_at     = EXCLUDED.built_at,
		    ttl_seconds  = EXCLUDED.ttl_seconds`,
		meta.RepoKey, meta.RepoPath, meta.GraphName,
		meta.FileCount, meta.SymbolCount, meta.EdgeCount,
		meta.BuiltAt, meta.TTLSeconds,
	)
	if err != nil {
		return fmt.Errorf("upsert meta: %w", err)
	}
	return nil
}

// isFresh reports whether builtAt is within ttlSeconds of the current time.
func isFresh(builtAt time.Time, ttlSeconds int) bool {
	if ttlSeconds <= 0 {
		return false
	}
	return time.Since(builtAt) < time.Duration(ttlSeconds)*time.Second
}

// buildVertexBatch generates a Cypher statement that MERGEs all vertices in batch.
func buildVertexBatch(graphName string, vertices []vertexData) string {
	if len(vertices) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, v := range vertices {
		key := vertexKey(v)
		varName := fmt.Sprintf("v%d", i)
		fmt.Fprintf(&sb, "MERGE (%s:%s {%s})\n", varName, v.Label, matchKey(v.Label, key))
		if len(v.Props) > 0 {
			fmt.Fprintf(&sb, "ON CREATE SET %s\n", formatSet(varName, v.Props))
			fmt.Fprintf(&sb, "ON MATCH SET %s\n", formatSet(varName, v.Props))
		}
	}
	sb.WriteString("RETURN 1")
	return sb.String()
}

// buildEdgeBatch generates a Cypher statement that MATCHes endpoints and MERGEs edges.
func buildEdgeBatch(graphName string, edges []edgeData) string {
	if len(edges) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, e := range edges {
		fromVar := fmt.Sprintf("f%d", i)
		toVar := fmt.Sprintf("t%d", i)
		edgeVar := fmt.Sprintf("e%d", i)
		fmt.Fprintf(&sb, "MATCH (%s:%s {%s})\n", fromVar, e.FromLabel, matchKey(e.FromLabel, e.FromKey))
		fmt.Fprintf(&sb, "MATCH (%s:%s {%s})\n", toVar, e.ToLabel, matchKey(e.ToLabel, e.ToKey))
		if len(e.Props) > 0 {
			fmt.Fprintf(&sb, "MERGE (%s)-[%s:%s {%s}]->(%s)\n",
				fromVar, edgeVar, e.EdgeLabel, formatProps(e.Props), toVar)
		} else {
			fmt.Fprintf(&sb, "MERGE (%s)-[%s:%s]->(%s)\n",
				fromVar, edgeVar, e.EdgeLabel, toVar)
		}
	}
	sb.WriteString("RETURN 1")
	return sb.String()
}

// vertexKey returns the primary key value for a vertex.
// Package: keyed by path. File: keyed by path. Symbol: keyed by "name:file".
func vertexKey(v vertexData) string {
	switch v.Label {
	case "Package":
		if p, ok := v.Props["path"]; ok {
			return p
		}
		return v.Props["name"]
	case "File":
		return v.Props["path"]
	case "Symbol":
		name := v.Props["name"]
		file := v.Props["file"]
		return name + ":" + file
	default:
		return v.Props["name"]
	}
}

// matchKey builds the Cypher property filter for a MATCH/MERGE by label and key.
// Package: if key contains "/" match by path, else by name.
// Symbol: split "name:file" into name + file props.
// Everything else: match by path.
func matchKey(label, key string) string {
	switch label {
	case "Package":
		if strings.Contains(key, "/") {
			return fmt.Sprintf("path: '%s'", escapeCypher(key))
		}
		return fmt.Sprintf("name: '%s'", escapeCypher(key))
	case "Symbol":
		idx := strings.Index(key, ":")
		if idx >= 0 {
			name := key[:idx]
			file := key[idx+1:]
			return fmt.Sprintf("name: '%s', file: '%s'", escapeCypher(name), escapeCypher(file))
		}
		return fmt.Sprintf("name: '%s'", escapeCypher(key))
	default:
		return fmt.Sprintf("path: '%s'", escapeCypher(key))
	}
}

// formatProps renders a Props map as a Cypher inline property literal.
// e.g. {key: 'value', key2: 'value2'}
func formatProps(props map[string]string) string {
	if len(props) == 0 {
		return ""
	}
	parts := make([]string, 0, len(props))
	for k, v := range props {
		parts = append(parts, fmt.Sprintf("%s: '%s'", k, escapeCypher(v)))
	}
	return strings.Join(parts, ", ")
}

// formatSet renders a SET clause fragment for a variable.
// e.g. v.key = 'value', v.key2 = 'value2'
func formatSet(varName string, props map[string]string) string {
	parts := make([]string, 0, len(props))
	for k, v := range props {
		parts = append(parts, fmt.Sprintf("%s.%s = '%s'", varName, k, escapeCypher(v)))
	}
	return strings.Join(parts, ", ")
}

// relPath returns the path of abs relative to root.
// If abs does not have root as prefix, it returns abs unchanged.
func relPath(abs, root string) string {
	if root == "" {
		return abs
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return rel
}

// insertBatches splits vertices into batches of size batchSize and executes
// each batch as a Cypher write.
func insertBatches(
	ctx context.Context,
	store *Store,
	gname string,
	batchSize int,
	vertices []vertexData,
	buildFn func(string, []vertexData) string,
) error {
	for i := 0; i < len(vertices); i += batchSize {
		end := i + batchSize
		if end > len(vertices) {
			end = len(vertices)
		}
		batch := vertices[i:end]
		cypher := buildFn(gname, batch)
		if cypher == "" {
			continue
		}
		if err := store.ExecCypherWrite(ctx, gname, cypher); err != nil {
			return fmt.Errorf("batch [%d:%d]: %w", i, end, err)
		}
	}
	return nil
}

// insertEdgeBatches splits edges into batches and executes each as a Cypher write.
func insertEdgeBatches(ctx context.Context, store *Store, gname string, batchSize int, edges []edgeData) error {
	for i := 0; i < len(edges); i += batchSize {
		end := i + batchSize
		if end > len(edges) {
			end = len(edges)
		}
		batch := edges[i:end]
		cypher := buildEdgeBatch(gname, batch)
		if cypher == "" {
			continue
		}
		if err := store.ExecCypherWrite(ctx, gname, cypher); err != nil {
			return fmt.Errorf("edge batch [%d:%d]: %w", i, end, err)
		}
	}
	return nil
}
