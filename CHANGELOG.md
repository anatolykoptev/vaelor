# Changelog

## [1.21.0](https://github.com/anatolykoptev/go-code/compare/v1.20.0...v1.21.0) (2026-06-20)


### Features

* **codegraph:** populate Go IMPLEMENTS edges via go/types satisfaction ([#220](https://github.com/anatolykoptev/go-code/issues/220)) ([beaa484](https://github.com/anatolykoptev/go-code/commit/beaa484edd59202b0f4b0394c9aff3a718131629))
* **embeddings:** cut model from jina-code-v2 to code-rank-embed ([#231](https://github.com/anatolykoptev/go-code/issues/231)) ([eceed51](https://github.com/anatolykoptev/go-code/commit/eceed511dc56b69fe9e483f3bc899310780693e6))
* **embeddings:** gocode_repo_info gauge — resolve opaque repo hash to path ([#227](https://github.com/anatolykoptev/go-code/issues/227)) ([bc67080](https://github.com/anatolykoptev/go-code/commit/bc670806af73c40ef44ae4b9323897ab3e7a15e0))
* **federate:** deadline-bounded federated_cochange with partial results + background prep ([#171](https://github.com/anatolykoptev/go-code/issues/171)) ([33f0e3d](https://github.com/anatolykoptev/go-code/commit/33f0e3da190410f61051ac41c549a113da99454e))
* find_duplicates — intra-repo semantic clone detector (5 phases) ([#215](https://github.com/anatolykoptev/go-code/issues/215)) ([8d68938](https://github.com/anatolykoptev/go-code/commit/8d68938f5fdf790258efebdbb324e0e309c7cc17))
* **go-code:** add nullable sparse_embedding sparsevec column (SPLADE P1) ([#194](https://github.com/anatolykoptev/go-code/issues/194)) ([ce90fde](https://github.com/anatolykoptev/go-code/commit/ce90fde117d87caa8b14aa6529b44aa7f4af82af))
* **go-code:** binary stale-demote safety-net for missed orphans (defense-in-depth) ([#210](https://github.com/anatolykoptev/go-code/issues/210)) ([bb2db6e](https://github.com/anatolykoptev/go-code/commit/bb2db6e817a3fa698731377f355b26eb95c020d5))
* **go-code:** BM25F lexical search arm over trigram candidates (BM25F P3) ([#206](https://github.com/anatolykoptev/go-code/issues/206)) ([a1c8c8e](https://github.com/anatolykoptev/go-code/commit/a1c8c8e78e5b876e36287a85e023da268488cd07))
* **go-code:** flag-gated BM25F keyword arm with grep fallback (BM25F P4) ([#207](https://github.com/anatolykoptev/go-code/issues/207)) ([7e2480f](https://github.com/anatolykoptev/go-code/commit/7e2480f7e41defca79d533b937ea97fd45167cb8))
* **go-code:** gated SPLADE sparse-vector indexing, batched by server cap (SPLADE P2) ([#195](https://github.com/anatolykoptev/go-code/issues/195)) ([44f9e7f](https://github.com/anatolykoptev/go-code/commit/44f9e7f0ea3e3ad459fac32c6259455319c3a246))
* **go-code:** graph-candidate generator as dark-launched 4th RRF arm (graph-first P1) ([#212](https://github.com/anatolykoptev/go-code/issues/212)) ([de3a099](https://github.com/anatolykoptev/go-code/commit/de3a099005ad32f498d79faf5c0657f8494c568a))
* **go-code:** index-time named execution flows (graph-first Phase 2 CORE) ([#213](https://github.com/anatolykoptev/go-code/issues/213)) ([bbecd49](https://github.com/anatolykoptev/go-code/commit/bbecd492ee12185ca50d778b63eea77f43af88ec))
* **go-code:** offline A/B harness for SPLADE arm (nDCG@10 + paired t-test gate, SPLADE P6) ([#199](https://github.com/anatolykoptev/go-code/issues/199)) ([5595fcf](https://github.com/anatolykoptev/go-code/commit/5595fcfde5a82fdc59760be531584e06e952e04f))
* **go-code:** operator-triggered sparse_backfill MCP tool (SPLADE P5) ([#198](https://github.com/anatolykoptev/go-code/issues/198)) ([9c200da](https://github.com/anatolykoptev/go-code/commit/9c200daf61d29bea6b33a88d504ca1046e0020c1))
* **go-code:** Phase 3a federated MCP foundation — repo resolver + cross-repo co-change ([#160](https://github.com/anatolykoptev/go-code/issues/160)) ([4969a70](https://github.com/anatolykoptev/go-code/commit/4969a70e9e2a0e6e460cbd0ee877928160d46b1c))
* **go-code:** Phase 3a.1 — federated co-change signal quality (origin-dedup + lift + sw.js filter) ([#161](https://github.com/anatolykoptev/go-code/issues/161)) ([3d69779](https://github.com/anatolykoptev/go-code/commit/3d69779cff5eff749060df282987c548ab41c6f3))
* **go-code:** Phase 3a.2 — Dunning G² significance ranking (two-tier, support-first) ([#162](https://github.com/anatolykoptev/go-code/issues/162)) ([7d050db](https://github.com/anatolykoptev/go-code/commit/7d050dbd628d76945bbca546ac2ba2130efc3e1c))
* **go-code:** Phase 3a.3 — Wilson-LB ranking + ubiquitous-file filter (CodeScene/Evan-Miller port) ([#163](https://github.com/anatolykoptev/go-code/issues/163)) ([a595370](https://github.com/anatolykoptev/go-code/commit/a5953705ca1964c2b8f937e548b600d9fba40c1d))
* **go-code:** Phase B — semantic route-match verification (verified-first cross-repo coupling) ([#164](https://github.com/anatolykoptev/go-code/issues/164)) ([98c388e](https://github.com/anatolykoptev/go-code/commit/98c388e4dca03e80ab57fd0f428f828ec3209fbc))
* **go-code:** resolve relative TS/JS imports to their package container ([#187](https://github.com/anatolykoptev/go-code/issues/187)) ([059ec4c](https://github.com/anatolykoptev/go-code/commit/059ec4ca37350c379c0c71f7206315070f58bb95))
* **go-code:** resolve TS $lib and @scope/workspace imports ([#189](https://github.com/anatolykoptev/go-code/issues/189)) ([aa86f18](https://github.com/anatolykoptev/go-code/commit/aa86f188b97e7f48f556d25519363158521b3633))
* **go-code:** sparse as dark-launched 3rd weighted-RRF arm (SPLADE P4) ([#197](https://github.com/anatolykoptev/go-code/issues/197)) ([bbd1f3d](https://github.com/anatolykoptev/go-code/commit/bbd1f3d45f324eb16b08a77bfbf44559c9ddc9d6))
* **go-code:** sparse retrieval + sparsevec HNSW index (SPLADE P3) ([#196](https://github.com/anatolykoptev/go-code/issues/196)) ([9d26c27](https://github.com/anatolykoptev/go-code/commit/9d26c271aa20909b36e274399b40f47037e8bc50))
* **ingest:** INDEX_SKIP_DIRS override + gocode_ingest_skipped_dirs_total counter ([#211](https://github.com/anatolykoptev/go-code/issues/211)) ([4d4602f](https://github.com/anatolykoptev/go-code/commit/4d4602f2396f4028e02ff6be2a9a94e68020f17e))
* **llm:** configurable cooldown TTL via LLM_COOLDOWN_SECONDS (default 15m) ([#234](https://github.com/anatolykoptev/go-code/issues/234)) ([413e636](https://github.com/anatolykoptev/go-code/commit/413e636fe461fe8bc399c97cc16e587544a96fad))
* **llm:** wire per-model cooldown + bump go-kit v0.83.0 ([#233](https://github.com/anatolykoptev/go-code/issues/233)) ([233681d](https://github.com/anatolykoptev/go-code/commit/233681d36d4848c78a74702bc366ed52f21a28df))
* **metrics:** wire ModelFilterObserver to Prometheus counters ([#230](https://github.com/anatolykoptev/go-code/issues/230)) ([f41597a](https://github.com/anatolykoptev/go-code/commit/f41597a1f4ca75210c2a08096ca84c5cbdfea7e0))


### Bug Fixes

* **autoindex:** emit skipped_no_vendor outcome + assert no-WARN contract ([#180](https://github.com/anatolykoptev/go-code/issues/180)) ([0508198](https://github.com/anatolykoptev/go-code/commit/0508198d020db22c2cac00395cdb433dbbd9b063))
* **codegraph:** repair fleet-wide HANDLES/FETCHES=0 — route→graph edge builder ([#167](https://github.com/anatolykoptev/go-code/issues/167)) ([0d6832d](https://github.com/anatolykoptev/go-code/commit/0d6832d1c29c8bfc33f28eab4e25fcc32329d146))
* **db:** reset pooled-conn search_path on release — bare code_* resolves public, not ag_catalog ([#173](https://github.com/anatolykoptev/go-code/issues/173)) ([a7842c2](https://github.com/anatolykoptev/go-code/commit/a7842c2c8c3d4fd5dff71ba2fc890f9b5fbdfe22))
* **embeddings:** incremental sync froze indexed_sha on first unsupported file in diff ([#170](https://github.com/anatolykoptev/go-code/issues/170)) ([e5f19c3](https://github.com/anatolykoptev/go-code/commit/e5f19c37c7d5fc4c30500ec8cc95c26f46b2da82))
* **embeddings:** rate-gate autoindex concurrency to 1 for single-worker embed backend ([#217](https://github.com/anatolykoptev/go-code/issues/217)) ([d46237e](https://github.com/anatolykoptev/go-code/commit/d46237e2e4e9287fcf7499fd086c42a8dd54e65d))
* **embeddings:** replace misleading freshness gauge with commits-behind + count SetRepoState write-failures ([#172](https://github.com/anatolykoptev/go-code/issues/172)) ([98085c0](https://github.com/anatolykoptev/go-code/commit/98085c0f4eece07ff2e7756bc5ddf6a2b3eb6f3b))
* **embeddings:** treat all 5xx as retryable; add embed_model per-row; continuous orphan gauge ([#232](https://github.com/anatolykoptev/go-code/issues/232)) ([50d40b2](https://github.com/anatolykoptev/go-code/commit/50d40b20ac8e9a623603201b9cc4149edb70a52c))
* **go-code:** batch build-time dead_code rerank to the server's per-request cap ([#191](https://github.com/anatolykoptev/go-code/issues/191)) ([8f05159](https://github.com/anatolykoptev/go-code/commit/8f05159b62bee95081ea41c107123bd9dddae242))
* **go-code:** embed HTTP timeout + bounded async index ctx + attributable cancel ([#216](https://github.com/anatolykoptev/go-code/issues/216)) ([b527a0b](https://github.com/anatolykoptev/go-code/commit/b527a0b3451681739ad82467b150dc7bde2cf552))
* **go-code:** exclude *_test.go imports from circular-dep detection ([#184](https://github.com/anatolykoptev/go-code/issues/184)) ([cd5ce38](https://github.com/anatolykoptev/go-code/commit/cd5ce3817f321a6051ea742948e9580fd7e57c52))
* **go-code:** group archgraph queries by package path, not base name ([#186](https://github.com/anatolykoptev/go-code/issues/186)) ([b6b2360](https://github.com/anatolykoptev/go-code/commit/b6b2360bfc5d9f735fc4d2fa13b6f6af2d7254ab))
* **go-code:** Phase 2a cleanup — 17 items (BUG-FH-1/2 closed, error encoding unified, +13 cosmetic) ([#157](https://github.com/anatolykoptev/go-code/issues/157)) ([bacce97](https://github.com/anatolykoptev/go-code/commit/bacce979fff85bc2da46de45484752a3f2a762dc))
* **go-code:** Phase 2b infra — Commits count, churn growth, since window, --follow, WithFreshness wiring ([#158](https://github.com/anatolykoptev/go-code/issues/158)) ([f7a0cb5](https://github.com/anatolykoptev/go-code/commit/f7a0cb5c3acb38c2831e1b0eedec8b930e40c9ac))
* **go-code:** pool AfterRelease RESET ALL, not DISCARD ALL (26000 regression) ([#176](https://github.com/anatolykoptev/go-code/issues/176)) ([68fa170](https://github.com/anatolykoptev/go-code/commit/68fa17005d221693faf884829a5ce63073380572))
* **go-code:** reconcile orphan embedding rows on full index + operator sweep (Bug B — phantom symbols) ([#209](https://github.com/anatolykoptev/go-code/issues/209)) ([caa34d5](https://github.com/anatolykoptev/go-code/commit/caa34d518baf65ea8103b846a1ff93ddba29d801))
* **go-code:** rerank via go-kit/rerank.Client, drop hardcoded embed-server URL ([#190](https://github.com/anatolykoptev/go-code/issues/190)) ([adead81](https://github.com/anatolykoptev/go-code/commit/adead81f0ae4b19f9a071a4588201940ce6428fb))
* **go-code:** self-index desync (SHA-gate data-aware) + HTTP-index-cancel observability ([#214](https://github.com/anatolykoptev/go-code/issues/214)) ([ec215cc](https://github.com/anatolykoptev/go-code/commit/ec215cc2cde0035540d40ca5268273b130824fa9))
* **go-code:** sparsevec batch size 500→100 (data-bound statement_timeout) + accurate write_failed counter ([#201](https://github.com/anatolykoptev/go-code/issues/201)) ([b0e5a8b](https://github.com/anatolykoptev/go-code/commit/b0e5a8bfe46209225e265e76df391e95bd1317ab))
* **go-code:** unify local package nodes (stop duplicate dir/import-path vertices) ([#185](https://github.com/anatolykoptev/go-code/issues/185)) ([ef83e22](https://github.com/anatolykoptev/go-code/commit/ef83e227ad1e9ea7a1945a7eae54b1a673623217))
* **graph-arm:** invert pagerank sub-generator — keyword-relevant ranked by pagerank ([#219](https://github.com/anatolykoptev/go-code/issues/219)) ([93b413f](https://github.com/anatolykoptev/go-code/commit/93b413f5e9da81667b2237e7aac5ff665708a4d9))
* **mcpmeta:** correct misleading stale-index remediation advice ([#169](https://github.com/anatolykoptev/go-code/issues/169)) ([2d2b9b6](https://github.com/anatolykoptev/go-code/commit/2d2b9b66553575629090c9e3da84e8ab67c6793e))
* **oxcodes:** bump structural-search HTTP timeout 10s-&gt;30s ([#168](https://github.com/anatolykoptev/go-code/issues/168)) ([c71889f](https://github.com/anatolykoptev/go-code/commit/c71889fedf084d3b6d6407100ab2da74e05c5960))
* **resolve:** resolve bare repo names against LocalRepoDirs registry ([#226](https://github.com/anatolykoptev/go-code/issues/226)) ([62adfe3](https://github.com/anatolykoptev/go-code/commit/62adfe3f818c1979c73f3f0ba63c4667537afc24))
* **semantic-search:** dedup semantic-only + CE-rerank results by file:symbol (Bug A) ([#208](https://github.com/anatolykoptev/go-code/issues/208)) ([c6650b9](https://github.com/anatolykoptev/go-code/commit/c6650b9d004f9181baca97133d38a08d7c250d00))
* **semhealth:** eliminate two find_duplicates false-positive classes ([#218](https://github.com/anatolykoptev/go-code/issues/218)) ([eba3622](https://github.com/anatolykoptev/go-code/commit/eba3622dec54f2a35a91afce947eb9a110623ba6))
* three go-code anomalies from 2026-06-12 investigation ([#228](https://github.com/anatolykoptev/go-code/issues/228)) ([23e245f](https://github.com/anatolykoptev/go-code/commit/23e245f95053acef5ed1b8b9c0ebee6517f54888))


### Performance Improvements

* **go-code:** batch sparse-embedding writes + raise backfill deadline ([#200](https://github.com/anatolykoptev/go-code/issues/200)) ([ea7e0ac](https://github.com/anatolykoptev/go-code/commit/ea7e0acd6b1f78ab2464c38281dd46d9d1c795e2))
* **go-code:** Phase 2c — batch initialCreationLines (BUG-FH-2b cold latency 34s→~3s) ([#159](https://github.com/anatolykoptev/go-code/issues/159)) ([3449902](https://github.com/anatolykoptev/go-code/commit/3449902a535d7d7392e7bd3a9d94d994a0c3d37a))
