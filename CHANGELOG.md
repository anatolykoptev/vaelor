# Changelog

## [1.37.2](https://github.com/anatolykoptev/go-code/compare/v1.37.1...v1.37.2) (2026-07-16)


### Changed

* **callgraph:** unified ingest→parse→build→enrich pipeline ([#463](https://github.com/anatolykoptev/go-code/issues/463)) ([#475](https://github.com/anatolykoptev/go-code/issues/475)) ([#478](https://github.com/anatolykoptev/go-code/issues/478)) ([2844f0f](https://github.com/anatolykoptev/go-code/commit/2844f0fe37104eb4007cc1fdcb63f2b0b541abcc))

## [1.37.1](https://github.com/anatolykoptev/go-code/compare/v1.37.0...v1.37.1) (2026-07-16)


### Fixed

* **callgraph:** apply stdlib filter to tree-sitter path, not just SCIP ([#466](https://github.com/anatolykoptev/go-code/issues/466)) ([#470](https://github.com/anatolykoptev/go-code/issues/470)) ([72984c1](https://github.com/anatolykoptev/go-code/commit/72984c183e02c3c0147dd9b9b084d11d0803284d))
* **ingest:** defensive copy in IngestRepo cache to prevent aliasing ([#477](https://github.com/anatolykoptev/go-code/issues/477)) ([9e3378f](https://github.com/anatolykoptev/go-code/commit/9e3378faff0887ec6955d0971a8c441624689aab))


### Performance

* **ingest:** process-level IngestRepo cache to eliminate redundant walks ([#464](https://github.com/anatolykoptev/go-code/issues/464)) ([#474](https://github.com/anatolykoptev/go-code/issues/474)) ([1256d34](https://github.com/anatolykoptev/go-code/commit/1256d34ab2ee9b6e443b3955aa7ac247924528c2))
* **scip:** parallelize multi-language SCIP indexing ([#465](https://github.com/anatolykoptev/go-code/issues/465)) ([#471](https://github.com/anatolykoptev/go-code/issues/471)) ([03cad27](https://github.com/anatolykoptev/go-code/commit/03cad27d5bb472b3c17cb90a279e9565bd0a6c53))


### Changed

* **callgraph:** move extractGoImplements into EnrichWithTypedResolution ([#467](https://github.com/anatolykoptev/go-code/issues/467)) ([#472](https://github.com/anatolykoptev/go-code/issues/472)) ([896f4b7](https://github.com/anatolykoptev/go-code/commit/896f4b7817db3d2903aa255e9028c5cde06bd715))
* **ingest:** unify parseFilesParallel into shared ingest.ParseFilesParallel ([#469](https://github.com/anatolykoptev/go-code/issues/469)) ([#473](https://github.com/anatolykoptev/go-code/issues/473)) ([0f06050](https://github.com/anatolykoptev/go-code/commit/0f060506f86b7870ae23974d8daa1f624e3f1344))

## [1.37.0](https://github.com/anatolykoptev/go-code/compare/v1.36.0...v1.37.0) (2026-07-16)


### Added

* **scip:** run SCIP indexers for ALL detected languages, not just dominant ([#459](https://github.com/anatolykoptev/go-code/issues/459)) ([749f236](https://github.com/anatolykoptev/go-code/commit/749f2368c92e8a0fc3ade6282a9d827c72aa1bc5))


### Changed

* **codegraph:** unify IMPLEMENTS edge paths — single construction via buildRelationshipEdges ([#461](https://github.com/anatolykoptev/go-code/issues/461)) ([d708e33](https://github.com/anatolykoptev/go-code/commit/d708e33d3aaf644da3f3bbc051b4fec57e924af5))

## [1.36.0](https://github.com/anatolykoptev/go-code/compare/v1.35.2...v1.36.0) (2026-07-16)


### Added

* **call_trace:** add refresh parameter to bypass in-memory cache ([#457](https://github.com/anatolykoptev/go-code/issues/457)) ([bc916a4](https://github.com/anatolykoptev/go-code/commit/bc916a47430ff31c83ecc2b2423eb19915467909))
* **scip:** filter stdlib method calls from SCIP edges to reduce call_trace noise ([#456](https://github.com/anatolykoptev/go-code/issues/456)) ([99feb08](https://github.com/anatolykoptev/go-code/commit/99feb0839e52e922129cbc67cbec797feaca0c85))


### Fixed

* **scip:** use content hash instead of mtimes for CacheKey — no false misses on git checkout ([#458](https://github.com/anatolykoptev/go-code/issues/458)) ([5417846](https://github.com/anatolykoptev/go-code/commit/54178469dd2302a6444d0771a541eff9001f1a03))

## [1.35.2](https://github.com/anatolykoptev/go-code/compare/v1.35.1...v1.35.2) (2026-07-16)


### Fixed

* **codegraph:** remove HasGoModule gate from buildAGECallGraph — enable SCIP for Rust ([#449](https://github.com/anatolykoptev/go-code/issues/449)) ([daeb2b0](https://github.com/anatolykoptev/go-code/commit/daeb2b014bca971e50e9ed3c63dcf57fb0b1da49))

## [1.35.1](https://github.com/anatolykoptev/go-code/compare/v1.35.0...v1.35.1) (2026-07-16)


### Fixed

* **codegraph:** emit IMPLEMENTS edge label for IsInterface call edges ([#447](https://github.com/anatolykoptev/go-code/issues/447)) ([1ab0d54](https://github.com/anatolykoptev/go-code/commit/1ab0d5439471261e5dae0d3288d985d67369b567))

## [1.35.0](https://github.com/anatolykoptev/go-code/compare/v1.34.1...v1.35.0) (2026-07-16)


### Added

* **scip:** extract IMPLEMENTS edges from Rust SCIP index — trait impl discovery ([#445](https://github.com/anatolykoptev/go-code/issues/445)) ([b839988](https://github.com/anatolykoptev/go-code/commit/b8399887a384efda16140d7180545096678339e8))

## [1.34.1](https://github.com/anatolykoptev/go-code/compare/v1.34.0...v1.34.1) (2026-07-16)


### Fixed

* **scip:** wire Cache into trySCIPResolution — skip re-indexing on cache hit ([#443](https://github.com/anatolykoptev/go-code/issues/443)) ([3b07816](https://github.com/anatolykoptev/go-code/commit/3b07816bca3c527d6229339c3c3a8f48fa01cf73))

## [1.34.0](https://github.com/anatolykoptev/go-code/compare/v1.33.1...v1.34.0) (2026-07-16)


### Added

* **oxcodes:** custom taint rules, anti-patterns, rewrite rejections, cache metrics ([#438](https://github.com/anatolykoptev/go-code/issues/438)) ([bfc5104](https://github.com/anatolykoptev/go-code/commit/bfc510452298fcec3e8a47ba4b7079993fc6335d))

## [1.33.1](https://github.com/anatolykoptev/go-code/compare/v1.33.0...v1.33.1) (2026-07-16)


### Fixed

* **call_trace:** rewrite TraceFromAGE with iterative BFS (AGE lacks list comprehension) ([#436](https://github.com/anatolykoptev/go-code/issues/436)) ([580da03](https://github.com/anatolykoptev/go-code/commit/580da03d3e1dce4c853a86a2f65e8cd1b4caa839))

## [1.33.0](https://github.com/anatolykoptev/go-code/compare/v1.32.3...v1.33.0) (2026-07-16)


### Added

* **call_trace:** fast path from AGE graph — avoid 2-60s repo reparse ([#434](https://github.com/anatolykoptev/go-code/issues/434)) ([e842112](https://github.com/anatolykoptev/go-code/commit/e842112f09098808ce4c5e23ca12987ad831ea34))

## [1.32.3](https://github.com/anatolykoptev/go-code/compare/v1.32.2...v1.32.3) (2026-07-16)


### Fixed

* **mcp:** raise code_graph timeout + non-blocking narrative + branch cleanup ([#433](https://github.com/anatolykoptev/go-code/issues/433)) ([2001cc6](https://github.com/anatolykoptev/go-code/commit/2001cc6dd85f41aecf6f96b4e9d54cabc9a83bc0))

## [1.32.2](https://github.com/anatolykoptev/go-code/compare/v1.32.1...v1.32.2) (2026-07-16)


### Fixed

* **codegraph:** prune stale dead-code scores when a function stops being an orphan ([#295](https://github.com/anatolykoptev/go-code/issues/295)) ([8d8c88b](https://github.com/anatolykoptev/go-code/commit/8d8c88b44120f0e0e791365faddcc1e69752a58d))
* **compare:** reuse tree-sitter parser per worker in BuildSnapshot ([#384](https://github.com/anatolykoptev/go-code/issues/384)) ([cc9d35d](https://github.com/anatolykoptev/go-code/commit/cc9d35dafa5eb5c45d50b912a535e3a2393895a2))

## [1.32.1](https://github.com/anatolykoptev/go-code/compare/v1.32.0...v1.32.1) (2026-07-16)


### Fixed

* **codegraph:** memory guard + chunked COPY to prevent OOM kernel panic ([#428](https://github.com/anatolykoptev/go-code/issues/428)) ([#429](https://github.com/anatolykoptev/go-code/issues/429)) ([9e188ad](https://github.com/anatolykoptev/go-code/commit/9e188ad3c947675279c58cdd9f5fd9a2accc1921))

## [1.32.0](https://github.com/anatolykoptev/go-code/compare/v1.31.11...v1.32.0) (2026-07-16)


### Added

* **importresolve:** stopgap virtual:* module resolution to defining package ([#423](https://github.com/anatolykoptev/go-code/issues/423)) ([#425](https://github.com/anatolykoptev/go-code/issues/425)) ([f24aff8](https://github.com/anatolykoptev/go-code/commit/f24aff86c6f543434617e179a9bae5975a92e864))

## [1.31.11](https://github.com/anatolykoptev/go-code/compare/v1.31.10...v1.31.11) (2026-07-16)


### Fixed

* **importresolve:** honor package.json exports map for workspace subpath imports ([#422](https://github.com/anatolykoptev/go-code/issues/422)) ([#424](https://github.com/anatolykoptev/go-code/issues/424)) ([0cff3a6](https://github.com/anatolykoptev/go-code/commit/0cff3a6ff83ca14387e0c85eaea9da3405964c7d))

## [1.31.10](https://github.com/anatolykoptev/go-code/compare/v1.31.9...v1.31.10) (2026-07-15)


### Fixed

* **review:** use valid ox-codes scope "function_bodies" in review_delta ([#420](https://github.com/anatolykoptev/go-code/issues/420)) ([cb7ea2c](https://github.com/anatolykoptev/go-code/commit/cb7ea2cc32f185052f1eb8c78d9a0dcdeb34d094)), closes [#419](https://github.com/anatolykoptev/go-code/issues/419)

## [1.31.9](https://github.com/anatolykoptev/go-code/compare/v1.31.8...v1.31.9) (2026-07-15)


### Fixed

* serialize EnsureGraph provisioning to fix pg_type 23505 race ([#417](https://github.com/anatolykoptev/go-code/issues/417)) ([1fbf8a4](https://github.com/anatolykoptev/go-code/commit/1fbf8a48631ecfe117421fdbfb9e15dd71a04028))

## [1.31.8](https://github.com/anatolykoptev/go-code/compare/v1.31.7...v1.31.8) (2026-07-14)


### Fixed

* **parser:** route Vue call extraction through the two-region ScriptCalls/MarkupCalls split ([#409](https://github.com/anatolykoptev/go-code/issues/409)) ([#414](https://github.com/anatolykoptev/go-code/issues/414)) ([7276002](https://github.com/anatolykoptev/go-code/commit/727600228c934f2286b2f3071018f958034d397d))


### Performance

* **review:** cap review_delta impacted_symbols by default ([#391](https://github.com/anatolykoptev/go-code/issues/391)) ([#415](https://github.com/anatolykoptev/go-code/issues/415)) ([600e5e0](https://github.com/anatolykoptev/go-code/commit/600e5e0cc0588fd79b15089a54e4ae738505e95d))

## [1.31.7](https://github.com/anatolykoptev/go-code/compare/v1.31.6...v1.31.7) (2026-07-14)


### Fixed

* sane fresh-deploy defaults for LLM model and /resolve rate limit ([#412](https://github.com/anatolykoptev/go-code/issues/412)) ([cba14fb](https://github.com/anatolykoptev/go-code/commit/cba14fb842451dcd73e9ce96ece7481345a283cf))

## [1.31.6](https://github.com/anatolykoptev/go-code/compare/v1.31.5...v1.31.6) (2026-07-14)


### Performance

* **parser:** share one tree between ParseFile and ExtractCalls ([#400](https://github.com/anatolykoptev/go-code/issues/400)) ([#408](https://github.com/anatolykoptev/go-code/issues/408)) ([1331695](https://github.com/anatolykoptev/go-code/commit/13316954ea28ca29b2191fdea9244633c804e900))

## [1.31.5](https://github.com/anatolykoptev/go-code/compare/v1.31.4...v1.31.5) (2026-07-14)


### Performance

* **parser:** add BenchmarkParseFile and BenchmarkBuildSnapshot ([#404](https://github.com/anatolykoptev/go-code/issues/404)) ([9b41631](https://github.com/anatolykoptev/go-code/commit/9b41631b646ab9b1c6993a70c8ad9539e5343b97))
* **parser:** single-parse Svelte runes instead of double parse ([#406](https://github.com/anatolykoptev/go-code/issues/406)) ([f453d47](https://github.com/anatolykoptev/go-code/commit/f453d475c4ab50ee4f339f1ffa01bf767c3557fd)), closes [#401](https://github.com/anatolykoptev/go-code/issues/401)

## [1.31.4](https://github.com/anatolykoptev/go-code/compare/v1.31.3...v1.31.4) (2026-07-14)


### Fixed

* shrink code_compare LLM prompt to fit 8k-token fleet models ([#398](https://github.com/anatolykoptev/go-code/issues/398)) ([5f74acd](https://github.com/anatolykoptev/go-code/commit/5f74acd664dd71ee35c540f9857eab3afe65d024))

## [1.31.3](https://github.com/anatolykoptev/go-code/compare/v1.31.2...v1.31.3) (2026-07-14)


### Fixed

* **llm:** default per-attempt timeout for chain rotation + review_delta 120s ([#391](https://github.com/anatolykoptev/go-code/issues/391)) ([#395](https://github.com/anatolykoptev/go-code/issues/395)) ([c65210c](https://github.com/anatolykoptev/go-code/commit/c65210c34b9f3a1d7b39df36ffdc15c756a85445))

## [1.31.2](https://github.com/anatolykoptev/go-code/compare/v1.31.1...v1.31.2) (2026-07-14)


### Fixed

* **review:** correct untested-symbol false positives in review_delta ([#392](https://github.com/anatolykoptev/go-code/issues/392)) ([b414402](https://github.com/anatolykoptev/go-code/commit/b4144027c3a89cd26accbd522dea8a27410c2e73))

## [1.31.1](https://github.com/anatolykoptev/go-code/compare/v1.31.0...v1.31.1) (2026-07-14)


### Fixed

* **test:** make TestSignalHitsLiveIntegration self-contained (nightly green) ([#389](https://github.com/anatolykoptev/go-code/issues/389)) ([39b3bdb](https://github.com/anatolykoptev/go-code/commit/39b3bdb5fcdae190117205efd9c4c4fe44ccdcda))

## [1.31.0](https://github.com/anatolykoptev/go-code/compare/v1.30.1...v1.31.0) (2026-07-14)


### Added

* **github_code_search:** add max_fragment_chars and max_total_chars ([#383](https://github.com/anatolykoptev/go-code/issues/383)) ([50cad3d](https://github.com/anatolykoptev/go-code/commit/50cad3dfa1eed923b7abb304f9e6d52796d50d04))

## [1.30.1](https://github.com/anatolykoptev/go-code/compare/v1.30.0...v1.30.1) (2026-07-14)


### Fixed

* **go-code:** accept owner/repo form in github_code_search tool ([#381](https://github.com/anatolykoptev/go-code/issues/381)) ([4ae079a](https://github.com/anatolykoptev/go-code/commit/4ae079a821604a5e113ca84d1b26763474f7b1eb))

## [1.30.0](https://github.com/anatolykoptev/go-code/compare/v1.29.0...v1.30.0) (2026-07-14)


### Added

* **compare:** wire ParseCache through BuildSnapshot/CompareInput ([d36a72c](https://github.com/anatolykoptev/go-code/commit/d36a72c41566f6967ca32475fa1ed549d9198915))


### Fixed

* **compare:** dedupe self-compare snapshots + ParseCache integration ([01891bf](https://github.com/anatolykoptev/go-code/commit/01891bfb489c24e5c201d94dd82f8b4ba429cf2d))

## [1.29.0](https://github.com/anatolykoptev/go-code/compare/v1.28.1...v1.29.0) (2026-07-14)


### Added

* port github_code_search from go-search to go-code ([#377](https://github.com/anatolykoptev/go-code/issues/377)) ([b970624](https://github.com/anatolykoptev/go-code/commit/b970624b88d878ed8a775ef9dd63ab867a6ee938))

## [1.28.1](https://github.com/anatolykoptev/go-code/compare/v1.28.0...v1.28.1) (2026-07-14)


### Fixed

* **code_graph:** return building status instead of tool error ([#361](https://github.com/anatolykoptev/go-code/issues/361)) ([6de3bba](https://github.com/anatolykoptev/go-code/commit/6de3bba70a919694fe3815bb3d6765f6636d3d95))

## [1.28.0](https://github.com/anatolykoptev/go-code/compare/v1.27.0...v1.28.0) (2026-07-14)


### Added

* **semantic_search:** add code_graph hint to indexing status ([#359](https://github.com/anatolykoptev/go-code/issues/359)) ([2d20399](https://github.com/anatolykoptev/go-code/commit/2d203997651f2548163489349b124f52a03259f9))

## [1.27.0](https://github.com/anatolykoptev/go-code/compare/v1.26.3...v1.27.0) (2026-07-14)


### Added

* **embeddings:** enable graph, hotspot, and recency arms in semantic_search RRF ([8c228c9](https://github.com/anatolykoptev/go-code/commit/8c228c912862b8d7c9c614a174a3b8953f518a53))
* **go-code:** enable graph, hotspot, and recency RRF arms in semantic_search ([ee30f88](https://github.com/anatolykoptev/go-code/commit/ee30f88fea203832b2139988bb4b7d80e13d1811))


### Fixed

* **federate:** pass asOf time.Time to CrossRepoCoChange to avoid wall-clock git log --since ([9325d79](https://github.com/anatolykoptev/go-code/commit/9325d79a7402b784b7332a8c4384b7fdef1c0521))
* **semantic_search:** strip AGE agtype quotes from complexity values ([d0ba33d](https://github.com/anatolykoptev/go-code/commit/d0ba33d3f2a030a6f48ed63648c3d62ed7167c70))

## [1.26.3](https://github.com/anatolykoptev/go-code/compare/v1.26.2...v1.26.3) (2026-07-13)


### Fixed

* **federate:** FU-1.1 — thread request ctx into ResolveRepos for cancellable origin dedup ([#337](https://github.com/anatolykoptev/go-code/issues/337)) ([fc12d53](https://github.com/anatolykoptev/go-code/commit/fc12d53400912e6d4e4f37c37baaaff5633c22d1))

## [1.26.2](https://github.com/anatolykoptev/go-code/compare/v1.26.1...v1.26.2) (2026-07-13)


### Fixed

* **codegraph:** FU-CG.9 — make route edge counters truthful (built vs unmatched) ([#335](https://github.com/anatolykoptev/go-code/issues/335)) ([2cce1a5](https://github.com/anatolykoptev/go-code/commit/2cce1a531dbfd898ae0a731332d4c9c0af26b7db))

## [1.26.1](https://github.com/anatolykoptev/go-code/compare/v1.26.0...v1.26.1) (2026-07-13)


### Fixed

* **codegraph:** add side to side-blind Route MATCH queries (FU-CG.8) ([#333](https://github.com/anatolykoptev/go-code/issues/333)) ([4ee9f1f](https://github.com/anatolykoptev/go-code/commit/4ee9f1fa14040223231508d8d6742f8524da21e0))

## [1.26.0](https://github.com/anatolykoptev/go-code/compare/v1.25.0...v1.26.0) (2026-07-13)


### Added

* **routes:** consolidate lineAt helper and add Line capture to 5 matchers (FU-CG.7) ([#331](https://github.com/anatolykoptev/go-code/issues/331)) ([513e808](https://github.com/anatolykoptev/go-code/commit/513e8085460993e5670029e684b65d26f5aafccc))


### Documentation

* **debug_investigate:** align hint_kind count with code ([#328](https://github.com/anatolykoptev/go-code/issues/328)) ([982ef21](https://github.com/anatolykoptev/go-code/commit/982ef2192ede4aeab4c37080f6817d8b701112b0))

## [1.25.0](https://github.com/anatolykoptev/go-code/compare/v1.24.0...v1.25.0) (2026-07-13)


### Added

* **resolve:** per-IP rate limit for POST /resolve ([#326](https://github.com/anatolykoptev/go-code/issues/326)) ([97cc6ed](https://github.com/anatolykoptev/go-code/commit/97cc6ed390a76b596f34ff3f60c4234c66100095))

## [1.24.0](https://github.com/anatolykoptev/go-code/compare/v1.23.6...v1.24.0) (2026-07-13)


### Added

* **sourcemap:** make sourcemap max body size configurable ([#324](https://github.com/anatolykoptev/go-code/issues/324)) ([a1ebdaf](https://github.com/anatolykoptev/go-code/commit/a1ebdafe84a42185bc4b10bb95cce430772b0aba))

## [1.23.6](https://github.com/anatolykoptev/go-code/compare/v1.23.5...v1.23.6) (2026-07-13)


### Documentation

* **adr:** add 0003 callgraph resolver strategy ([#322](https://github.com/anatolykoptev/go-code/issues/322)) ([4a8976a](https://github.com/anatolykoptev/go-code/commit/4a8976a5e919594a08898e1d27b82943f1883907))

## [1.23.5](https://github.com/anatolykoptev/go-code/compare/v1.23.4...v1.23.5) (2026-07-13)


### Fixed

* **call_trace:** normalize direction values to callers/callees ([#320](https://github.com/anatolykoptev/go-code/issues/320)) ([fbc1c1f](https://github.com/anatolykoptev/go-code/commit/fbc1c1face793cb117d9a135a701ccc2ae0bc31c))

## [1.23.4](https://github.com/anatolykoptev/go-code/compare/v1.23.3...v1.23.4) (2026-07-13)


### Fixed

* **debug_investigate:** drop t.Skip and document %q/%s label choice ([#318](https://github.com/anatolykoptev/go-code/issues/318)) ([f37117f](https://github.com/anatolykoptev/go-code/commit/f37117fa39c5965856327b917d1026a9d3db347f))

## [1.23.3](https://github.com/anatolykoptev/go-code/compare/v1.23.2...v1.23.3) (2026-07-13)


### Fixed

* **clients:** stop allocating httputil.Client on every call ([#316](https://github.com/anatolykoptev/go-code/issues/316)) ([a8c168c](https://github.com/anatolykoptev/go-code/commit/a8c168cd3b0809e0c36b73a487a2bedb10ccfed0))

## [1.23.2](https://github.com/anatolykoptev/go-code/compare/v1.23.1...v1.23.2) (2026-07-13)


### Fixed

* **codegraph:** enable typed call enrichment by default ([#314](https://github.com/anatolykoptev/go-code/issues/314)) ([c425f89](https://github.com/anatolykoptev/go-code/commit/c425f895ce14e606692b87587125d0fb23eab4e0))

## [1.23.1](https://github.com/anatolykoptev/go-code/compare/v1.23.0...v1.23.1) (2026-07-13)


### Fixed

* **release-please:** guard auto-merge step when no release PR ([#311](https://github.com/anatolykoptev/go-code/issues/311)) ([6c355f0](https://github.com/anatolykoptev/go-code/commit/6c355f0846b6c8e84259877dcbaef85dc0a171db))

## [1.23.0](https://github.com/anatolykoptev/go-code/compare/v1.22.2...v1.23.0) (2026-07-13)


### Features

* **envdetect:** ADR 0002 Phase 0 — static build/test/install command detection ([#296](https://github.com/anatolykoptev/go-code/issues/296)) ([011eddd](https://github.com/anatolykoptev/go-code/commit/011eddd82299d7d3a0e4cdad42c33406cd9ba375))


### Bug Fixes

* **callgraph:** wire typed call-edge resolution into the AGE-graph path for dead-code accuracy (BUG A, gated default-off) ([328b7d2](https://github.com/anatolykoptev/go-code/commit/328b7d2923dc416bdeaebb5b40bfe833aa1dafde))
* **compare:** raise code_compare deadline from 90s to 3m ([#309](https://github.com/anatolykoptev/go-code/issues/309)) ([987559e](https://github.com/anatolykoptev/go-code/commit/987559e95738d1d951b2d049b1b0864890a2dc73))
* **forge:** deflake metrics_test.go counter delta assertions ([#308](https://github.com/anatolykoptev/go-code/issues/308)) ([05a906e](https://github.com/anatolykoptev/go-code/commit/05a906ecbc06db16a803ee119cfb76801ae86160))
* **ingest:** accept comma-separated focus keywords ([#305](https://github.com/anatolykoptev/go-code/issues/305)) ([7148b9f](https://github.com/anatolykoptev/go-code/commit/7148b9f13e62642746fcfc57d4f3c9f92499fb61))
* **metrics:** scope code-graph age gauge to AUTO_INDEX_DIRS repos ([#291](https://github.com/anatolykoptev/go-code/issues/291)) ([808976e](https://github.com/anatolykoptev/go-code/commit/808976e32975abe643d1d3d412f36b0b70dd6d55))


### Performance Improvements

* **ci:** -short merge gate + nightly full suite (26m -&gt; ~min) ([#301](https://github.com/anatolykoptev/go-code/issues/301)) ([deba927](https://github.com/anatolykoptev/go-code/commit/deba927b56db92e4438fb010ca05919afbd575bf))
* **test:** parallelize DB-free test packages (gate ~8m -&gt; ~3.2m) ([#302](https://github.com/anatolykoptev/go-code/issues/302)) ([0d47c6f](https://github.com/anatolykoptev/go-code/commit/0d47c6fc74afac50349518de0dd458eec587b7fb))

## [1.22.2](https://github.com/anatolykoptev/go-code/compare/v1.22.1...v1.22.2) (2026-07-02)


### Bug Fixes

* **callgraph:** resolve generic-function callers in package-level var initializers ([#280](https://github.com/anatolykoptev/go-code/issues/280)) ([599cf02](https://github.com/anatolykoptev/go-code/commit/599cf02b6d17f912ca53a12a31a56ec1ad87e95e))
* **deadcode:** language-aware exported check for non-IsPublic languages ([#281](https://github.com/anatolykoptev/go-code/issues/281)) ([157f9b9](https://github.com/anatolykoptev/go-code/commit/157f9b91b62a56684fceefaa7c8aa7cf0667d218))
* **metrics:** pre-register alert-facing series at boot (graph age, zero-embeddings) ([#287](https://github.com/anatolykoptev/go-code/issues/287)) ([6ae4294](https://github.com/anatolykoptev/go-code/commit/6ae42941e3659afb247c2129806c5c0f6e3b8984))
* ParseCache drops call sites and ignores includeBody on hit ([#286](https://github.com/anatolykoptev/go-code/issues/286)) ([52427a3](https://github.com/anatolykoptev/go-code/commit/52427a3c9df882b56f413e1fc3b5c3803f83b71f))
* **review:** route PR-post write path through the multi-forge registry ([#284](https://github.com/anatolykoptev/go-code/issues/284)) ([bf32802](https://github.com/anatolykoptev/go-code/commit/bf3280267cd96440a5fccb5acb83ba32dcd009c3))

## [1.22.1](https://github.com/anatolykoptev/go-code/compare/v1.22.0...v1.22.1) (2026-07-01)


### Bug Fixes

* **release:** amd64 CGO CC override + consolidate to one goreleaser config ([#277](https://github.com/anatolykoptev/go-code/issues/277)) ([b49b0b3](https://github.com/anatolykoptev/go-code/commit/b49b0b32505c202d15e82f8cabed2ae79f265240))

## [1.22.0](https://github.com/anatolykoptev/go-code/compare/v1.21.0...v1.22.0) (2026-07-01)


### Features

* **metrics:** code_health/code_graph build-failure counters + AGE staleness gauge ([f115569](https://github.com/anatolykoptev/go-code/commit/f115569195dbd0525178bb7f996ecc6ce8905d7b))
* **parser:** astro alias resolution + vue SFC handler ([#241](https://github.com/anatolykoptev/go-code/issues/241)) ([d7724cf](https://github.com/anatolykoptev/go-code/commit/d7724cfe6b640edcbbe289d1f246dd863cad5a2b))
* **parser:** Astro markup {expr} calls + refs via shared tsxLang reparse ([#269](https://github.com/anatolykoptev/go-code/issues/269)) ([b5777c5](https://github.com/anatolykoptev/go-code/commit/b5777c5eb8bb9d86d78ecd30683f0ec69b188449))
* **parser:** Svelte component composition — TemplateRefs, USES edges, destructured $props() ([#270](https://github.com/anatolykoptev/go-code/issues/270)) ([fcba841](https://github.com/anatolykoptev/go-code/commit/fcba841e073d76d1380ecfc7a51c378840e40f04))
* **parser:** Svelte template-expressions + control-flow-effective calls/refs ([#271](https://github.com/anatolykoptev/go-code/issues/271)) ([c4302db](https://github.com/anatolykoptev/go-code/commit/c4302db1f7bb01b24b515fa5d9210638d691542e))


### Bug Fixes

* **astro:** narrow alias-counter emit-gate to broken declared aliases ([#243](https://github.com/anatolykoptev/go-code/issues/243)) ([c42afd4](https://github.com/anatolykoptev/go-code/commit/c42afd48669b11ae11d08425e3a4cdbf537dedfe))
* **code_health:** stop deleting a remote clone while the background snapshot is still reading it ([#246](https://github.com/anatolykoptev/go-code/issues/246)) ([02e0357](https://github.com/anatolykoptev/go-code/commit/02e035787d632d9c94d57a477404c77f8d539913))
* **compare,codegraph:** code_compare grade reflects freshness + language-aware isExported; [#253](https://github.com/anatolykoptev/go-code/issues/253) cleanup ([9e2c05f](https://github.com/anatolykoptev/go-code/commit/9e2c05f247f5433fcc6b48c0af82ae48b9fbc98d))
* **compare:** deterministic cycle-pair order in find2Cycles (flaky test) ([#272](https://github.com/anatolykoptev/go-code/issues/272)) ([dafcecf](https://github.com/anatolykoptev/go-code/commit/dafcecf1609ce270a9e3c88286ca77dc54e2c88c))
* **compare:** treat zero-dependency repos as N/A for freshness+vuln scoring ([2763dcd](https://github.com/anatolykoptev/go-code/commit/2763dcd3912b8c5badc5a665b22de351698cbb8b))
* **compare:** treat zero-dependency repos as N/A in code_compare grade (match code_health/[#250](https://github.com/anatolykoptev/go-code/issues/250)) ([aa6c42c](https://github.com/anatolykoptev/go-code/commit/aa6c42c8c4173f69db168ca183b8efb2c5895e13))
* **complexity:** unify cyclomatic complexity on parser as single owner ([39e89bb](https://github.com/anatolykoptev/go-code/commit/39e89bb6ccb0ae2869846760b7f70f6ec20b6879))
* **embeddings:** delete only true orphans (positive IN-list), not per-chunk anti-join ([728fe3c](https://github.com/anatolykoptev/go-code/commit/728fe3c062cccebef79b89c18bf46ea3b65950e4))
* **embeddings:** NUL-separate in-memory symbol keys (colon-in-path safe) + document dedup lossiness ([f20afcc](https://github.com/anatolykoptev/go-code/commit/f20afcc5330a8dbc38997db8e919f754fbe65753))
* **explore:** label health score as approximate with hint ([#249](https://github.com/anatolykoptev/go-code/issues/249)) ([7d6137a](https://github.com/anatolykoptev/go-code/commit/7d6137a3e5380ad97bc36d53b8a53e492aadf449))
* **mcp:** return tool results as application/json, not single-line SSE ([#245](https://github.com/anatolykoptev/go-code/issues/245)) ([7c9da8b](https://github.com/anatolykoptev/go-code/commit/7c9da8b0e0ef167c9ebb8f925e18a3e8255dd489))
* **metrics:** add per-symbol cognitive complexity and fix JS docRatio ([#247](https://github.com/anatolykoptev/go-code/issues/247)) ([4099691](https://github.com/anatolykoptev/go-code/commit/40996914774b232cf3cbd773376d4a316a1aad2a))
* **metrics:** unify health score and add arch fallback for unindexed repos ([#248](https://github.com/anatolykoptev/go-code/issues/248)) ([6db6ef2](https://github.com/anatolykoptev/go-code/commit/6db6ef2fdf413651996c386235ca93729a1a6070))
* **parser:** JS/TS-family Symbol.Language parity — .jsx/.js/.mjs/.cjs emit javascript ([#268](https://github.com/anatolykoptev/go-code/issues/268)) ([b758db4](https://github.com/anatolykoptev/go-code/commit/b758db41af3ae39c415e39823b7514fd0fe5c7c5))
* transfer table ownership on learnings + designmd store init ([#265](https://github.com/anatolykoptev/go-code/issues/265)) ([7768df5](https://github.com/anatolykoptev/go-code/commit/7768df5726d625db7e0f7350ee5eb240d10448da))


### Performance Improvements

* compact hand-built XML formatters + code_compare metrics json ([#261](https://github.com/anatolykoptev/go-code/issues/261)) ([4c8d2a2](https://github.com/anatolykoptev/go-code/commit/4c8d2a27abec9596a88bbaeccd069ee7c22a5e53))
* drop MCP response indentation + duration-only meta footer ([#260](https://github.com/anatolykoptev/go-code/issues/260)) ([e76ae8b](https://github.com/anatolykoptev/go-code/commit/e76ae8b0f286b65b4191aeb8d053dcc198c5aed4))

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
