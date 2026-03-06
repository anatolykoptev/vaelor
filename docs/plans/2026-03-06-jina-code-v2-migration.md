# Jina Code V2 Migration Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace multilingual-e5-large (1024-dim, slow) with Jina Code V2 (768-dim, 3.5x smaller, code-optimized) for semantic code search.

**Architecture:** Two-repo change. memdb-go gets multi-model ONNX support (model registry keyed by name, each with own dim/padID/prefix). go-code updates dimension constant and docker-compose env. pgvector table recreated with vector(768).

**Tech Stack:** Go, ONNX Runtime, pgvector, Docker

---

## Pre-requisites

- Download & quantize Jina Code V2 ONNX model
- Verify ONNX input/output tensor compatibility

## Task 1: Download Jina Code V2 ONNX Model

**Files:**
- Create: `/path/to/repos/deploy/example-server/models/jina-code-v2/model_quantized.onnx`
- Create: `/path/to/repos/deploy/example-server/models/jina-code-v2/tokenizer.json`

**Step 1: Export Jina Code V2 to ONNX**

On the server (Python with optimum):

```bash
pip install optimum[onnxruntime] transformers
optimum-cli export onnx \
  --model jinaai/jina-embeddings-v2-base-code \
  --task feature-extraction \
  --trust-remote-code \
  --opset 14 \
  /path/to/repos/deploy/example-server/models/jina-code-v2/
```

**Step 2: Quantize to INT8 (smaller, faster on CPU)**

```bash
python3 -c "
from optimum.onnxruntime import ORTQuantizer
from optimum.onnxruntime.configuration import AutoQuantizationConfig
q = ORTQuantizer.from_pretrained('/path/to/repos/deploy/example-server/models/jina-code-v2')
qconfig = AutoQuantizationConfig.avx512_vnni(is_static=False, per_channel=False)
q.quantize(save_dir='/path/to/repos/deploy/example-server/models/jina-code-v2/', quantization_config=qconfig)
"
# Rename: model_quantized.onnx should exist after quantization
```

**Step 3: Verify ONNX model inputs/outputs**

```bash
python3 -c "
import onnxruntime as ort
s = ort.InferenceSession('/path/to/repos/deploy/example-server/models/jina-code-v2/model_quantized.onnx')
print('Inputs:', [(i.name, i.shape) for i in s.get_inputs()])
print('Outputs:', [(o.name, o.shape) for o in s.get_outputs()])
"
```

Expected:
- Inputs: `input_ids` [batch, seq], `attention_mask` [batch, seq]
- Outputs: `last_hidden_state` [batch, seq, 768]

**Step 4: Copy tokenizer**

```bash
# tokenizer.json should already be in the export dir
ls -la /path/to/repos/deploy/example-server/models/jina-code-v2/
# Expect: model_quantized.onnx (~150-300MB), tokenizer.json (~17MB)
```

---

## Task 2: Make ONNX Embedder Model-Agnostic in memdb-go

**Files:**
- Modify: `/path/to/repos/src/MemDB/memdb-go/internal/embedder/onnx.go`
- Modify: `/path/to/repos/src/MemDB/memdb-go/internal/embedder/onnx_stub.go`
- Test: `/path/to/repos/src/MemDB/memdb-go/internal/embedder/onnx_test.go`

Currently hardcoded constants:
```go
const (
    e5Dim            = 1024
    e5MaxLen         = 512
    e5PadID          = 1    // XLM-RoBERTa
    onnxIntraOpThreads = 4
)
```

**Step 1: Add ModelConfig struct and make ONNXEmbedder configurable**

Replace hardcoded constants with a config struct:

```go
// ONNXModelConfig holds model-specific parameters for ONNX embedder.
type ONNXModelConfig struct {
    Dim    int // output embedding dimension (e.g. 1024 for e5, 768 for jina)
    MaxLen int // max token sequence length
    PadID  int // tokenizer pad token ID (1 for XLM-RoBERTa, 0 for BERT)
}

// Known model configs.
var knownModels = map[string]ONNXModelConfig{
    "multilingual-e5-large": {Dim: 1024, MaxLen: 512, PadID: 1},
    "jina-code-v2":          {Dim: 768, MaxLen: 512, PadID: 0},
}

// DefaultONNXConfig returns the e5-large config for backward compatibility.
func DefaultONNXConfig() ONNXModelConfig {
    return knownModels["multilingual-e5-large"]
}
```

**Step 2: Update ONNXEmbedder to use config**

Change `NewONNXEmbedder` signature:

```go
func NewONNXEmbedder(modelDir string, cfg ONNXModelConfig, logger *slog.Logger) (*ONNXEmbedder, error) {
```

Replace all `e5Dim` → `cfg.Dim`, `e5MaxLen` → `cfg.MaxLen`, `e5PadID` → `cfg.PadID` in onnx.go.

**Step 3: Update onnx_stub.go**

The stub (non-cgo build) must match the new signature:

```go
func NewONNXEmbedder(modelDir string, cfg ONNXModelConfig, logger *slog.Logger) (*ONNXEmbedder, error) {
    return nil, errors.New("onnx embedder requires CGO build")
}
```

**Step 4: Update factory.go to pass config**

In `New()`, the onnx case:

```go
case "onnx", "":
    if cfg.ONNXModelDir == "" {
        return nil, errors.New("embedder: onnx requires MEMDB_ONNX_MODEL_DIR")
    }
    modelCfg := DefaultONNXConfig()
    if mc, ok := knownModels[cfg.Model]; ok {
        modelCfg = mc
    }
    e, err := NewONNXEmbedder(cfg.ONNXModelDir, modelCfg, logger)
```

**Step 5: Run existing tests**

```bash
cd /path/to/repos/src/MemDB/memdb-go && go test ./internal/embedder/ -v
```

Expected: All tests pass (config is backward-compatible with defaults).

**Step 6: Commit**

```bash
cd /path/to/repos/src/MemDB/memdb-go
git add internal/embedder/onnx.go internal/embedder/onnx_stub.go internal/embedder/factory.go
git commit -m "feat: make ONNX embedder model-agnostic with configurable dim/padID/maxLen"
```

---

## Task 3: Multi-Model Support in memdb-go `/v1/embeddings`

**Files:**
- Modify: `/path/to/repos/src/MemDB/memdb-go/internal/embedder/factory.go` (add Config field)
- Modify: `/path/to/repos/src/MemDB/memdb-go/internal/handlers/embeddings.go`
- Modify: `/path/to/repos/src/MemDB/memdb-go/internal/server/server.go` (or wherever Handler is constructed)
- Create: `/path/to/repos/src/MemDB/memdb-go/internal/embedder/registry.go`

The `/v1/embeddings` endpoint currently uses a single `h.embedder`. We need a model registry so that `model` field in the request selects the right embedder.

**Step 1: Create embedder registry**

```go
// registry.go
package embedder

import "sync"

// Registry holds named embedders for multi-model support.
type Registry struct {
    mu       sync.RWMutex
    models   map[string]Embedder
    fallback string // default model name
}

func NewRegistry(fallback string) *Registry {
    return &Registry{models: make(map[string]Embedder), fallback: fallback}
}

func (r *Registry) Register(name string, e Embedder) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.models[name] = e
}

func (r *Registry) Get(name string) (Embedder, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    if name == "" {
        name = r.fallback
    }
    e, ok := r.models[name]
    return e, ok
}

func (r *Registry) Close() error {
    r.mu.Lock()
    defer r.mu.Unlock()
    for _, e := range r.models {
        e.Close()
    }
    return nil
}
```

**Step 2: Update Handler to use Registry instead of single Embedder**

In `embeddings.go`, change `h.embedder` to `h.registry.Get(req.Model)`:

```go
func (h *Handler) OpenAIEmbeddings(w http.ResponseWriter, r *http.Request) {
    // ... parse request ...

    emb, ok := h.registry.Get(req.Model)
    if !ok {
        h.writeOpenAIError(w, http.StatusBadRequest,
            "unknown model: "+req.Model, "invalid_request_error")
        return
    }

    // Determine prefix based on model (e5 needs "passage: ", jina doesn't)
    prefixed := applyPrefix(texts, req.Model)

    embeddings, err := emb.Embed(r.Context(), prefixed)
    // ...
}

func applyPrefix(texts []string, model string) []string {
    // e5 models need "passage: " prefix; jina models don't
    switch {
    case strings.Contains(model, "e5"):
        prefixed := make([]string, len(texts))
        for i, t := range texts {
            prefixed[i] = "passage: " + t
        }
        return prefixed
    default:
        return texts
    }
}
```

**Step 3: Wire up Registry in server initialization**

Where Handler is created, load both models if both model dirs exist:

```go
// Primary model (e5-large) — always loaded from MEMDB_ONNX_MODEL_DIR
registry := embedder.NewRegistry("multilingual-e5-large")
if primaryEmb != nil {
    registry.Register("multilingual-e5-large", primaryEmb)
}

// Secondary model (jina-code-v2) — loaded from MEMDB_ONNX_MODEL_DIR_CODE
codeModelDir := os.Getenv("MEMDB_ONNX_MODEL_DIR_CODE")
if codeModelDir != "" {
    codeCfg := embedder.ONNXModelConfig{Dim: 768, MaxLen: 512, PadID: 0}
    codeEmb, err := embedder.NewONNXEmbedder(codeModelDir, codeCfg, logger)
    if err != nil {
        logger.Warn("code embedder init failed", slog.Any("error", err))
    } else {
        registry.Register("jina-code-v2", codeEmb)
    }
}
```

**Step 4: Update tests**

In `embeddings_test.go`, add test for model selection:

```go
func TestOpenAIEmbeddings_ModelSelection(t *testing.T) {
    // Test that requesting unknown model returns error
    // Test that requesting "jina-code-v2" uses the jina embedder
    // Test that empty model falls back to e5-large
}
```

**Step 5: Run tests**

```bash
cd /path/to/repos/src/MemDB/memdb-go && go test ./internal/handlers/ -v -run TestOpenAI
```

**Step 6: Commit**

```bash
git add internal/embedder/registry.go internal/handlers/embeddings.go internal/server/server.go
git commit -m "feat: multi-model embedder registry for /v1/embeddings endpoint"
```

---

## Task 4: Update Docker Compose for Dual Models

**Files:**
- Modify: `/path/to/repos/deploy/example-server/docker-compose.yml`

**Step 1: Add second model volume and env var to memdb-go**

```yaml
memdb-go:
  volumes:
    - /path/to/repos/deploy/example-server/models/multilingual-e5-large:/models:ro
    - /path/to/repos/deploy/example-server/models/jina-code-v2:/models-code:ro
  environment:
    MEMDB_ONNX_MODEL_DIR: "/models"
    MEMDB_ONNX_MODEL_DIR_CODE: "/models-code"
```

**Step 2: Update go-code env vars**

```yaml
go-code:
  environment:
    - EMBED_URL=http://memdb-go:8080
    - EMBED_MODEL=jina-code-v2    # was: multilingual-e5-large
```

**Step 3: Increase memdb-go memory limit**

Two ONNX models need more RAM. e5-large ~560MB + jina-code-v2 ~300MB:

```yaml
memdb-go:
  deploy:
    resources:
      limits:
        memory: 3072M    # was: 2048M (or whatever current value)
```

**Step 4: Commit**

```bash
cd /path/to/repos/deploy/example-server
git add docker-compose.yml
git commit -m "feat: add jina-code-v2 model volume and env for memdb-go multi-model"
```

---

## Task 5: Update go-code Embedding Dimension (1024 → 768)

**Files:**
- Modify: `/path/to/repos/src/go-code/internal/embeddings/store.go` (dimSize, schemaSQL)
- Modify: `/path/to/repos/src/go-code/internal/embeddings/store_test.go` (dimSize ref)
- Modify: `/path/to/repos/src/go-code/internal/embeddings/client.go` (remove "passage: " prefix)
- Modify: `/path/to/repos/src/go-code/cmd/go-code/config.go` (default model name)

**Step 1: Update store.go constants and schema**

```go
const (
    defaultTopK  = 20
    maxTopK      = 100
    dimSize      = 768    // was: 1024
    batchSize    = 50
    fieldsPerRec = 8
)

const schemaSQL = `CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE IF NOT EXISTS code_embeddings (
    repo_key TEXT NOT NULL, file_path TEXT NOT NULL, symbol_name TEXT NOT NULL,
    symbol_kind TEXT NOT NULL, language TEXT NOT NULL DEFAULT '',
    start_line INT NOT NULL DEFAULT 0, body_hash BIGINT NOT NULL DEFAULT 0,
    embedding vector(768) NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (repo_key, file_path, symbol_name));
CREATE INDEX IF NOT EXISTS idx_code_embeddings_repo ON code_embeddings (repo_key);
CREATE INDEX IF NOT EXISTS idx_code_embeddings_hnsw ON code_embeddings
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64)`
```

**Step 2: Update client.go — remove "passage: " prefix**

Jina Code V2 does NOT use passage/query prefixes. Remove them from the client:

```go
// Embed returns embeddings for texts. Batches automatically.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
    if len(texts) == 0 {
        return nil, nil
    }
    return c.embedBatched(ctx, texts)
}

// EmbedQuery embeds a single search query.
func (c *Client) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
    res, err := c.embedBatched(ctx, []string{query})
    if err != nil {
        return nil, err
    }
    return res[0], nil
}
```

**Step 3: Update config.go default model**

```go
EmbedModel: env.Str("EMBED_MODEL", "jina-code-v2"),
```

**Step 4: Update store_test.go**

The test references `dimSize` — will auto-update since it's a constant. But verify:

```bash
cd /path/to/repos/src/go-code && go test ./internal/embeddings/ -v
```

**Step 5: Update client_test.go prefix tests**

Tests `TestPrefix_Passage` and `TestEmbedQuery_Prefix` check for "passage: " and "query: " prefixes. Update them:

```go
func TestEmbed_NoPrefix(t *testing.T) {
    var lastInput []string
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var req embeddingReq
        json.NewDecoder(r.Body).Decode(&req)
        lastInput = req.Input
        // ... return embeddings ...
    }))
    defer srv.Close()

    c := NewClient(srv.URL, "test-model")
    c.Embed(context.Background(), []string{"hello", "world"})

    if lastInput[0] != "hello" || lastInput[1] != "world" {
        t.Fatalf("expected no prefix, got %v", lastInput)
    }
}

func TestEmbedQuery_NoPrefix(t *testing.T) {
    // similar — query should be sent as-is
}
```

**Step 6: Run all tests**

```bash
cd /path/to/repos/src/go-code && go test ./... -v
```

**Step 7: Commit**

```bash
git add internal/embeddings/store.go internal/embeddings/client.go \
    internal/embeddings/store_test.go internal/embeddings/client_test.go \
    cmd/go-code/config.go
git commit -m "feat: switch to jina-code-v2 (768-dim, no prefix)"
```

---

## Task 6: Recreate pgvector Table & Deploy

**Step 1: Drop old embeddings table (dimension changed)**

```bash
docker compose exec postgres psql -U memos -d gocode -c "
DROP INDEX IF EXISTS idx_code_embeddings_hnsw;
DROP INDEX IF EXISTS idx_code_embeddings_repo;
DROP TABLE IF EXISTS code_embeddings;
"
```

**Step 2: Rebuild and deploy memdb-go**

```bash
cd /path/to/repos/deploy/example-server
docker compose build --no-cache memdb-go
docker compose up -d --no-deps --force-recreate memdb-go
```

**Step 3: Verify memdb-go loads both models**

```bash
docker compose logs --tail=20 memdb-go 2>&1 | grep -i "onnx\|embedder\|model"
```

Expected: two "onnx embedder loaded" lines (e5-large at /models, jina-code-v2 at /models-code).

**Step 4: Rebuild and deploy go-code**

```bash
cd /path/to/repos/src/go-code && make vendor
cd /path/to/repos/deploy/example-server
docker compose build --no-cache go-code
docker compose up -d --no-deps --force-recreate go-code
```

**Step 5: Verify go-code health**

```bash
curl http://127.0.0.1:8897/health
```

---

## Task 7: E2E Test

**Step 1: Trigger semantic search (starts indexing)**

```bash
# Init MCP session
RESP=$(curl -s -D- -X POST http://127.0.0.1:8897/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}')
SID=$(echo "$RESP" | grep -oP 'Mcp-Session-Id: \K\S+')

# Call semantic_search
curl -s -X POST http://127.0.0.1:8897/mcp \
  -H "Content-Type: application/json" \
  -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"semantic_search","arguments":{"repo":"/path/to/repos/src/go-mcpserver","query":"middleware handler chain"}}}'
```

Expected: "indexing" status (first run).

**Step 2: Monitor indexing speed**

```bash
# Watch memdb-go for jina-code-v2 embedding requests
docker compose logs -f memdb-go 2>&1 | grep "embeddings complete"
```

Expected: batches complete in ~5-8s (was ~25s with e5-large). 3-5x speedup.

**Step 3: Verify embeddings in DB**

```bash
# Wait for indexing to complete, then:
docker compose exec postgres psql -U memos -d gocode -c "
SELECT count(*), array_length(embedding::real[], 1) as dim
FROM code_embeddings LIMIT 1;
"
```

Expected: count=95, dim=768.

**Step 4: Retry semantic search for results**

```bash
curl -s -X POST http://127.0.0.1:8897/mcp \
  -H "Content-Type: application/json" \
  -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"semantic_search","arguments":{"repo":"/path/to/repos/src/go-mcpserver","query":"middleware handler chain"}}}'
```

Expected: 10 results with file paths, symbol names, distances.

**Step 5: Compare quality with e5-large baseline**

Previous results (e5-large):
- Rank 1: `middleware.go:Chain` distance=0.1213
- Rank 2: `hooks.go:Middleware` distance=0.1605

New results should have similar or tighter distances for code-related queries (Jina Code V2 is code-optimized).

**Step 6: Verify memdb still works (regression check)**

```bash
# memdb-go should still serve e5-large for MemDB
curl -s -X POST http://127.0.0.1:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{"input":"test text","model":"multilingual-e5-large"}' | python3 -m json.tool | head -5
```

Expected: 200 OK, embedding with 1024 dimensions.

---

## Rollback Plan

If anything breaks:

1. Revert go-code to e5-large: `EMBED_MODEL=multilingual-e5-large` in docker-compose
2. Revert store.go dimSize to 1024 and schema to vector(1024)
3. Drop and let schema auto-recreate
4. memdb-go: remove MEMDB_ONNX_MODEL_DIR_CODE env var (registry will only have e5)

---

## Expected Improvements

| Metric | Before (e5-large) | After (Jina Code V2) |
|--------|-------------------|---------------------|
| Model size | 560MB ONNX | ~150-300MB ONNX |
| Dimensions | 1024 | 768 |
| Batch speed | ~25s/8 texts | ~5-8s/8 texts (est.) |
| Code quality | General multilingual | Code-optimized (30 PL) |
| pgvector index | Larger (1024-dim) | 25% smaller (768-dim) |
| Total index time (95 funcs) | ~3 min | ~1 min (est.) |
