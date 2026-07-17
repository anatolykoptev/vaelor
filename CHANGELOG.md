# Changelog

## [1.38.1](https://github.com/anatolykoptev/go-code/compare/v1.38.0...v1.38.1) (2026-07-17)


### Documentation

* rewrite README for launch (capability-led, source-verified claims) ([#482](https://github.com/anatolykoptev/go-code/issues/482)) ([f47d66b](https://github.com/anatolykoptev/go-code/commit/f47d66b5dedcb022a8decdc3f8c84a5f089fd2da))

## [1.38.0](https://github.com/anatolykoptev/go-code/compare/v1.37.2...v1.38.0) (2026-07-17)


### Added

* add arch_central section to repo_analyze with top-5 PageRank symbols ([35b023d](https://github.com/anatolykoptev/go-code/commit/35b023de7834dbd5a89c7e07c75b3aea6bc0bfd9))
* add explain_architecture + hotspot_files code_graph templates ([07e461d](https://github.com/anatolykoptev/go-code/commit/07e461d5c9269f47c82e51ce8f2bbcd78f9f0e2a))
* add recent_commits and top_coupled_files to explore ([29aaf0a](https://github.com/anatolykoptev/go-code/commit/29aaf0ac3f5f4f20e219444b87737f9479ad1732))
* **analyze:** rank.go fusion via WeightedRRF (opt-in via ANALYZE_RANK_FUSION_MODE=rrf) ([aeb0c22](https://github.com/anatolykoptev/go-code/commit/aeb0c22d06e7905604464679eb35f286d9170467))
* **analyze:** rank.go fusion via WeightedRRF (opt-in) ([0a08802](https://github.com/anatolykoptev/go-code/commit/0a0880228d40f097db2b7385f2be709918fc9c94))
* annotate removed symbols in review_delta with dead_code_score ([5b31f90](https://github.com/anatolykoptev/go-code/commit/5b31f90aa1b71113306ca7797cf926697c35e305))
* annotate review_pr removed symbols with dead_code_score ([7b252c8](https://github.com/anatolykoptev/go-code/commit/7b252c83b41b864f8b51f120f4cf9c5d08b8c860))
* async go/types warm-up for cold GOCACHE ([fe56a7f](https://github.com/anatolykoptev/go-code/commit/fe56a7f489305ebe89c7e431272d998854e1aa0c))
* **autoindex:** skip repos whose main branch hasn't moved ([#10](https://github.com/anatolykoptev/go-code/issues/10)) ([552a910](https://github.com/anatolykoptev/go-code/commit/552a91052297ba637ce019f8030e35c425057358))
* **b3:** expand body window to +50 lines when EndLine unknown ([#86](https://github.com/anatolykoptev/go-code/issues/86)) ([43cb7a5](https://github.com/anatolykoptev/go-code/commit/43cb7a51b58c3217564ab1afac18b8f7b4df82f0))
* background computation + 1h cache for code_health ([45cddd5](https://github.com/anatolykoptev/go-code/commit/45cddd5a0e8f760f8c8f92dbb5f05adadf4b798c))
* boost code_research files by AGE symbol PageRank ([9a1a7de](https://github.com/anatolykoptev/go-code/commit/9a1a7de7173826780773ec5ea46701baa51fb0b5))
* **bootstrap:** self-grant ownership + create perms; fail-fast on missing ag_catalog access ([#112](https://github.com/anatolykoptev/go-code/issues/112)) ([4bb419f](https://github.com/anatolykoptev/go-code/commit/4bb419f5a865172c83f35e595e0da6314a7db5f0))
* **call_trace:** add refresh parameter to bypass in-memory cache ([#457](https://github.com/anatolykoptev/go-code/issues/457)) ([935f848](https://github.com/anatolykoptev/go-code/commit/935f848c7743284f468e28ec78baa7eaf1188f3b))
* **call_trace:** fast path from AGE graph — avoid 2-60s repo reparse ([#434](https://github.com/anatolykoptev/go-code/issues/434)) ([9e23c28](https://github.com/anatolykoptev/go-code/commit/9e23c2816451a1488c6e6b859d74f5ecf5d00aa6))
* **callgraph:** eager GOCACHE warm at startup for AUTO_INDEX_DIRS Go repos ([#35](https://github.com/anatolykoptev/go-code/issues/35)) ([613cc4d](https://github.com/anatolykoptev/go-code/commit/613cc4d70ae7070bf93be0244adf9169332d8dcf))
* **codegraph:** build FETCHES FromKey as Handler:File composite (Wave 5) ([#154](https://github.com/anatolykoptev/go-code/issues/154)) ([d9d848c](https://github.com/anatolykoptev/go-code/commit/d9d848c37b6f4116506def28fd5e3837cff9c1d9))
* **codegraph:** build HANDLES FromKey as Handler:File composite (Wave 6) ([#155](https://github.com/anatolykoptev/go-code/issues/155)) ([0e10fff](https://github.com/anatolykoptev/go-code/commit/0e10fff35acd3e46f53104deeb415dcddf76f95d))
* **codegraph:** populate Go IMPLEMENTS edges via go/types satisfaction ([#220](https://github.com/anatolykoptev/go-code/issues/220)) ([6d42ab9](https://github.com/anatolykoptev/go-code/commit/6d42ab992b4281a12ac8c60f8caafcc49f149d17))
* **codegraph:** preflight graph-existence check on read-path ([#43](https://github.com/anatolykoptev/go-code/issues/43)) ([d349c74](https://github.com/anatolykoptev/go-code/commit/d349c7475655128dccb62fb722acee9914fd18d2))
* **compare:** wire ParseCache through BuildSnapshot/CompareInput ([606b64f](https://github.com/anatolykoptev/go-code/commit/606b64fc19d029e6f33f9b18c123767e8b9613fc))
* dead code metrics in code_health with CE confidence scores ([dcce95b](https://github.com/anatolykoptev/go-code/commit/dcce95bd7917f648e5820526ff52aace978c3aec))
* debug_investigate MCP tool — Prometheus + Jaeger + symbol correlation ([#56](https://github.com/anatolykoptev/go-code/issues/56)) ([2747b03](https://github.com/anatolykoptev/go-code/commit/2747b030f37b47db8e02d5278111bcec4043afdf))
* **debug_investigate:** latency + saturation spike detection (Phase β.4) ([#63](https://github.com/anatolykoptev/go-code/issues/63)) ([dddd4f3](https://github.com/anatolykoptev/go-code/commit/dddd4f38dfd4d4bbfe70e19ff2743b53e5d3aa45))
* **debug_investigate:** Phase 3 — direct symbol resolution via OTEL code.* tags (closes [#74](https://github.com/anatolykoptev/go-code/issues/74)) ([#77](https://github.com/anatolykoptev/go-code/issues/77)) ([69810e1](https://github.com/anatolykoptev/go-code/commit/69810e19b934b64ba7b97009fd3888aec974dda0))
* **debug_investigate:** Phase 6 — log excerpts via dozor side-car (β.3b) ([#66](https://github.com/anatolykoptev/go-code/issues/66)) ([d79c205](https://github.com/anatolykoptev/go-code/commit/d79c205c6bf4549b6a33601905e3ed4a9b82b690))
* **debug_investigate:** Phase α — auto-discovery, sourcemap resolver, hint_kind, SRP split ([#61](https://github.com/anatolykoptev/go-code/issues/61)) ([5327a74](https://github.com/anatolykoptev/go-code/commit/5327a743e42b91dc00b1050c3831304a084374f2))
* **debug_investigate:** Phase γ.B — dead-code filter + impact + symbol body ([#69](https://github.com/anatolykoptev/go-code/issues/69)) ([fa2148c](https://github.com/anatolykoptev/go-code/commit/fa2148c8e8a5d84e45ebb0835fbb50458d7eff23))
* **debug_investigate:** Phase γ.C — historical incidents + hint-driven candidate hypotheses ([#70](https://github.com/anatolykoptev/go-code/issues/70)) ([18cdc6a](https://github.com/anatolykoptev/go-code/commit/18cdc6a535830adaadcb3c5bbca0b38400c06a88))
* **debug_investigate:** Phase γ.D — multi-signal fusion + recent diff embedding ([#71](https://github.com/anatolykoptev/go-code/issues/71)) ([7f578b2](https://github.com/anatolykoptev/go-code/commit/7f578b2cdf25e2563b87d25e57c3f7176d53eb88))
* **debug_investigate:** Phase γ.E — LLM cache + structured next_check (machine-readable) ([#72](https://github.com/anatolykoptev/go-code/issues/72)) ([df46b42](https://github.com/anatolykoptev/go-code/commit/df46b42d4ed174e142c13b1cf0e4c85b7eba9ee9))
* **debug_investigate:** Prometheus alerts ingestion (Phase β.5) — captures constant-state invariant violations ([#64](https://github.com/anatolykoptev/go-code/issues/64)) ([f0f8dfa](https://github.com/anatolykoptev/go-code/commit/f0f8dfa1e314a70169b41c1f1b72525b2039567b))
* **debug_investigate:** Sprint B1 — function body in LLM context (deep code reasoning) ([#79](https://github.com/anatolykoptev/go-code/issues/79)) ([9d91584](https://github.com/anatolykoptev/go-code/commit/9d91584b0cffdda5c5850247e5d50958a962d51b))
* **debug_investigate:** Sprint B2 — upstream callgraph walk for root-cause discovery ([#80](https://github.com/anatolykoptev/go-code/issues/80)) ([84831c2](https://github.com/anatolykoptev/go-code/commit/84831c2b2789daf5512f3d27a1dccdcdfe566a4f))
* **debug_investigate:** Sprint B4/B5 — downstream callees walk + body excerpts top-5 ([#88](https://github.com/anatolykoptev/go-code/issues/88)) ([8f886d6](https://github.com/anatolykoptev/go-code/commit/8f886d69c004c966036a4d2e42c8aedc9ddcda1c))
* drop-in httpmw.NewServeMux + slogh trace correlation ([#95](https://github.com/anatolykoptev/go-code/issues/95)) ([60c5258](https://github.com/anatolykoptev/go-code/commit/60c5258d557f2336237e01f0c9202457d4d6783b))
* **embeddings:** autoindex concurrency cap + retry-with-backoff (28min→14min cold-start) ([#4](https://github.com/anatolykoptev/go-code/issues/4)) ([fbb0a8a](https://github.com/anatolykoptev/go-code/commit/fbb0a8af4c911388abb1e949e0d3c37d04943a52))
* **embeddings:** cache symbol entries via go-kit cache.GetIfValid (-80% embed-server traffic) ([#5](https://github.com/anatolykoptev/go-code/issues/5)) ([771275a](https://github.com/anatolykoptev/go-code/commit/771275a7e79b9b97ad32365fb9dfd6f5ec08a294))
* **embeddings:** cut model from jina-code-v2 to code-rank-embed ([#231](https://github.com/anatolykoptev/go-code/issues/231)) ([20c43d9](https://github.com/anatolykoptev/go-code/commit/20c43d907bdccb5dbee802fb5e468d105086f82d))
* **embeddings:** enable graph, hotspot, and recency arms in semantic_search RRF ([7e62092](https://github.com/anatolykoptev/go-code/commit/7e620922a2a3d247028e74f6a80345442868ab1f))
* **embeddings:** file-level IndexFile primitive for incremental indexing ([bc3c77f](https://github.com/anatolykoptev/go-code/commit/bc3c77fd1ee638b84fc3503d283d5b5eff3fd7a6))
* **embeddings:** file-level IndexFile primitive for incremental indexing ([3ba53d8](https://github.com/anatolykoptev/go-code/commit/3ba53d87f9150d17ef2f8279d6aba6577cea3e55))
* **embeddings:** gocode_repo_info gauge — resolve opaque repo hash to path ([#227](https://github.com/anatolykoptev/go-code/issues/227)) ([0db6977](https://github.com/anatolykoptev/go-code/commit/0db69772b383cd47cf62a94181d77815e98ed0b1))
* **embeddings:** IncrementalSync orchestrator using git-diff reconciliation ([64f5850](https://github.com/anatolykoptev/go-code/commit/64f58507744558ecdd6f059d76f6cb2672d87601))
* **embeddings:** IncrementalSync orchestrator using git-diff reconciliation ([78fc5d2](https://github.com/anatolykoptev/go-code/commit/78fc5d243dc27a01fbb9f2ae3dea0b31fdaf276d))
* **embeddings:** WeightedRRF static weights via RRF_WEIGHT_SEMANTIC/KEYWORD env ([#7](https://github.com/anatolykoptev/go-code/issues/7)) ([4107e16](https://github.com/anatolykoptev/go-code/commit/4107e162913906898fb8ac4f1089fa2d5e176122))
* **envdetect:** ADR 0002 Phase 0 — static build/test/install command detection ([#296](https://github.com/anatolykoptev/go-code/issues/296)) ([b968e54](https://github.com/anatolykoptev/go-code/commit/b968e54e3204f3889d66956a70c1ae750a3f06ac))
* **eval:** offline retrieval-quality harness for go-code ([#6](https://github.com/anatolykoptev/go-code/issues/6)) ([807da18](https://github.com/anatolykoptev/go-code/commit/807da189bf89e3763d7d6c17815b1a8eec4bfd37))
* expose apply=true in go-code rewrite tool ([bb1aee7](https://github.com/anatolykoptev/go-code/commit/bb1aee7d39a196cba5fb61a56fc195c7c40dab1c))
* **federate:** deadline-bounded federated_cochange with partial results + background prep ([#171](https://github.com/anatolykoptev/go-code/issues/171)) ([6ac85ad](https://github.com/anatolykoptev/go-code/commit/6ac85ad7052a4f6dea562cd15bc6be5ebfdaf338))
* filter compiled artifacts from coupling and explore dead code ([5675663](https://github.com/anatolykoptev/go-code/commit/5675663a9a4bb105e0e9fc5ca465611c5f180f76))
* find_duplicates — intra-repo semantic clone detector (5 phases) ([#215](https://github.com/anatolykoptev/go-code/issues/215)) ([e0d41e6](https://github.com/anatolykoptev/go-code/commit/e0d41e663d44692f6d10f27edf39356b35f72716))
* **fleet/ssh:** shadow-copy ~/.ssh to writable dir to bypass strict-mode check ([#130](https://github.com/anatolykoptev/go-code/issues/130)) ([eddb8da](https://github.com/anatolykoptev/go-code/commit/eddb8da374507248b4470514cd97057c8caea4ea))
* **fleet:** multi-host hosts[] input + cross-host SiblingDrift ([#132](https://github.com/anatolykoptev/go-code/issues/132)) ([c35ffc0](https://github.com/anatolykoptev/go-code/commit/c35ffc06c1c0eac305781cae63ebf7df559d84aa))
* **fleet:** runtime binary version awareness — fleet_versions tool + debug_investigate Phase 7 ([#124](https://github.com/anatolykoptev/go-code/issues/124)) ([4c1196d](https://github.com/anatolykoptev/go-code/commit/4c1196d0b34f98cc12d98f64137b44d891e62415))
* **fleet:** upstream changelog correlation for TagDrift rows ([#133](https://github.com/anatolykoptev/go-code/issues/133)) ([c106906](https://github.com/anatolykoptev/go-code/commit/c106906dfa6813445b6e036f5a9c85468cf45385))
* **forge:** GitHub App authentication for separate rate-limit pool ([#39](https://github.com/anatolykoptev/go-code/issues/39)) ([662b168](https://github.com/anatolykoptev/go-code/commit/662b168610a593059964978a58d7c398484dd30e))
* **github_code_search:** add max_fragment_chars and max_total_chars ([#383](https://github.com/anatolykoptev/go-code/issues/383)) ([e568200](https://github.com/anatolykoptev/go-code/commit/e56820053dd29f24b41a3b58d02d274b40720d5e))
* **go-code:** add nullable sparse_embedding sparsevec column (SPLADE P1) ([#194](https://github.com/anatolykoptev/go-code/issues/194)) ([6af5f9e](https://github.com/anatolykoptev/go-code/commit/6af5f9e51b928730e611390526fb4e7825c2a958))
* **go-code:** binary stale-demote safety-net for missed orphans (defense-in-depth) ([#210](https://github.com/anatolykoptev/go-code/issues/210)) ([66b6a45](https://github.com/anatolykoptev/go-code/commit/66b6a451cf7b4bc7c7ff9e41997a8ddc675e5bee))
* **go-code:** BM25F lexical search arm over trigram candidates (BM25F P3) ([#206](https://github.com/anatolykoptev/go-code/issues/206)) ([8903d37](https://github.com/anatolykoptev/go-code/commit/8903d37b1e17e8ec47537eda54b14496e4c1c89a))
* **go-code:** enable graph, hotspot, and recency RRF arms in semantic_search ([b9add3e](https://github.com/anatolykoptev/go-code/commit/b9add3ece0755e85a45ce5ac5cba4bde275d450f))
* **go-code:** flag-gated BM25F keyword arm with grep fallback (BM25F P4) ([#207](https://github.com/anatolykoptev/go-code/issues/207)) ([2acd5b0](https://github.com/anatolykoptev/go-code/commit/2acd5b073292af494b4592351ecdf3dd7addf907))
* **go-code:** gated SPLADE sparse-vector indexing, batched by server cap (SPLADE P2) ([#195](https://github.com/anatolykoptev/go-code/issues/195)) ([31b7bf1](https://github.com/anatolykoptev/go-code/commit/31b7bf191803f6fa92c30e19b86e3b1a94cd7ef8))
* **go-code:** graph-candidate generator as dark-launched 4th RRF arm (graph-first P1) ([#212](https://github.com/anatolykoptev/go-code/issues/212)) ([0563deb](https://github.com/anatolykoptev/go-code/commit/0563debe9676e38e7e531f50800392601d5106a1))
* **go-code:** index-time named execution flows (graph-first Phase 2 CORE) ([#213](https://github.com/anatolykoptev/go-code/issues/213)) ([217593b](https://github.com/anatolykoptev/go-code/commit/217593bdd4472b8c9120dfc1ebe1128f1f047e3e))
* **go-code:** offline A/B harness for SPLADE arm (nDCG@10 + paired t-test gate, SPLADE P6) ([#199](https://github.com/anatolykoptev/go-code/issues/199)) ([c06c0d4](https://github.com/anatolykoptev/go-code/commit/c06c0d4bf28ec2f15640a91b6937a59156677f12))
* **go-code:** operator-triggered sparse_backfill MCP tool (SPLADE P5) ([#198](https://github.com/anatolykoptev/go-code/issues/198)) ([0c3d5af](https://github.com/anatolykoptev/go-code/commit/0c3d5af7a8cc6e95ddbb8644cfe0f993d4ef9e16))
* **go-code:** Phase 3a federated MCP foundation — repo resolver + cross-repo co-change ([#160](https://github.com/anatolykoptev/go-code/issues/160)) ([54cce3c](https://github.com/anatolykoptev/go-code/commit/54cce3c3085f82ff659e78e33d2b8e5cac805c1c))
* **go-code:** Phase 3a.1 — federated co-change signal quality (origin-dedup + lift + sw.js filter) ([#161](https://github.com/anatolykoptev/go-code/issues/161)) ([5746a0a](https://github.com/anatolykoptev/go-code/commit/5746a0adf6b568287b322d1ed540db790f3c634d))
* **go-code:** Phase 3a.2 — Dunning G² significance ranking (two-tier, support-first) ([#162](https://github.com/anatolykoptev/go-code/issues/162)) ([f66534e](https://github.com/anatolykoptev/go-code/commit/f66534e2e4c1d82cb91d7c53f7387c0f042f19ef))
* **go-code:** Phase 3a.3 — Wilson-LB ranking + ubiquitous-file filter (CodeScene/Evan-Miller port) ([#163](https://github.com/anatolykoptev/go-code/issues/163)) ([6062b31](https://github.com/anatolykoptev/go-code/commit/6062b311eb6307ddea1d0858f8d78698ea8c2fe1))
* **go-code:** Phase B — semantic route-match verification (verified-first cross-repo coupling) ([#164](https://github.com/anatolykoptev/go-code/issues/164)) ([25f885f](https://github.com/anatolykoptev/go-code/commit/25f885fec0a0a612cf9f09c591b181799fa088fa))
* **go-code:** port repowise patterns — Phase 1 (_meta envelope + biomarkers + 2 new tools) ([#156](https://github.com/anatolykoptev/go-code/issues/156)) ([33f4f01](https://github.com/anatolykoptev/go-code/commit/33f4f014d050dce2869becad30d6fc0357df5332))
* **go-code:** resolve relative TS/JS imports to their package container ([#187](https://github.com/anatolykoptev/go-code/issues/187)) ([3666037](https://github.com/anatolykoptev/go-code/commit/36660374b872b697cffb066c754566762bc2eb70))
* **go-code:** resolve TS $lib and @scope/workspace imports ([#189](https://github.com/anatolykoptev/go-code/issues/189)) ([350f185](https://github.com/anatolykoptev/go-code/commit/350f1853a528332bd27213424a9696c736552730))
* **go-code:** sparse as dark-launched 3rd weighted-RRF arm (SPLADE P4) ([#197](https://github.com/anatolykoptev/go-code/issues/197)) ([a817eb2](https://github.com/anatolykoptev/go-code/commit/a817eb23b49897fa4c8e34a4d0e15bf20e3af27f))
* **go-code:** sparse retrieval + sparsevec HNSW index (SPLADE P3) ([#196](https://github.com/anatolykoptev/go-code/issues/196)) ([d205078](https://github.com/anatolykoptev/go-code/commit/d205078d9eadb24b84bb121221742b973292be29))
* **html:** Wave 3 — applicable cross-cuts + docs 15→16 + MAJOR-2 fix ([#152](https://github.com/anatolykoptev/go-code/issues/152)) ([e0ab94d](https://github.com/anatolykoptev/go-code/commit/e0ab94d45c2cbbb521fc995e4aeb929b634448bc))
* **html:** Wave 4 — enclosing-template scope tracking → Route.Handler ([#153](https://github.com/anatolykoptev/go-code/issues/153)) ([5bd0596](https://github.com/anatolykoptev/go-code/commit/5bd0596293c1a2c0cfed028d4a2475237b52b349))
* **image:** add openssh-client to runtime so fleet_versions ssh-probe works ([#129](https://github.com/anatolykoptev/go-code/issues/129)) ([0410ac4](https://github.com/anatolykoptev/go-code/commit/0410ac428fa6682b4779c9283e1260156dca75bb))
* **importresolve:** stopgap virtual:* module resolution to defining package ([#423](https://github.com/anatolykoptev/go-code/issues/423)) ([#425](https://github.com/anatolykoptev/go-code/issues/425)) ([cfa43ed](https://github.com/anatolykoptev/go-code/commit/cfa43ed2be9f248bd9fc41197542b938f5b5b08f))
* improve dead_code detection for Rust pub functions ([57f9bd5](https://github.com/anatolykoptev/go-code/commit/57f9bd542cfbcdba04e92dfab37487babf14d377))
* in-memory LRU+TTL cache for BuildFromRepo ([962cae5](https://github.com/anatolykoptev/go-code/commit/962cae5924e3650ddc6d1237d8aa63ba6a914634))
* **ingest:** add MaxFiles cap to SnapshotOpts and IngestOpts ([018ce4b](https://github.com/anatolykoptev/go-code/commit/018ce4bae8ea0a5d4d6e5263d0d35ac419249653))
* **ingest:** INDEX_SKIP_DIRS override + gocode_ingest_skipped_dirs_total counter ([#211](https://github.com/anatolykoptev/go-code/issues/211)) ([f8acfc8](https://github.com/anatolykoptev/go-code/commit/f8acfc8019458adc5869ae4e345f493f77c45995))
* **ingest:** surface skip reasons in IngestResult + index.go log ([#113](https://github.com/anatolykoptev/go-code/issues/113)) ([0ed0cbd](https://github.com/anatolykoptev/go-code/commit/0ed0cbd6e3019702ae7e862db631c5b41fde7d24))
* **investigate:** Tasks 5+6 — OperationToFuncName + Hypothesis/RankHypotheses ([#51](https://github.com/anatolykoptev/go-code/issues/51)) ([491840e](https://github.com/anatolykoptev/go-code/commit/491840efdab088e7ba98bd637fdbca7607404d5a))
* **investigate:** Tasks 7+8 — InvestigationStore + BuildSystemPrompt ([#52](https://github.com/anatolykoptev/go-code/issues/52)) ([ed97bd9](https://github.com/anatolykoptev/go-code/commit/ed97bd98017f5ba47a25637338ee24d73a811731))
* **jaegerclient:** bootstrap Jaeger HTTP client + ListServices + FindTraces + GetTrace ([#47](https://github.com/anatolykoptev/go-code/issues/47)) ([fea6fe7](https://github.com/anatolykoptev/go-code/commit/fea6fe7dd52a179f0a30e72448009ba785c99007))
* **kotlin:** Wave 3 — cross-cutting integration (tested_by, speculative, astdiff, importcat, apisurf, delta) ([#146](https://github.com/anatolykoptev/go-code/issues/146)) ([45d67fa](https://github.com/anatolykoptev/go-code/commit/45d67fa10d7105f540a91b25e7d9781df143eb56))
* **llm:** circuit breaker + observability middleware for LLM client ([#120](https://github.com/anatolykoptev/go-code/issues/120)) ([a7b34fc](https://github.com/anatolykoptev/go-code/commit/a7b34fc826f0216b4811917b6e2d7d82177264c9))
* **llm:** configurable cooldown TTL via LLM_COOLDOWN_SECONDS (default 15m) ([#234](https://github.com/anatolykoptev/go-code/issues/234)) ([91b1e0c](https://github.com/anatolykoptev/go-code/commit/91b1e0ca3c53294dad94d454a760b8caaf537341))
* **llm:** expose LLM_PER_ATTEMPT_TIMEOUT for model chains ([35374dd](https://github.com/anatolykoptev/go-code/commit/35374dd6ab239eace805cbdd76cfc0c199b623d6))
* **llm:** make LLM optional (Completer iface + per-tool degrade policy) ([#118](https://github.com/anatolykoptev/go-code/issues/118)) ([aae4786](https://github.com/anatolykoptev/go-code/commit/aae47862a5046dd10746d6690962d87dfd121bd5))
* **llm:** wire LLM_MODEL_FALLBACK chain (Phase 2) ([695401e](https://github.com/anatolykoptev/go-code/commit/695401ecd711524fd2760c9726bcce63430fe9c3))
* **llm:** wire LLM_MODEL_FALLBACK chain (Phase 2) ([0843161](https://github.com/anatolykoptev/go-code/commit/08431613a17c5fc09a37fceaddd5ee22e75a5bbf))
* **llm:** wire per-model cooldown + bump go-kit v0.83.0 ([#233](https://github.com/anatolykoptev/go-code/issues/233)) ([923a25f](https://github.com/anatolykoptev/go-code/commit/923a25f3b76bb668894692fb5b7d4267d51d6d15))
* LRU+TTL cache for CollectChurn (git log --numstat) ([b869013](https://github.com/anatolykoptev/go-code/commit/b869013107811eb2ca53e486e8af87dfda0451a2))
* LRU+TTL cache for CollectCoupling (git log co-change analysis) ([767bfbf](https://github.com/anatolykoptev/go-code/commit/767bfbf156a790870720f5aa1ce0f09e535478c3))
* markdown format for expanded code_search results ([f73391f](https://github.com/anatolykoptev/go-code/commit/f73391faf56c7048939e93e1c33b689e5f1f10a8))
* **metrics:** code_health/code_graph build-failure counters + AGE staleness gauge ([e1959f5](https://github.com/anatolykoptev/go-code/commit/e1959f58ea9d7de7fed1f18e83483860a046636b))
* **metrics:** observability counters for slug-normalize, files-changed, forge-resolve ([#30](https://github.com/anatolykoptev/go-code/issues/30)) ([e1b9265](https://github.com/anatolykoptev/go-code/commit/e1b926500ef8f5f4cd090a70b36ca3d334c2a53b))
* **metrics:** wire ModelFilterObserver to Prometheus counters ([#230](https://github.com/anatolykoptev/go-code/issues/230)) ([06623d0](https://github.com/anatolykoptev/go-code/commit/06623d089f9fc3ff4a203d2643859821a5b75d9b))
* **otel:** instrument go-code with go-kit/tracing — Jaeger integration ([#87](https://github.com/anatolykoptev/go-code/issues/87)) ([ed3756b](https://github.com/anatolykoptev/go-code/commit/ed3756bc883a10c27e1c0a16b8f61ca1710d738b))
* ox-codes scoped keyword search in semantic_search ([6797f98](https://github.com/anatolykoptev/go-code/commit/6797f98a7e76adac68c65c4367a4be587e1c9913))
* **oxcodes:** custom taint rules, anti-patterns, rewrite rejections, cache metrics ([#438](https://github.com/anatolykoptev/go-code/issues/438)) ([356e7b3](https://github.com/anatolykoptev/go-code/commit/356e7b321c75159e8082d05a4f82b18a8d58d7b4))
* **parser, routes:** HTML/htmx Wave 2 — attribute extraction + routes/match_html ([#151](https://github.com/anatolykoptev/go-code/issues/151)) ([bd19d8d](https://github.com/anatolykoptev/go-code/commit/bd19d8dfae830d01d10392cfa52482f795b68fc7))
* **parser:** astro alias resolution + vue SFC handler ([#241](https://github.com/anatolykoptev/go-code/issues/241)) ([498a29e](https://github.com/anatolykoptev/go-code/commit/498a29eb05238e37f66513f502aa489e58c8d450))
* **parser:** Astro markup {expr} calls + refs via shared tsxLang reparse ([#269](https://github.com/anatolykoptev/go-code/issues/269)) ([fa08aea](https://github.com/anatolykoptev/go-code/commit/fa08aeac15d6347c04e7ff763670a7e58fb884d0))
* **parser:** HTML/htmx Wave 1 — handler + Go template preproc ([#150](https://github.com/anatolykoptev/go-code/issues/150)) ([0925259](https://github.com/anatolykoptev/go-code/commit/0925259581fbbcee1d47374a30ece6487c5056af))
* **parser:** Kotlin Wave 1 — handler + tag query ([#144](https://github.com/anatolykoptev/go-code/issues/144)) ([b44a4b4](https://github.com/anatolykoptev/go-code/commit/b44a4b447254e0e79447a6e3e3d894fba7a09fc0))
* **parser:** Kotlin Wave 2 — calls + rels + interface + sealed/enum ([#145](https://github.com/anatolykoptev/go-code/issues/145)) ([2e347c9](https://github.com/anatolykoptev/go-code/commit/2e347c92ad360971d9c30ded5530fe0924d0c561))
* **parser:** Svelte component composition — TemplateRefs, USES edges, destructured $props() ([#270](https://github.com/anatolykoptev/go-code/issues/270)) ([3b1b7e2](https://github.com/anatolykoptev/go-code/commit/3b1b7e2626339e7147c171bc1ee298847357cb18))
* **parser:** Svelte template-expressions + control-flow-effective calls/refs ([#271](https://github.com/anatolykoptev/go-code/issues/271)) ([d80878d](https://github.com/anatolykoptev/go-code/commit/d80878d2cec2c926aee0c8cbcbfd6fa31503ccf1))
* **parser:** Swift Wave 1 — handler + tag query ([#147](https://github.com/anatolykoptev/go-code/issues/147)) ([85aa283](https://github.com/anatolykoptev/go-code/commit/85aa283f4ff483b28bcf763d8e1cd62a43317ddd))
* **parser:** Swift Wave 2 — calls + rels + protocol body + nits ([#148](https://github.com/anatolykoptev/go-code/issues/148)) ([a8db35f](https://github.com/anatolykoptev/go-code/commit/a8db35f50c341823ccc10f02c3ccb09a5ce99927))
* pg_trgm symbol name boosting for repo_analyze file prioritization ([3919a3c](https://github.com/anatolykoptev/go-code/commit/3919a3cec65bd325b114db62210d72f0d67b1817))
* pg_trgm symbol search + CE reranking for code_research ([918c2b1](https://github.com/anatolykoptev/go-code/commit/918c2b1c75d4e4e4fa8af6b4a7fea368e621ad57))
* port github_code_search from go-search to go-code ([#377](https://github.com/anatolykoptev/go-code/issues/377)) ([59fe4e5](https://github.com/anatolykoptev/go-code/commit/59fe4e565388016b421631478e9167afb8b7c2aa))
* **promclient:** bootstrap Prometheus HTTP client + QueryRange ([#46](https://github.com/anatolykoptev/go-code/issues/46)) ([4922ccc](https://github.com/anatolykoptev/go-code/commit/4922ccc40f595830eb2864738cc3b231fc2d62a1))
* **repo_analyze:** surface ox-codes dataflow signals at deep mode ([#23](https://github.com/anatolykoptev/go-code/issues/23)) ([d41d472](https://github.com/anatolykoptev/go-code/commit/d41d472ebd6f3621d48f34861687eb8f9d75b3bb))
* **rerank:** env-tunable rerank timeouts (GOCODE_RERANK_TIMEOUT_S, GOCODE_SEMANTIC_RERANK_TIMEOUT_S) ([#110](https://github.com/anatolykoptev/go-code/issues/110)) ([f2dba84](https://github.com/anatolykoptev/go-code/commit/f2dba8431fc12e1ef25f279c0c6d48038a2d23fe))
* **resolve:** per-IP rate limit for POST /resolve ([#326](https://github.com/anatolykoptev/go-code/issues/326)) ([58e1828](https://github.com/anatolykoptev/go-code/commit/58e1828bc89935bfaa6ad5e8f6cbb2bd970ec2c1))
* **routes:** consolidate lineAt helper and add Line capture to 5 matchers (FU-CG.7) ([#331](https://github.com/anatolykoptev/go-code/issues/331)) ([342998d](https://github.com/anatolykoptev/go-code/commit/342998d2139bf6f3e5560322bfe9ffdadb6f578f))
* **scip:** extract IMPLEMENTS edges from Rust SCIP index — trait impl discovery ([#445](https://github.com/anatolykoptev/go-code/issues/445)) ([57aa79a](https://github.com/anatolykoptev/go-code/commit/57aa79a1ea7c80a20fd5b5f386dc6154f1ab0276))
* **scip:** filter stdlib method calls from SCIP edges to reduce call_trace noise ([#456](https://github.com/anatolykoptev/go-code/issues/456)) ([f55c191](https://github.com/anatolykoptev/go-code/commit/f55c1912834363fe8e29909744dc6b255af915bd))
* **scip:** install scip-java for multi-language type-aware analysis ([#37](https://github.com/anatolykoptev/go-code/issues/37)) ([3b0de6d](https://github.com/anatolykoptev/go-code/commit/3b0de6dd880d8792f10d05aaf7a16a0db6b5d53b))
* **scip:** run SCIP indexers for ALL detected languages, not just dominant ([#459](https://github.com/anatolykoptev/go-code/issues/459)) ([4fba5d7](https://github.com/anatolykoptev/go-code/commit/4fba5d73a182342b46099f888ac6ba23f14c591c))
* **semantic_search:** add code_graph hint to indexing status ([#359](https://github.com/anatolykoptev/go-code/issues/359)) ([2907928](https://github.com/anatolykoptev/go-code/commit/2907928fe3e924bc34a6d0084c17d9190968ae03))
* show human-readable structural importance in understand ([517e216](https://github.com/anatolykoptev/go-code/commit/517e2164e1eca61acab20188b28227b41d1a39a0))
* show PageRank in semantic_search results for architectural awareness ([e66e896](https://github.com/anatolykoptev/go-code/commit/e66e896219e595f6b952b663512037ab41f8d0ec))
* sort impact_analysis callers by PageRank within each tier ([8f25419](https://github.com/anatolykoptev/go-code/commit/8f254190ad4a9adc84d90c8c4881c54764bcd7d0))
* **sourcemap:** make sourcemap max body size configurable ([#324](https://github.com/anatolykoptev/go-code/issues/324)) ([6cdd1c0](https://github.com/anatolykoptev/go-code/commit/6cdd1c09d9a3a5e756587290037214e56fe4cca8))
* structural call site count in prepare_change ([1cf3255](https://github.com/anatolykoptev/go-code/commit/1cf3255b7fc7df7d2a5005ed2ae7137505453ae9))
* **suggestions:** replace embedding fallback with pg_trgm trigram search ([c22d98d](https://github.com/anatolykoptev/go-code/commit/c22d98d81144cbb859da230626efa1dac85a36ae))
* **suggestions:** replace embedding fallback with pg_trgm trigram search ([6bd5ca6](https://github.com/anatolykoptev/go-code/commit/6bd5ca6543bb6d668fe9d0dc3f0440d476c593d7))
* **swift:** Wave 3 — cross-cutting integration (tested_by, speculative, astdiff, importcat, apisurf, delta) ([#149](https://github.com/anatolykoptev/go-code/issues/149)) ([89c2dda](https://github.com/anatolykoptev/go-code/commit/89c2ddaf20ac875717ed3c92d65cdae52b561452))
* **symbol_search:** add ast-grep structural pattern mode ([#22](https://github.com/anatolykoptev/go-code/issues/22)) ([37f8c44](https://github.com/anatolykoptev/go-code/commit/37f8c44e5d760975f62f3f1848bb90ad3a8ac5ea))
* **tracing:** wire httpmw.RegisterRoute for OTEL code.* attrs ([c145b89](https://github.com/anatolykoptev/go-code/commit/c145b89935b7502aa7e87a0084fed08386299a2a))
* **tracing:** wire httpmw.RegisterRoute for OTEL code.* attrs on webhook route ([63e9e94](https://github.com/anatolykoptev/go-code/commit/63e9e94ea665138eab53bbb2d6f7b9290818393a))


### Fixed

* add .svelte-kit to scip skipDirs ([454a814](https://github.com/anatolykoptev/go-code/commit/454a8148e78bc3830926872b656c173d7b419b52))
* add FileGlob to ScopedSearchInput (server added it 2026-03-22) ([c8c858e](https://github.com/anatolykoptev/go-code/commit/c8c858e486bff616dc76abaab411caa6799f6691))
* add GOCACHE, GOPATH, GOWORK=off for go/packages in container ([b2f96a0](https://github.com/anatolykoptev/go-code/commit/b2f96a0ea2bbbb5808df8fa40710b6a7dc369d32))
* add Rust SCIP support and fix copyForIndexing for build dirs ([3a692ab](https://github.com/anatolykoptev/go-code/commit/3a692abd8814c6899ec34e480e8d608955cc2151))
* **age:** use $libdir/plugins/age path so non-superuser roles can LOAD ([#109](https://github.com/anatolykoptev/go-code/issues/109)) ([736f7cf](https://github.com/anatolykoptev/go-code/commit/736f7cf95acc58d4438701eee07114da45554c1e))
* annotateWithPageRank + sortCallersByPageRank now receive root path. ([906f4c2](https://github.com/anatolykoptev/go-code/commit/906f4c2d889c6efa6a4083f58bab5f7397632444))
* annotateWithPageRank uses batch TopPageRank instead of N Symbol() queries ([82b36c7](https://github.com/anatolykoptev/go-code/commit/82b36c736e4af7a2852999d0267044f52384f913))
* **astro:** narrow alias-counter emit-gate to broken declared aliases ([#243](https://github.com/anatolykoptev/go-code/issues/243)) ([76b9c77](https://github.com/anatolykoptev/go-code/commit/76b9c774412b6ff9fe4754e7f4c1216ad236a0ed))
* **autoindex:** emit skipped_no_vendor outcome + assert no-WARN contract ([#180](https://github.com/anatolykoptev/go-code/issues/180)) ([8dab7a6](https://github.com/anatolykoptev/go-code/commit/8dab7a6d2be4f71c2481738f97c98ae220cbcec7))
* **autoindex:** skip eager-warm for repos without vendor/ (etsy-forge, dozor) ([#104](https://github.com/anatolykoptev/go-code/issues/104)) ([372b42c](https://github.com/anatolykoptev/go-code/commit/372b42cbfa45438105680696effeb7a139ae40d9))
* B1 relative path candidates + B2 cycle node skip + Source=Span seed test (closes [#81](https://github.com/anatolykoptev/go-code/issues/81)) ([#82](https://github.com/anatolykoptev/go-code/issues/82)) ([25fa689](https://github.com/anatolykoptev/go-code/commit/25fa689e844628b354ccd10efeffd01a40becc36))
* **b1:** service-aware path candidate /host/src/&lt;service&gt;/&lt;rel&gt; ([#83](https://github.com/anatolykoptev/go-code/issues/83)) ([01127a8](https://github.com/anatolykoptev/go-code/commit/01127a8fe5ad83aff84bb4b989d94af08ce7dfaa))
* **call_trace:** normalize direction values to callers/callees ([#320](https://github.com/anatolykoptev/go-code/issues/320)) ([6ba6db4](https://github.com/anatolykoptev/go-code/commit/6ba6db4a5f1d19cfffe4a41e6fe5547f79678ad8))
* **call_trace:** rewrite TraceFromAGE with iterative BFS (AGE lacks list comprehension) ([#436](https://github.com/anatolykoptev/go-code/issues/436)) ([ee6ae6a](https://github.com/anatolykoptev/go-code/commit/ee6ae6a91d01bb08af5eafa1b32f23946f4875d8))
* **callgraph:** apply stdlib filter to tree-sitter path, not just SCIP ([#466](https://github.com/anatolykoptev/go-code/issues/466)) ([#470](https://github.com/anatolykoptev/go-code/issues/470)) ([38d7a63](https://github.com/anatolykoptev/go-code/commit/38d7a632cf68740b2c4a9f5fc8c74f085801a05b))
* **callgraph:** filter callees to call_expression only, exclude member access and vars ([#28](https://github.com/anatolykoptev/go-code/issues/28)) ([fe20945](https://github.com/anatolykoptev/go-code/commit/fe20945efcc4cfa707ba5a45bcefbceff4da48c0))
* **callgraph:** resolve generic-function callers in package-level var initializers ([#280](https://github.com/anatolykoptev/go-code/issues/280)) ([effbb9a](https://github.com/anatolykoptev/go-code/commit/effbb9a955cbf09b92695dde3255d1cb17a579e1))
* **callgraph:** unblock cold-cache prewarm with CGO_ENABLED=0 + log packages.Load failure ([#29](https://github.com/anatolykoptev/go-code/issues/29)) ([7707f1c](https://github.com/anatolykoptev/go-code/commit/7707f1c4c106733e4cefac8ba6d6b397aa451e81))
* **callgraph:** wire typed call-edge resolution into the AGE-graph path for dead-code accuracy (BUG A, gated default-off) ([d0cdfa8](https://github.com/anatolykoptev/go-code/commit/d0cdfa8177aa287c1965e4677f63818c7ae9fb3a))
* cap direct callers at 100 in impact_analysis post-processing ([3067240](https://github.com/anatolykoptev/go-code/commit/30672402860ecc1121d3a8b7c920fc6d2f66503e))
* **clients:** stop allocating httputil.Client on every call ([#316](https://github.com/anatolykoptev/go-code/issues/316)) ([2b132c1](https://github.com/anatolykoptev/go-code/commit/2b132c18fb29e5d83c5a50a151012cc6787c0544))
* **code_graph:** return building status instead of tool error ([#361](https://github.com/anatolykoptev/go-code/issues/361)) ([90b1b55](https://github.com/anatolykoptev/go-code/commit/90b1b554403ab64914a76c6122da4ed1b411645f))
* **code_health:** stop deleting a remote clone while the background snapshot is still reading it ([#246](https://github.com/anatolykoptev/go-code/issues/246)) ([67a3f8f](https://github.com/anatolykoptev/go-code/commit/67a3f8f266c79eedbc0e5662c3cd7e8b0c96c5dc))
* **codegraph:** add side to side-blind Route MATCH queries (FU-CG.8) ([#333](https://github.com/anatolykoptev/go-code/issues/333)) ([b01ce35](https://github.com/anatolykoptev/go-code/commit/b01ce35f18eb16e12440dcd2e41c215fef1af8d2))
* **codegraph:** apply ageSetup search_path in bookkeeping-table accessors ([4701d46](https://github.com/anatolykoptev/go-code/commit/4701d4670c9d55fa257ab61ad0b82c80308cf79d))
* **codegraph:** apply ageSetup search_path in bookkeeping-table accessors ([30a1849](https://github.com/anatolykoptev/go-code/commit/30a18490a77f6f20505e86c57cac8ec7d49034e0))
* **codegraph:** emit IMPLEMENTS edge label for IsInterface call edges ([#447](https://github.com/anatolykoptev/go-code/issues/447)) ([d7c0e57](https://github.com/anatolykoptev/go-code/commit/d7c0e571d75a0a7d81382e28daf760f852671b22))
* **codegraph:** enable typed call enrichment by default ([#314](https://github.com/anatolykoptev/go-code/issues/314)) ([b25a28c](https://github.com/anatolykoptev/go-code/commit/b25a28cb68a9325a41de5cb822907f3ed20396b6))
* **codegraph:** FU-CG.9 — make route edge counters truthful (built vs unmatched) ([#335](https://github.com/anatolykoptev/go-code/issues/335)) ([f16ca86](https://github.com/anatolykoptev/go-code/commit/f16ca869f1d79b3603e5674f62f8dca8d54bcc10))
* **codegraph:** memory guard + chunked COPY to prevent OOM kernel panic ([#428](https://github.com/anatolykoptev/go-code/issues/428)) ([#429](https://github.com/anatolykoptev/go-code/issues/429)) ([1e17f84](https://github.com/anatolykoptev/go-code/commit/1e17f846da12c0ebae17c7c2e85ac40409fcb0db))
* **codegraph:** preflight guard for graph-missing on read-path ([#42](https://github.com/anatolykoptev/go-code/issues/42)) ([7abed8e](https://github.com/anatolykoptev/go-code/commit/7abed8e661c32726a7d84577305eefe6fe81bf0e))
* **codegraph:** prune stale dead-code scores when a function stops being an orphan ([#295](https://github.com/anatolykoptev/go-code/issues/295)) ([dd7dc2b](https://github.com/anatolykoptev/go-code/commit/dd7dc2bef984d18f3bdc8b5f7db4dc13592dec48))
* **codegraph:** remove HasGoModule gate from buildAGECallGraph — enable SCIP for Rust ([#449](https://github.com/anatolykoptev/go-code/issues/449)) ([3186c9a](https://github.com/anatolykoptev/go-code/commit/3186c9a4e4cf0bef27099da9942ab8525df31e27))
* **codegraph:** repair fleet-wide HANDLES/FETCHES=0 — route→graph edge builder ([#167](https://github.com/anatolykoptev/go-code/issues/167)) ([72f8495](https://github.com/anatolykoptev/go-code/commit/72f8495ddd4f081b852237e2228801a6981da7f6))
* **codegraph:** write-path guards + replace fragile template count test ([#44](https://github.com/anatolykoptev/go-code/issues/44)) ([a1c931f](https://github.com/anatolykoptev/go-code/commit/a1c931f29f14d473e0935223393b42a12ff2e77f))
* **compare,codegraph:** code_compare grade reflects freshness + language-aware isExported; [#253](https://github.com/anatolykoptev/go-code/issues/253) cleanup ([fa70a9f](https://github.com/anatolykoptev/go-code/commit/fa70a9f1f10b196239d15b088c0e45065468444c))
* **compare:** avoid duplicate BuildSnapshot when comparing a repo to itself ([5103601](https://github.com/anatolykoptev/go-code/commit/51036017cd2ec1c8629d8b3c530eda936f18ff78))
* **compare:** avoid re-parsing files for type relationships ([adb8317](https://github.com/anatolykoptev/go-code/commit/adb8317678bcdf9114ccd9bc4a758ebd311c3f2b))
* **compare:** cap code_compare deadlines to fit 100s proxy timeout ([53327d1](https://github.com/anatolykoptev/go-code/commit/53327d1367b53afdcd4e9229d61be107b1d7f4e4))
* **compare:** dedupe self-compare snapshots + ParseCache integration ([ec665a0](https://github.com/anatolykoptev/go-code/commit/ec665a08920f549614ce52083c0e9d448e7a814a))
* **compare:** deterministic cycle-pair order in find2Cycles (flaky test) ([#272](https://github.com/anatolykoptev/go-code/issues/272)) ([5f45850](https://github.com/anatolykoptev/go-code/commit/5f45850ad4e424348bab62d91b442c0877689472))
* **compare:** raise code_compare deadline from 90s to 3m ([#309](https://github.com/anatolykoptev/go-code/issues/309)) ([ae5aefd](https://github.com/anatolykoptev/go-code/commit/ae5aefd990fd447958db1dc152c1275070200dd1))
* **compare:** reuse tree-sitter parser per worker in BuildSnapshot ([#384](https://github.com/anatolykoptev/go-code/issues/384)) ([ad61442](https://github.com/anatolykoptev/go-code/commit/ad614422c8fc7ec1c72d2e7061799a8ddeace5eb))
* **compare:** treat zero-dependency repos as N/A for freshness+vuln scoring ([9f2158e](https://github.com/anatolykoptev/go-code/commit/9f2158ec80da41c876b026e7b56da4d62ab054aa))
* **compare:** treat zero-dependency repos as N/A in code_compare grade (match code_health/[#250](https://github.com/anatolykoptev/go-code/issues/250)) ([e1ade2f](https://github.com/anatolykoptev/go-code/commit/e1ade2f60e29a214b227a94f172902dad8513750))
* **complexity:** unify cyclomatic complexity on parser as single owner ([dcf2118](https://github.com/anatolykoptev/go-code/commit/dcf2118403252b8a1849b8174273e1f3dfe81daf))
* correct sigmoid formula — Exp(-rawScore) not Exp(rawScore) ([0a485d7](https://github.com/anatolykoptev/go-code/commit/0a485d7c2096e61593d8b89e2c016eeccf967ac2))
* **db:** reset pooled-conn search_path on release — bare code_* resolves public, not ag_catalog ([#173](https://github.com/anatolykoptev/go-code/issues/173)) ([5b97004](https://github.com/anatolykoptev/go-code/commit/5b970047579547c3b9af2042a500d190f6b90f34))
* **deadcode:** language-aware exported check for non-IsPublic languages ([#281](https://github.com/anatolykoptev/go-code/issues/281)) ([097ce3a](https://github.com/anatolykoptev/go-code/commit/097ce3af2c11bc0331d67b0f13074cfea18fa6ba))
* **debug_investigate:** dedup historical incidents by (Repo, Symbol) ([#85](https://github.com/anatolykoptev/go-code/issues/85)) ([9403722](https://github.com/anatolykoptev/go-code/commit/940372278c71109cff13adf373f927dfad7bd62c)), closes [#84](https://github.com/anatolykoptev/go-code/issues/84)
* **debug_investigate:** drop t.Skip and document %q/%s label choice ([#318](https://github.com/anatolykoptev/go-code/issues/318)) ([6675eed](https://github.com/anatolykoptev/go-code/commit/6675eed977167aa948138cbb5c3b48ab605ba27c))
* **debug_investigate:** faster polling + LLM timeout bump + service-&gt;repo body mapping ([#99](https://github.com/anatolykoptev/go-code/issues/99)) ([03c6b34](https://github.com/anatolykoptev/go-code/commit/03c6b34e221484d745970f69dfa083cdddab82f8))
* **debug_investigate:** include code.function in subject + map /build/ paths to host ([e3aca61](https://github.com/anatolykoptev/go-code/commit/e3aca61a981d134da1d6af22c8d4972f7156c4eb))
* **debug_investigate:** include code.function in subject + map /build/ paths to host ([e3aca61](https://github.com/anatolykoptev/go-code/commit/e3aca61a981d134da1d6af22c8d4972f7156c4eb))
* **debug_investigate:** include code.function in subject + map /build/ paths to host ([16a8cc7](https://github.com/anatolykoptev/go-code/commit/16a8cc710688af36e5ba8635c2da8270679b22aa))
* **debug_investigate:** include repo in cache key + honor explicit repo arg ([#90](https://github.com/anatolykoptev/go-code/issues/90)) ([c2ed3cc](https://github.com/anatolykoptev/go-code/commit/c2ed3cc276f61f3315630474c8d6f09cac042f3a))
* **debug_investigate:** MetricsQueried in legacy path is += not = (closes [#75](https://github.com/anatolykoptev/go-code/issues/75)) ([#76](https://github.com/anatolykoptev/go-code/issues/76)) ([6b86be3](https://github.com/anatolykoptev/go-code/commit/6b86be3c81e8d2d4260da97cc92a914dd981fbce))
* **debug_investigate:** Phase 2 — baseline trace fetch (was error-only, starved symbol correlation) ([#73](https://github.com/anatolykoptev/go-code/issues/73)) ([9850f9e](https://github.com/anatolykoptev/go-code/commit/9850f9ead4134409e612f89bedf608c00f7c1b41))
* dynamic pg_trgm distance proportional to similarity score ([e5b6dc1](https://github.com/anatolykoptev/go-code/commit/e5b6dc14148b0876e8cf8381fa1b013750f6ab4b))
* **embeddings:** delete only true orphans (positive IN-list), not per-chunk anti-join ([ff591c6](https://github.com/anatolykoptev/go-code/commit/ff591c6436ef29d44ec0efbccbf6bc41c0d8422c))
* **embeddings:** incremental sync froze indexed_sha on first unsupported file in diff ([#170](https://github.com/anatolykoptev/go-code/issues/170)) ([5a50d6d](https://github.com/anatolykoptev/go-code/commit/5a50d6db99461f92867dc7a9fba9bc2c6380bf44))
* **embeddings:** NUL-separate in-memory symbol keys (colon-in-path safe) + document dedup lossiness ([e1e874a](https://github.com/anatolykoptev/go-code/commit/e1e874ad9463ab93bdab8e6f9e6754447bd49488))
* **embeddings:** rate-gate autoindex concurrency to 1 for single-worker embed backend ([#217](https://github.com/anatolykoptev/go-code/issues/217)) ([766f328](https://github.com/anatolykoptev/go-code/commit/766f3281afc324479c41925739affc9818156f94))
* **embeddings:** replace misleading freshness gauge with commits-behind + count SetRepoState write-failures ([#172](https://github.com/anatolykoptev/go-code/issues/172)) ([7fdb35a](https://github.com/anatolykoptev/go-code/commit/7fdb35ad3858a89ddd3d32424ce233fe20066fc0))
* **embeddings:** treat all 5xx as retryable; add embed_model per-row; continuous orphan gauge ([#232](https://github.com/anatolykoptev/go-code/issues/232)) ([fae4679](https://github.com/anatolykoptev/go-code/commit/fae46790d6efc80f9f4240c3828a81a7d1b43900))
* **explore:** files_changed reflects single commit diff, not cumulative range ([#26](https://github.com/anatolykoptev/go-code/issues/26)) ([cd62036](https://github.com/anatolykoptev/go-code/commit/cd6203667aa945a60cb2af3504e2fb72c08603b2))
* **explore:** label health score as approximate with hint ([#249](https://github.com/anatolykoptev/go-code/issues/249)) ([d0f8a79](https://github.com/anatolykoptev/go-code/commit/d0f8a796726bb18aa9f8382af37da8e1eb5e80a3))
* **federate:** FU-1.1 — thread request ctx into ResolveRepos for cancellable origin dedup ([#337](https://github.com/anatolykoptev/go-code/issues/337)) ([d45bb8e](https://github.com/anatolykoptev/go-code/commit/d45bb8e99eb59d322d994f1a986ee213ffd06feb))
* **federate:** pass asOf time.Time to CrossRepoCoChange to avoid wall-clock git log --since ([e1c7938](https://github.com/anatolykoptev/go-code/commit/e1c79389188bc39e4636cbd06f8224f956e29388))
* **fleet/ssh:** pass -F flag explicitly and rewrite ~ paths in shadow config ([#131](https://github.com/anatolykoptev/go-code/issues/131)) ([ba5031d](https://github.com/anatolykoptev/go-code/commit/ba5031d05ca114b4ce02305b08d88ace2c6ca094))
* **forge:** deflake metrics_test.go counter delta assertions ([#308](https://github.com/anatolykoptev/go-code/issues/308)) ([c411916](https://github.com/anatolykoptev/go-code/commit/c411916c7fe42811b1f868aecd057088c9adbb42))
* **forge:** ExtractSlug + DetectForge accept URL/SSH forms ([#27](https://github.com/anatolykoptev/go-code/issues/27)) ([aac796f](https://github.com/anatolykoptev/go-code/commit/aac796f8ec43204f83b81fff0ecbedb9b51b3f1c))
* **gitutil:** accept .git file form in worktree detection ([#36](https://github.com/anatolykoptev/go-code/issues/36)) ([0a717a6](https://github.com/anatolykoptev/go-code/commit/0a717a65aa2d4198ba3d351d23019c4b47ad4db1))
* go build pre-warm + longer timeouts for go/types GOCACHE ([cc45408](https://github.com/anatolykoptev/go-code/commit/cc45408eef1c1d46943814f3cf4905799ac57b40))
* **go-code:** accept owner/repo form in github_code_search tool ([#381](https://github.com/anatolykoptev/go-code/issues/381)) ([f2ecfb2](https://github.com/anatolykoptev/go-code/commit/f2ecfb20a76da5cbb1d5ed1e430b8f904dd1e9c9))
* **go-code:** batch build-time dead_code rerank to the server's per-request cap ([#191](https://github.com/anatolykoptev/go-code/issues/191)) ([10193f4](https://github.com/anatolykoptev/go-code/commit/10193f40858e5b6c2d1ac27b8ffda4ddb79812f1))
* **go-code:** embed HTTP timeout + bounded async index ctx + attributable cancel ([#216](https://github.com/anatolykoptev/go-code/issues/216)) ([182b9c2](https://github.com/anatolykoptev/go-code/commit/182b9c2e34d7a06d8be9c94a8f1e7c2d96176ea5))
* **go-code:** exclude *_test.go imports from circular-dep detection ([#184](https://github.com/anatolykoptev/go-code/issues/184)) ([2240c69](https://github.com/anatolykoptev/go-code/commit/2240c696ded57d89edaa8345cd852ca0e5afe453))
* **go-code:** group archgraph queries by package path, not base name ([#186](https://github.com/anatolykoptev/go-code/issues/186)) ([d374b36](https://github.com/anatolykoptev/go-code/commit/d374b36ba0dd677d103f15092ba6b9c79f2c0f3f))
* **go-code:** Phase 2a cleanup — 17 items (BUG-FH-1/2 closed, error encoding unified, +13 cosmetic) ([#157](https://github.com/anatolykoptev/go-code/issues/157)) ([ac4c658](https://github.com/anatolykoptev/go-code/commit/ac4c6585be7d8a2eb571fa138946577189bfee11))
* **go-code:** Phase 2b infra — Commits count, churn growth, since window, --follow, WithFreshness wiring ([#158](https://github.com/anatolykoptev/go-code/issues/158)) ([f820043](https://github.com/anatolykoptev/go-code/commit/f8200435904638f29e629cf69efefaecc565bf3c))
* **go-code:** pool AfterRelease RESET ALL, not DISCARD ALL (26000 regression) ([#176](https://github.com/anatolykoptev/go-code/issues/176)) ([b69699f](https://github.com/anatolykoptev/go-code/commit/b69699fb1d2bcf2ff21370e4448cc2bfb5596255))
* **go-code:** reconcile orphan embedding rows on full index + operator sweep (Bug B — phantom symbols) ([#209](https://github.com/anatolykoptev/go-code/issues/209)) ([2f46ea7](https://github.com/anatolykoptev/go-code/commit/2f46ea7cbcc041f479253b81a92a9ee8d6af44af))
* **go-code:** rerank via go-kit/rerank.Client, drop hardcoded embed-server URL ([#190](https://github.com/anatolykoptev/go-code/issues/190)) ([fa3e041](https://github.com/anatolykoptev/go-code/commit/fa3e041e4628140218b67f161b619e4633aaef4e))
* **go-code:** self-index desync (SHA-gate data-aware) + HTTP-index-cancel observability ([#214](https://github.com/anatolykoptev/go-code/issues/214)) ([619ca62](https://github.com/anatolykoptev/go-code/commit/619ca6200a42de2e1225f7349e9c2f76c2943272))
* **go-code:** sparsevec batch size 500→100 (data-bound statement_timeout) + accurate write_failed counter ([#201](https://github.com/anatolykoptev/go-code/issues/201)) ([029f8d3](https://github.com/anatolykoptev/go-code/commit/029f8d312d497d18a9d3c70a6418d065d17aa18b))
* **go-code:** unify local package nodes (stop duplicate dir/import-path vertices) ([#185](https://github.com/anatolykoptev/go-code/issues/185)) ([0cd7136](https://github.com/anatolykoptev/go-code/commit/0cd71362cde3ba850fade01f82bb6458a49c5f54))
* **graph-arm:** invert pagerank sub-generator — keyword-relevant ranked by pagerank ([#219](https://github.com/anatolykoptev/go-code/issues/219)) ([9b55813](https://github.com/anatolykoptev/go-code/commit/9b55813e6ff6fcc6fb02caa7f3a6c0a6dc0a0388))
* **importresolve:** honor package.json exports map for workspace subpath imports ([#422](https://github.com/anatolykoptev/go-code/issues/422)) ([#424](https://github.com/anatolykoptev/go-code/issues/424)) ([eca5227](https://github.com/anatolykoptev/go-code/commit/eca5227d858f10ce90fbeb9a2314606ad31ccf06))
* **ingest,explore:** shallow clone depth=2 + shallow-boundary guard in countDiffTreeFiles ([#31](https://github.com/anatolykoptev/go-code/issues/31)) ([8a00f4b](https://github.com/anatolykoptev/go-code/commit/8a00f4b9fbda3221c33ada9432f52b5f63de3a19))
* **ingest:** accept comma-separated focus keywords ([#305](https://github.com/anatolykoptev/go-code/issues/305)) ([f72bff8](https://github.com/anatolykoptev/go-code/commit/f72bff8e63a50ebedb6a102833807fbb792644a4))
* **ingest:** atomic clone via renameat2 RENAME_EXCHANGE; errno breakdown for read_error ([#116](https://github.com/anatolykoptev/go-code/issues/116)) ([3c005f9](https://github.com/anatolykoptev/go-code/commit/3c005f9a094e19acc44d142eb8f8afd91936188f))
* **ingest:** defensive copy in IngestRepo cache to prevent aliasing ([#477](https://github.com/anatolykoptev/go-code/issues/477)) ([3976976](https://github.com/anatolykoptev/go-code/commit/39769765a7f9cdd3866bfdf25c88240786e98178))
* **ingest:** NormalizeSlug accepts URL and SSH forms ([#24](https://github.com/anatolykoptev/go-code/issues/24)) ([a466a9b](https://github.com/anatolykoptev/go-code/commit/a466a9be50499e8b83b18de577a06fd02b95f241))
* **ingest:** refresh credentials via GIT_CONFIG before git fetch ([#107](https://github.com/anatolykoptev/go-code/issues/107)) ([70c29a2](https://github.com/anatolykoptev/go-code/commit/70c29a295911aa0c09dfe3a25cd3dadfed9a0bbe))
* **ingest:** refresh on cache-hit to remote HEAD instead of trusting on-disk state ([#21](https://github.com/anatolykoptev/go-code/issues/21)) ([bbc9346](https://github.com/anatolykoptev/go-code/commit/bbc9346e45e59c635ea0d470807719d0e7bf8713))
* **ingest:** use App installation token for clone when configured ([#105](https://github.com/anatolykoptev/go-code/issues/105)) ([3987a82](https://github.com/anatolykoptev/go-code/commit/3987a829d917d334bcf00fa1b2bd1efa44f253a6))
* **llm-obs:** register metrics against go-code's registry, not default ([#121](https://github.com/anatolykoptev/go-code/issues/121)) ([d82d1af](https://github.com/anatolykoptev/go-code/commit/d82d1af6d84604991706b5f49d0b9105d881cf80))
* **llm:** default per-attempt timeout for chain rotation + review_delta 120s ([#391](https://github.com/anatolykoptev/go-code/issues/391)) ([#395](https://github.com/anatolykoptev/go-code/issues/395)) ([e747f70](https://github.com/anatolykoptev/go-code/commit/e747f706b4a95780b187ea7ea1c2fb2c9fc473eb))
* **mcpmeta:** correct misleading stale-index remediation advice ([#169](https://github.com/anatolykoptev/go-code/issues/169)) ([23b60bd](https://github.com/anatolykoptev/go-code/commit/23b60bdb7ffb6be6d17cd48718549f3e64e9afdb))
* **mcp:** raise code_graph timeout + non-blocking narrative + branch cleanup ([#433](https://github.com/anatolykoptev/go-code/issues/433)) ([6bed115](https://github.com/anatolykoptev/go-code/commit/6bed1154278affee8211969de158283059ebd8d5))
* **mcp:** return tool results as application/json, not single-line SSE ([#245](https://github.com/anatolykoptev/go-code/issues/245)) ([52a6a97](https://github.com/anatolykoptev/go-code/commit/52a6a97444461ea13d82b83f4bdb78f9807c6469))
* **mcp:** reverse-map container paths in outputs + zero-result hint ([#45](https://github.com/anatolykoptev/go-code/issues/45)) ([3b5261a](https://github.com/anatolykoptev/go-code/commit/3b5261abf1f9fe5cf41d744e5ebc0583fb65dcff))
* **metrics:** add per-symbol cognitive complexity and fix JS docRatio ([#247](https://github.com/anatolykoptev/go-code/issues/247)) ([fb3ce79](https://github.com/anatolykoptev/go-code/commit/fb3ce7900add669cb629fd674ac724f26b5fc93b))
* **metrics:** pre-register alert-facing series at boot (graph age, zero-embeddings) ([#287](https://github.com/anatolykoptev/go-code/issues/287)) ([1253080](https://github.com/anatolykoptev/go-code/commit/1253080df01af09d9774629e21311f4e153c6a2a))
* **metrics:** record outcome=error on resolve failure + drop unemitted skipped label ([de1790a](https://github.com/anatolykoptev/go-code/commit/de1790a5d0e12b6adc46ea0ee003b932318b044c))
* **metrics:** scope code-graph age gauge to AUTO_INDEX_DIRS repos ([#291](https://github.com/anatolykoptev/go-code/issues/291)) ([0fa1677](https://github.com/anatolykoptev/go-code/commit/0fa16774b327bc19d9c38302ac1c85ca008250e8))
* **metrics:** unify health score and add arch fallback for unindexed repos ([#248](https://github.com/anatolykoptev/go-code/issues/248)) ([d0f16b6](https://github.com/anatolykoptev/go-code/commit/d0f16b69b86cfe1d2c5a970eccb6a86ce201c9fc))
* normalize CE dead_code_score to probability [0..1] via sigmoid ([ffe4b72](https://github.com/anatolykoptev/go-code/commit/ffe4b72740e271c429a8118dc13becb90d102b69))
* only pass format=markdown to ox-codes when expand is requested ([c2ca868](https://github.com/anatolykoptev/go-code/commit/c2ca868619c97272909a4fbbca50774c41d27d5d))
* **oxcodes:** bump structural-search HTTP timeout 10s-&gt;30s ([#168](https://github.com/anatolykoptev/go-code/issues/168)) ([3f07932](https://github.com/anatolykoptev/go-code/commit/3f079324da1c1edadf1d202c459a31d8743a6791))
* ParseCache drops call sites and ignores includeBody on hit ([#286](https://github.com/anatolykoptev/go-code/issues/286)) ([b56e73e](https://github.com/anatolykoptev/go-code/commit/b56e73e9a5ea105e205971e905b223481b451067))
* **parser:** dual-emit rune symbols so $state query finds all bound declarations ([#108](https://github.com/anatolykoptev/go-code/issues/108)) ([f8aaf48](https://github.com/anatolykoptev/go-code/commit/f8aaf48593482efae2bd22fef92ac3f35915a981))
* **parser:** JS/TS-family Symbol.Language parity — .jsx/.js/.mjs/.cjs emit javascript ([#268](https://github.com/anatolykoptev/go-code/issues/268)) ([93a9d69](https://github.com/anatolykoptev/go-code/commit/93a9d6939828c07a7589df181b6d3b8c7c3ee5f4))
* **parser:** route Vue call extraction through the two-region ScriptCalls/MarkupCalls split ([#409](https://github.com/anatolykoptev/go-code/issues/409)) ([#414](https://github.com/anatolykoptev/go-code/issues/414)) ([96da242](https://github.com/anatolykoptev/go-code/commit/96da242c5f6377d355caf719693ed215d8733cd9))
* pass Logger to mcpserver.Run to preserve slogh wrapper ([85952fa](https://github.com/anatolykoptev/go-code/commit/85952fa877dede27fafb3448af196c7eda3fc02d))
* pass root path (not pre-hashed key) to graph.Symbol() callers ([906f4c2](https://github.com/anatolykoptev/go-code/commit/906f4c2d889c6efa6a4083f58bab5f7397632444))
* pass root path to TopPageRank in repo_analyze (same double-hash bug) ([a0c8a50](https://github.com/anatolykoptev/go-code/commit/a0c8a50ab39301ab57a68f7456de2db3c01f0076))
* pgxpool MaxConns=10 + 5s safety timeout on symbol search ([c31ecff](https://github.com/anatolykoptev/go-code/commit/c31ecffb6fb1a01267b3ee2b9337816679f95723))
* **pipeline-file:** mirror indexRepo filters (isTestFile + maxIndexFileBytes) ([13df4f5](https://github.com/anatolykoptev/go-code/commit/13df4f50ebd1fc0dde4d16e175889104fc29fd7b))
* **pipeline-incremental:** bind ctx to git diff exec + surface stderr ([b9af9c5](https://github.com/anatolykoptev/go-code/commit/b9af9c520667a11df470ad3af6e184be310e5c51))
* **polyglot/pinned:** don't abort Collect walk on permission errors ([#126](https://github.com/anatolykoptev/go-code/issues/126)) ([5c8fc42](https://github.com/anatolykoptev/go-code/commit/5c8fc42baaa46d40888cb09dc4aaf6f0fcd16353))
* **polyglot/pinned:** resolve compose include: directive recursively ([#125](https://github.com/anatolykoptev/go-code/issues/125)) ([ca3cdcb](https://github.com/anatolykoptev/go-code/commit/ca3cdcbfb44b8b01568c9eb9da9abd9688da0533))
* **polyglot/pinned:** skip nested git repos, submodules, and .claude worktrees ([#127](https://github.com/anatolykoptev/go-code/issues/127)) ([a9fc807](https://github.com/anatolykoptev/go-code/commit/a9fc807bc923eda5c01465db827137a3e43077e3))
* put tracemcpmw first so hooks receive span context ([6264234](https://github.com/anatolykoptev/go-code/commit/6264234f8224be06ba2a7e4786c1d9d5815c6f36))
* reduce freshness timeout on large repos (313 deps → fits in 60s) ([c1bf80b](https://github.com/anatolykoptev/go-code/commit/c1bf80b184f1d5a278078e95491c2e90c3087fe5))
* regex patterns in speculative call resolution ([4a23c22](https://github.com/anatolykoptev/go-code/commit/4a23c22b162bfe93941fd16b53c885b05e95799e))
* **release-please:** guard auto-merge step when no release PR ([#311](https://github.com/anatolykoptev/go-code/issues/311)) ([6a01d88](https://github.com/anatolykoptev/go-code/commit/6a01d88d3b7e41280b27d35c0eb5abce8211766e))
* **release:** amd64 CGO CC override + consolidate to one goreleaser config ([#277](https://github.com/anatolykoptev/go-code/issues/277)) ([8096526](https://github.com/anatolykoptev/go-code/commit/80965268ced1585294bc0bd8e222844f3ab73ee5))
* remove Go-only file filter from CollectCoupling ([46cbbfa](https://github.com/anatolykoptev/go-code/commit/46cbbfa47e9595e609b110720a0313e6c262f098))
* replace N sequential Symbol() queries with 1 batch TopPageRank in sortCallersByPageRank ([2ab9b6e](https://github.com/anatolykoptev/go-code/commit/2ab9b6e232b153e2e6f13e60d4e18df83ec77c9a))
* **repo_analyze:** omit empty &lt;signature&gt; tag entirely (not just content) ([#19](https://github.com/anatolykoptev/go-code/issues/19)) ([bc06acf](https://github.com/anatolykoptev/go-code/commit/bc06acff97f005b8db5b7fa8b55c1c05ac5254aa))
* **resolve:** prefer local /host/src checkout over clone for matching slugs ([ccacd18](https://github.com/anatolykoptev/go-code/commit/ccacd1803b0ce7ac67a045885bf0aa1094ceb779))
* **resolve:** prefer local /host/src checkout over clone for matching slugs ([ea4a78f](https://github.com/anatolykoptev/go-code/commit/ea4a78f85bd0c9b937c79c7715a845b0f3aaba70))
* **resolve:** resolve bare repo names against LocalRepoDirs registry ([#226](https://github.com/anatolykoptev/go-code/issues/226)) ([75ab93a](https://github.com/anatolykoptev/go-code/commit/75ab93a5dbd53c5eb7bf17ec756c656871f88bd4))
* **review_pr:** pass FETCH_HEAD to diff, not warm-clone HEAD ([#12](https://github.com/anatolykoptev/go-code/issues/12)) ([79bce46](https://github.com/anatolykoptev/go-code/commit/79bce46e5d403fcc078ff39808b1bd3cdf2c5da1))
* **review_pr:** worktree-isolated checkout for call graph analysis ([#13](https://github.com/anatolykoptev/go-code/issues/13)) ([3dcbb88](https://github.com/anatolykoptev/go-code/commit/3dcbb88d176ad597f5d90e3c22dd8a06438665d7))
* **review:** correct untested-symbol false positives in review_delta ([#392](https://github.com/anatolykoptev/go-code/issues/392)) ([2f1dbe8](https://github.com/anatolykoptev/go-code/commit/2f1dbe81b2fc9460538c8c4e039a1d939337e5fe))
* **review:** route PR-post write path through the multi-forge registry ([#284](https://github.com/anatolykoptev/go-code/issues/284)) ([11d2b84](https://github.com/anatolykoptev/go-code/commit/11d2b8481b1a0ae105447cb53bbbab640509b771))
* **review:** use valid ox-codes scope "function_bodies" in review_delta ([#420](https://github.com/anatolykoptev/go-code/issues/420)) ([336306b](https://github.com/anatolykoptev/go-code/commit/336306b55963019e63e9a9e68a3994801860c007)), closes [#419](https://github.com/anatolykoptev/go-code/issues/419)
* **review:** worktree-aware git invocation via --git-dir + PathRewrite ([#38](https://github.com/anatolykoptev/go-code/issues/38)) ([8c32bc3](https://github.com/anatolykoptev/go-code/commit/8c32bc358a1c9e3a78b4bb208806296c4945c556))
* safe type assertion in resolveMethodSelection ([c4f82e7](https://github.com/anatolykoptev/go-code/commit/c4f82e7b4a477b7f87e4aa1bbd55b2da4e3387d1))
* sane fresh-deploy defaults for LLM model and /resolve rate limit ([#412](https://github.com/anatolykoptev/go-code/issues/412)) ([74cb317](https://github.com/anatolykoptev/go-code/commit/74cb317b0ec630d7ff9bff4592b665c4dbe59ab6))
* **scip:** use content hash instead of mtimes for CacheKey — no false misses on git checkout ([#458](https://github.com/anatolykoptev/go-code/issues/458)) ([acdde4a](https://github.com/anatolykoptev/go-code/commit/acdde4a7c0db1569ce831e38e3b52ffd5d82d15f))
* **scip:** wire Cache into trySCIPResolution — skip re-indexing on cache hit ([#443](https://github.com/anatolykoptev/go-code/issues/443)) ([4a096e9](https://github.com/anatolykoptev/go-code/commit/4a096e9072f694df0a3ddf98107724e7e88ece5f))
* scoped call site search + safe go/types type assertion ([856ad88](https://github.com/anatolykoptev/go-code/commit/856ad881e9891d209a3b3efbf85b7cf41c838294))
* **semantic_search:** strip AGE agtype quotes from complexity values ([8359edb](https://github.com/anatolykoptev/go-code/commit/8359edb442a8482ad52a9b794fbe76794f469ce7))
* **semantic-fallback:** cap embed query at 5s sub-context ([4ccbc3f](https://github.com/anatolykoptev/go-code/commit/4ccbc3f622ed739528d391bf2c89ed711ab79dca))
* **semantic-search:** dedup semantic-only + CE-rerank results by file:symbol (Bug A) ([#208](https://github.com/anatolykoptev/go-code/issues/208)) ([445e2ee](https://github.com/anatolykoptev/go-code/commit/445e2ee4242160f7dbc57e43963b533e06babc6d))
* **semhealth:** eliminate two find_duplicates false-positive classes ([#218](https://github.com/anatolykoptev/go-code/issues/218)) ([d5dc396](https://github.com/anatolykoptev/go-code/commit/d5dc39657e29112ec2bf253b6bcc89f1a21ec9f4))
* **semhealth:** guard self-join by repo size + statement_timeout ([f552367](https://github.com/anatolykoptev/go-code/commit/f552367e0d6d834bb52211914b817e2281e3c0a5))
* serialize EnsureGraph provisioning to fix pg_type 23505 race ([#417](https://github.com/anatolykoptev/go-code/issues/417)) ([74d4e11](https://github.com/anatolykoptev/go-code/commit/74d4e11d736fd58e2fe1742cf2b1b7e9c6d0793a))
* set keyword_name distance=0.4 (fixed) so pg_trgm hits pass threshold filter ([0517bf1](https://github.com/anatolykoptev/go-code/commit/0517bf148a7760c8cc32c9a6a4d91b65377da3aa))
* shrink code_compare LLM prompt to fit 8k-token fleet models ([#398](https://github.com/anatolykoptev/go-code/issues/398)) ([8140ccc](https://github.com/anatolykoptev/go-code/commit/8140ccc0620c4cc1ceabbecd84e6c61c4a12ca67))
* single TopPageRank(top-200) batch query → local map lookup (O(N), zero DB round-trips). ([2ab9b6e](https://github.com/anatolykoptev/go-code/commit/2ab9b6e232b153e2e6f13e60d4e18df83ec77c9a))
* **test:** make TestSignalHitsLiveIntegration self-contained (nightly green) ([#389](https://github.com/anatolykoptev/go-code/issues/389)) ([75ea56a](https://github.com/anatolykoptev/go-code/commit/75ea56abec6936ba145b4d969f2f15f8fc1f485b))
* three go-code anomalies from 2026-06-12 investigation ([#228](https://github.com/anatolykoptev/go-code/issues/228)) ([0141e3d](https://github.com/anatolykoptev/go-code/commit/0141e3d99e62ca6e2d942d7920abfca69fd57a2e))
* **toolserver:** add understand to ToolTimeouts (30s) ([0754a49](https://github.com/anatolykoptev/go-code/commit/0754a49b273ac98cdcc9d1fd2bf92fc23dc5c9bb))
* **tracing:** cast webhook handler to concrete type for correct code.* attrs ([f117560](https://github.com/anatolykoptev/go-code/commit/f1175609df482465949a9b309ce1eca00d7bff95))
* **tracing:** cast webhook handler to concrete type for correct code.* attrs ([1deca55](https://github.com/anatolykoptev/go-code/commit/1deca55cf811a31ffc8b0fec85abc524cd7cc127))
* **tracing:** use method expression for real code.* source location ([f55cba7](https://github.com/anatolykoptev/go-code/commit/f55cba7dcf63225002b7fb5efc376c7167b3cb87))
* **tracing:** use method expression for real source location in code.* attrs ([f1d81d7](https://github.com/anatolykoptev/go-code/commit/f1d81d79b1cb17f530da311d16c339e5623cc80c))
* transfer table ownership on learnings + designmd store init ([#265](https://github.com/anatolykoptev/go-code/issues/265)) ([f43ab51](https://github.com/anatolykoptev/go-code/commit/f43ab51fa2faed63b8d4b183ec43ea7d42bec2c1))
* **understand:** bound semantic fallback + add tool timeout + guard semhealth self-join ([f836e1a](https://github.com/anatolykoptev/go-code/commit/f836e1af40dcd12bd79768a553a32901f02c2e3a))
* update dataflow tool description for TS/JS/Rust support ([be77707](https://github.com/anatolykoptev/go-code/commit/be77707b321a3197ec8f59a6fd5ddc6421ec3a13))
* use -mod=vendor for go/packages when vendor/ exists ([bbb9ffd](https://github.com/anatolykoptev/go-code/commit/bbb9ffda70d3105e1668990eac1f0d98e1ea29e9))
* use concrete slog.TextHandler as slogh base to avoid log bridge deadlock ([#96](https://github.com/anatolykoptev/go-code/issues/96)) ([5bdda73](https://github.com/anatolykoptev/go-code/commit/5bdda7345499b61a872a696c68e464a1e40b7c07))
* use golang:1.26-alpine runtime to enable go/types enhanced call resolution ([13f7450](https://github.com/anatolykoptev/go-code/commit/13f7450fd4d5b10d5cf6c4ad33414d5b140bde31))
* use slog.InfoContext in hooks for trace_id correlation ([#97](https://github.com/anatolykoptev/go-code/issues/97)) ([2475ad8](https://github.com/anatolykoptev/go-code/commit/2475ad8192af05e278498874bdb1025c9b173818))
* use structural search for prepare_change call_site_count ([9760dbd](https://github.com/anatolykoptev/go-code/commit/9760dbd9de3a611fcbfe454d47079b95338d0781))
* **vendor:** commit tree-sitter PHP cgo headers stripped by go mod vendor ([#17](https://github.com/anatolykoptev/go-code/issues/17)) ([f2bd895](https://github.com/anatolykoptev/go-code/commit/f2bd895c8c089844e8fd669c5369e918bbba83a3))
* word-boundary guards for short symbol names in ox-codes scoped search ([ae933d5](https://github.com/anatolykoptev/go-code/commit/ae933d528f18111713699322159472b3ebf90b59))


### Performance

* **ci:** -short merge gate + nightly full suite (26m -&gt; ~min) ([#301](https://github.com/anatolykoptev/go-code/issues/301)) ([b1c5e5a](https://github.com/anatolykoptev/go-code/commit/b1c5e5abd9fca01207464cbc94b3088299770959))
* compact hand-built XML formatters + code_compare metrics json ([#261](https://github.com/anatolykoptev/go-code/issues/261)) ([056d513](https://github.com/anatolykoptev/go-code/commit/056d5137117b053d55c820703b10a1eb73ea5d2b))
* **debug_investigate:** Sprint A — parallel Prom queries + skip LLM on quiet signal (6× speedup) ([#78](https://github.com/anatolykoptev/go-code/issues/78)) ([acb1365](https://github.com/anatolykoptev/go-code/commit/acb13657ca917e95d553855794071522b690007c))
* drop MCP response indentation + duration-only meta footer ([#260](https://github.com/anatolykoptev/go-code/issues/260)) ([f05fe08](https://github.com/anatolykoptev/go-code/commit/f05fe08fff6e32e85f9a53d4530e89ba6ede66ac))
* **go-code:** batch sparse-embedding writes + raise backfill deadline ([#200](https://github.com/anatolykoptev/go-code/issues/200)) ([62095a2](https://github.com/anatolykoptev/go-code/commit/62095a2f1a4e66cfb68146eba04ad4b3252162cb))
* **go-code:** Phase 2c — batch initialCreationLines (BUG-FH-2b cold latency 34s→~3s) ([#159](https://github.com/anatolykoptev/go-code/issues/159)) ([7ccabb5](https://github.com/anatolykoptev/go-code/commit/7ccabb5570ad95888a4ab0b861c33cc1874d3402))
* **ingest:** process-level IngestRepo cache to eliminate redundant walks ([#464](https://github.com/anatolykoptev/go-code/issues/464)) ([#474](https://github.com/anatolykoptev/go-code/issues/474)) ([cbad6da](https://github.com/anatolykoptev/go-code/commit/cbad6da79f9a561d6436858fd6d5b30234a2db84))
* parallelize ox-codes dead code string ref checks (N serial → 10-concurrent) ([0433eec](https://github.com/anatolykoptev/go-code/commit/0433eece574d320fa82b68dc27631406e8555e6c))
* **parser:** add BenchmarkParseFile and BenchmarkBuildSnapshot ([#404](https://github.com/anatolykoptev/go-code/issues/404)) ([bfacd99](https://github.com/anatolykoptev/go-code/commit/bfacd996cf4b0f0467b397dee0b9b3187b024ffb))
* **parser:** share one tree between ParseFile and ExtractCalls ([#400](https://github.com/anatolykoptev/go-code/issues/400)) ([#408](https://github.com/anatolykoptev/go-code/issues/408)) ([878fa58](https://github.com/anatolykoptev/go-code/commit/878fa58857c659b513b37c3d53f89b2e8313b3e2))
* **parser:** single-parse Svelte runes instead of double parse ([#406](https://github.com/anatolykoptev/go-code/issues/406)) ([b79df56](https://github.com/anatolykoptev/go-code/commit/b79df5669c261f8e582e89ade77aee4353654b65)), closes [#401](https://github.com/anatolykoptev/go-code/issues/401)
* **review:** cap review_delta impacted_symbols by default ([#391](https://github.com/anatolykoptev/go-code/issues/391)) ([#415](https://github.com/anatolykoptev/go-code/issues/415)) ([a708a9a](https://github.com/anatolykoptev/go-code/commit/a708a9a2276a4449e06ed6a42fcf2e1ed303693b))
* **scip:** parallelize multi-language SCIP indexing ([#465](https://github.com/anatolykoptev/go-code/issues/465)) ([#471](https://github.com/anatolykoptev/go-code/issues/471)) ([84e3908](https://github.com/anatolykoptev/go-code/commit/84e39084ae002715091314c27d31632ab19df084))
* **test:** parallelize DB-free test packages (gate ~8m -&gt; ~3.2m) ([#302](https://github.com/anatolykoptev/go-code/issues/302)) ([dbaee80](https://github.com/anatolykoptev/go-code/commit/dbaee80570e690761a8e549f8e30a321c16686ab))


### Changed

* **age:** drop per-connection LOAD; rely on shared_preload_libraries with startup check ([#111](https://github.com/anatolykoptev/go-code/issues/111)) ([1423fb7](https://github.com/anatolykoptev/go-code/commit/1423fb7e9ab85405c2ab159a277f7c41594199b2))
* **cache:** migrate ParseCache onto generic cache.LRU + per-cache tests + semhealth fixture ([147e380](https://github.com/anatolykoptev/go-code/commit/147e3801c30079fb9bf5a63116f45d8070c2e844))
* **callgraph:** move extractGoImplements into EnrichWithTypedResolution ([#467](https://github.com/anatolykoptev/go-code/issues/467)) ([#472](https://github.com/anatolykoptev/go-code/issues/472)) ([bec6190](https://github.com/anatolykoptev/go-code/commit/bec61902aa2ab120d4a38eb1894d035a99abe237))
* **callgraph:** unified ingest→parse→build→enrich pipeline ([#463](https://github.com/anatolykoptev/go-code/issues/463)) ([#475](https://github.com/anatolykoptev/go-code/issues/475)) ([#478](https://github.com/anatolykoptev/go-code/issues/478)) ([a6c9896](https://github.com/anatolykoptev/go-code/commit/a6c989607b1d7771a928b64f20aeb1b71041d491))
* **clients:** migrate websearch/oxcodes onto httputil.Client ([#283](https://github.com/anatolykoptev/go-code/issues/283)) ([d5c9fe4](https://github.com/anatolykoptev/go-code/commit/d5c9fe4d4916e264eb96273dba5ee4a2616b30b3))
* **codegraph:** unify IMPLEMENTS edge paths — single construction via buildRelationshipEdges ([#461](https://github.com/anatolykoptev/go-code/issues/461)) ([7ba230e](https://github.com/anatolykoptev/go-code/commit/7ba230e3d4d439877ee8ee9c0cbfd0c536ccabc4))
* consolidate dominant-language argmax into one canonical helper ([#285](https://github.com/anatolykoptev/go-code/issues/285)) ([034b52b](https://github.com/anatolykoptev/go-code/commit/034b52b346e217ba3c922eb1636c9afb1cab0db9))
* consolidate symbol_boost_adapter.go into register.go ([fdc05cb](https://github.com/anatolykoptev/go-code/commit/fdc05cb9efa44576181b4b4e03d2c7dd281cef83))
* **embeddings:** use go-kit cache.WithMetrics (v0.33.0 bump) ([#8](https://github.com/anatolykoptev/go-code/issues/8)) ([82ee614](https://github.com/anatolykoptev/go-code/commit/82ee614a44d1ad84a7224cc94479a2a1ef6ac942))
* **embed:** migrate to go-kit/embed v0.30.0 ([#2](https://github.com/anatolykoptev/go-code/issues/2)) ([6dfe5b7](https://github.com/anatolykoptev/go-code/commit/6dfe5b72289a576dc47193e9ad7687ccdb90a4a4))
* generic cache.LRU for 4 caches + dedup 3 helpers ([09541dc](https://github.com/anatolykoptev/go-code/commit/09541dc5f4afab73921f4354ab29fc439bd60de2))
* **go-code:** decompose computeHealth into per-subscore helpers ([#183](https://github.com/anatolykoptev/go-code/issues/183)) ([a18b50c](https://github.com/anatolykoptev/go-code/commit/a18b50c50df47126f743278a864e1ac8dce61f3e))
* **go-code:** decompose formatInvestigationResult into per-section writers ([#182](https://github.com/anatolykoptev/go-code/issues/182)) ([1c609df](https://github.com/anatolykoptev/go-code/commit/1c609df112873c4cb1cd557a6fff827e7909f5fd))
* **go-code:** decompose ScanHtmxRefs (cyclomatic 57→3) ([#204](https://github.com/anatolykoptev/go-code/issues/204)) ([b1f31a0](https://github.com/anatolykoptev/go-code/commit/b1f31a00de0c7e3732fdbd9f52a95115d5430f2a))
* **go-code:** dedup 3 copy-paste blocks (dupl → 0 repo-wide) ([#181](https://github.com/anatolykoptev/go-code/issues/181)) ([1b65845](https://github.com/anatolykoptev/go-code/commit/1b6584554e5f484eb1949997c26306ac368f0b1d))
* **go-code:** migrate error/no-match + design_search/semantic XML onto typed structs + xml.Marshal ([#263](https://github.com/anatolykoptev/go-code/issues/263)) ([3c1040e](https://github.com/anatolykoptev/go-code/commit/3c1040eabee469665c2061b1d3e810de756ee467))
* **go-code:** migrate final 3 hand-rolled XML formatters onto xml.Marshal + collapse error/json clones ([#266](https://github.com/anatolykoptev/go-code/issues/266)) ([cb81b9d](https://github.com/anatolykoptev/go-code/commit/cb81b9d20f5cf3e4c2ac3c2545e1e5ada7575a0b))
* **go-code:** migrate site_analyze/site_crawl/debug_investigate XML onto typed structs + xml.Marshal ([#262](https://github.com/anatolykoptev/go-code/issues/262)) ([d32d1e4](https://github.com/anatolykoptev/go-code/commit/d32d1e46102bc9acaadf10ae872e116a4cf1b716))
* **go-code:** split AGE/data connection pools + schema-qualification guards ([#178](https://github.com/anatolykoptev/go-code/issues/178)) ([71fa9d8](https://github.com/anatolykoptev/go-code/commit/71fa9d8b4ae0c5b76d9bc12a26ea96b42ed167e8))
* **go-code:** unify 3 import resolvers into internal/importresolve ([#188](https://github.com/anatolykoptev/go-code/issues/188)) ([0914b6a](https://github.com/anatolykoptev/go-code/commit/0914b6a48157cce11af85203c8511cb2a114c0f5))
* **go-code:** unify tokenization + stopwords into internal/lextoken leaf (BM25F P2) ([#205](https://github.com/anatolykoptev/go-code/issues/205)) ([4172f05](https://github.com/anatolykoptev/go-code/commit/4172f0557d8b6bc492ef0a19a66a17ff4a73626f))
* **ingest:** unify parseFilesParallel into shared ingest.ParseFilesParallel ([#469](https://github.com/anatolykoptev/go-code/issues/469)) ([#473](https://github.com/anatolykoptev/go-code/issues/473)) ([a10b487](https://github.com/anatolykoptev/go-code/commit/a10b487791083b4f5240c99c34d905e134369f48))
* **llm-obs:** swap direct-prom histogram for kit Registry.ObserveSeconds ([#122](https://github.com/anatolykoptev/go-code/issues/122)) ([cfd0fb5](https://github.com/anatolykoptev/go-code/commit/cfd0fb50deb3d2f6d89e83be1739c01c1f8f2813))
* migrate to go-kit/rerank.RRF (v0.32.0 bump) ([#3](https://github.com/anatolykoptev/go-code/issues/3)) ([a53e185](https://github.com/anatolykoptev/go-code/commit/a53e1856ccba41148160d93155e502009d29f10b))
* **pgutil:** extract TransferOwnership shared helper (DRY PR [#112](https://github.com/anatolykoptev/go-code/issues/112)) ([#114](https://github.com/anatolykoptev/go-code/issues/114)) ([a2af05c](https://github.com/anatolykoptev/go-code/commit/a2af05cde8a658be9ca9395b9d0e477605043806))
* remove unused buildSingleEdge function ([6c5f2fb](https://github.com/anatolykoptev/go-code/commit/6c5f2fb5c588cb4c9e366ab5641e1fe991670cd5))
* **repo_analyze:** slim XML output without losing agent value ([#16](https://github.com/anatolykoptev/go-code/issues/16)) ([0cc19dc](https://github.com/anatolykoptev/go-code/commit/0cc19dcd1df8ab004f9cbeef81a0ce39e6f23e54))
* **tools:** trim noise from symbol_search and explore output ([#20](https://github.com/anatolykoptev/go-code/issues/20)) ([58ec879](https://github.com/anatolykoptev/go-code/commit/58ec8790f9ac7bb4a5f08c486ea61baa30abe5be))
* **xml:** close Tree xmlCDATA gap + sync assertNoEmptyTag godoc ([#32](https://github.com/anatolykoptev/go-code/issues/32)) ([9c48df0](https://github.com/anatolykoptev/go-code/commit/9c48df08a4c214a3fce4bf9fe063228d6f40f431))
* **xml:** convert empty-prone xmlCDATA fields to pointer-form ([#25](https://github.com/anatolykoptev/go-code/issues/25)) ([f7cfed7](https://github.com/anatolykoptev/go-code/commit/f7cfed71e3da02b9cc2e7978b4bf2dd674a70ee8))


### Documentation

* actualize README — 30 tools, expand Tools table to 27 rows ([4cc6479](https://github.com/anatolykoptev/go-code/commit/4cc6479a311845fd5e603486e619b98c28e4bd64))
* actualize README — 30 tools, expand Tools table to 27 rows ([2be34d5](https://github.com/anatolykoptev/go-code/commit/2be34d5b192f699ee0f145a03e012dc168876b7f))
* **adr:** 0002 environment detect & verify ([#297](https://github.com/anatolykoptev/go-code/issues/297)) ([40df3f2](https://github.com/anatolykoptev/go-code/commit/40df3f23b8188d62755f8497892cb2e1c8a3690e))
* **adr:** 0002 harden Phase 1 resolution per re-review ([#299](https://github.com/anatolykoptev/go-code/issues/299)) ([894f622](https://github.com/anatolykoptev/go-code/commit/894f622bd48d51375fc490754d5011693cb5c5b5))
* **adr:** 0002 Phase 1 design resolution — close 6 security-cost blockers (design-only) ([#298](https://github.com/anatolykoptev/go-code/issues/298)) ([ff637be](https://github.com/anatolykoptev/go-code/commit/ff637be59d0ca97b15d114250bc95ba9bda3ab60))
* **adr:** add 0003 callgraph resolver strategy ([#322](https://github.com/anatolykoptev/go-code/issues/322)) ([e2c8690](https://github.com/anatolykoptev/go-code/commit/e2c8690e1c5d6f668121fc05b90644e306af102e))
* **CLAUDE:** language count 11 → 13 (added Svelte, Astro) ([#100](https://github.com/anatolykoptev/go-code/issues/100)) ([cbb7b59](https://github.com/anatolykoptev/go-code/commit/cbb7b59264e1a49ee492f6d97e67c1d8f2b54bcd))
* **debug_investigate:** align hint_kind count with code ([#328](https://github.com/anatolykoptev/go-code/issues/328)) ([cd7ceae](https://github.com/anatolykoptev/go-code/commit/cd7ceae5e0cc5b075eb5c0c803c40f7a3a7802ff))
* fix v1.21 roadmap conflict + sync CLAUDE.md parser language count ([#103](https://github.com/anatolykoptev/go-code/issues/103)) ([641e0c7](https://github.com/anatolykoptev/go-code/commit/641e0c78cb98421075e63c9e4c0f2e9ca2c5cf44))
* **followups:** record fleet-wide codegraph route-&gt;graph breakage (FU-CG.1-6) [no-deploy] ([#166](https://github.com/anatolykoptev/go-code/issues/166)) ([b87215f](https://github.com/anatolykoptev/go-code/commit/b87215ff20d107c1ea7fe65b5d05b7e67e231fe1))
* **memos:** mark astro-template-refs memo as implemented ([#240](https://github.com/anatolykoptev/go-code/issues/240)) ([182646e](https://github.com/anatolykoptev/go-code/commit/182646e502ff6a35351fd44b955dad9ced4bfbb0))
* **migration:** record as-run hardened ag_catalog backfill (executed 2026-05-31) ([#175](https://github.com/anatolykoptev/go-code/issues/175)) ([df800b9](https://github.com/anatolykoptev/go-code/commit/df800b994a014f0e2fafe4f3e0fe9822672bb3c3))
* move MILESTONES.md to docs/ root (alongside ROADMAP.md and COMPETITORS.md) ([2cb7b9a](https://github.com/anatolykoptev/go-code/commit/2cb7b9ab0cc9b851945a86b4014890ef6bd3fc46))
* phase 1 repowise smoke test findings [no-deploy] ([235b863](https://github.com/anatolykoptev/go-code/commit/235b8639fdc9a6aaf5e9bfaa49a01bd85904534f))
* phase 2b smoke verified + BUG-FH-2b cold-latency followup [no-deploy] ([509e77a](https://github.com/anatolykoptev/go-code/commit/509e77acbc64f32e5674e5868a4a198b37ff72b0))
* **plan:** mark Phase 1 complete (Tasks 1-4) ([#50](https://github.com/anatolykoptev/go-code/issues/50)) ([55c8672](https://github.com/anatolykoptev/go-code/commit/55c867239b05fd31fa95109b4f5d87ff98398613))
* **plan:** mark Phase 2 complete (Tasks 5-8) ([#55](https://github.com/anatolykoptev/go-code/issues/55)) ([1407423](https://github.com/anatolykoptev/go-code/commit/1407423da2c60ad01153e747a7de2ef4bd420734))
* replace Mac home paths with generic placeholder ([92953e2](https://github.com/anatolykoptev/go-code/commit/92953e287948f246e407e6e9dc5676ecc3a08b09))
* **ROADMAP:** add v1.21 — OTel Function Attribution shipped 2026-05-09 ([#101](https://github.com/anatolykoptev/go-code/issues/101)) ([5b6da5e](https://github.com/anatolykoptev/go-code/commit/5b6da5e51a0e407fa6aea55ae43435fbdfc0ed5b))
* update ROADMAP, competitors, milestones for v1.19.x-v1.21 ([bc450b5](https://github.com/anatolykoptev/go-code/commit/bc450b5c855c2565076cbb011fe41416925f2ab0))
* write comprehensive MILESTONES.md ([a9bd3f7](https://github.com/anatolykoptev/go-code/commit/a9bd3f7c818187d3a1de657358b525ae0ae976c2))

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
