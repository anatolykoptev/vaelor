# CVE/Vulnerability Security Check Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add dependency vulnerability checking via OSV API to code_health, surfacing known CVEs and factoring security into the health grade as a 14th sub-score.

**Architecture:** Reuse existing `internal/freshness` package infrastructure (parsed deps with name+version+language, bounded concurrency, Cache interface). New `vuln_check.go` queries OSV API per dependency, aggregates results into `VulnResult`. Grade scoring adds 14th weight `vulnSecurity=0.06`, rebalanced from 6 existing weights.

**Tech Stack:** OSV.dev REST API (POST /v1/query), `net/http`, `encoding/json`, existing `freshness.Cache` interface.

---

### Task 1: Types and OSV Ecosystem Mapping

**Files:**
- Create: `internal/freshness/vuln_check.go`
- Create: `internal/freshness/vuln_check_test.go`

**Step 1:** Create `vuln_check.go` with types (`VulnResult`, `VulnDep`) and `osvEcosystems` map (go→Go, npm/typescript→npm, python→PyPI, rust→crates.io, java→Maven, ruby→RubyGems, csharp→NuGet).

**Step 2:** Create `vuln_check_test.go` with `TestOSVEcosystems_AllLanguages` and `TestOSVEcosystems_Values`.

**Step 3:** Run: `GOWORK=off go test ./internal/freshness/... -run TestOSV -v` → PASS

**Step 4:** Commit: `feat(freshness): add vuln check types and OSV ecosystem mapping`

---

### Task 2: OSV API Client and Severity Extraction

**Files:**
- Modify: `internal/freshness/vuln_check.go`
- Modify: `internal/freshness/vuln_check_test.go`

**Step 1:** Add OSV request/response types (`osvRequest`, `osvResponse`, `osvVuln`, `osvSeverity`, `osvDBSpecific`).

**Step 2:** Implement `queryOSV(ctx, client, url, name, version, ecosystem) ([]VulnDep, error)` — POST to OSV, decode response, map vulns to `VulnDep`.

**Step 3:** Implement `extractSeverity(v osvVuln) string` — priority: `database_specific.severity` > CVSS_V3 vector > "MEDIUM" default.

**Step 4:** Implement `extractSeverityFromCVSS(vector string) string` — count `:H/` occurrences: 4+ → CRITICAL, 2+ → HIGH, 1+ → MEDIUM, else LOW.

**Step 5:** Tests: `TestQueryOSV_Vulnerable`, `TestQueryOSV_Clean`, `TestQueryOSV_NoVulnsField`, `TestExtractSeverity_CVSS` (table-driven), `TestExtractSeverity_DatabaseSpecific`. All use `httptest` mock server.

**Step 6:** Run: `GOWORK=off go test ./internal/freshness/... -run "TestQueryOSV|TestExtractSeverity" -v` → PASS

**Step 7:** Commit: `feat(freshness): add OSV API client and severity extraction`

---

### Task 3: CheckVulnerabilities with Bounded Concurrency

**Files:**
- Modify: `internal/freshness/vuln_check.go`
- Modify: `internal/freshness/vuln_check_test.go`

**Step 1:** Implement `CheckVulnerabilities(ctx, deps, client, osvURL) *VulnResult` — bounded concurrency (reuse `maxConcurrency`, `perLookupTimeout` from check.go), skip deps with unknown language or empty version, aggregate via `aggregateVulnResults`.

**Step 2:** Implement `aggregateVulnResults(results []vulnDepResult) *VulnResult` — count total/vulnerable/critical/high/medium/low, compute ratio.

**Step 3:** Tests: `TestCheckVulnerabilities_Mixed` (1 vuln + 2 clean), `TestCheckVulnerabilities_Empty`, `TestCheckVulnerabilities_UnknownLanguage`, `TestCheckVulnerabilities_AllClean`.

**Step 4:** Run: `GOWORK=off go test ./internal/freshness/... -run TestCheckVulnerabilities -v` → PASS

**Step 5:** Commit: `feat(freshness): add CheckVulnerabilities with bounded concurrency`

---

### Task 4: Add VulnSecurityRatio to RepoMetrics and Grade Scoring

**Files:**
- Modify: `internal/compare/compare.go:185` — add `VulnSecurityRatio float64` field after `DepFreshnessRatio`
- Modify: `internal/compare/grade.go` — add 14th weight `weightVulnSecurity=0.06`, rebalance (−0.01 from cognitiveComplexity, testCoverage, funcSize, errorHandling, nestingDepth, fileSize), add `targetVulnSecurity=1.0`, add `vulnScore` line and sum term
- Modify: `internal/compare/recommend.go` — add 14th entry in `computeSubScores()`, add `"vuln_security"` case in `buildMessage()`, update stale comments

**Rebalanced weights (must sum to 1.0):**
- cognitiveComplexity: 0.13→0.12, testCoverage: 0.13→0.12, funcSize: 0.09→0.08, errorHandling: 0.09→0.08, nestingDepth: 0.08→0.07, fileSize: 0.08→0.07, NEW vulnSecurity: 0.06

**Step 1:** Make all changes.

**Step 2:** Run: `GOWORK=off go test ./internal/compare/... -v -count=1` → PASS

**Step 3:** Commit: `feat(compare): add vuln_security as 14th health sub-score`

---

### Task 5: XML Output Types and Converter

**Files:**
- Modify: `cmd/go-code/tool_code_health_reports.go` — add `xmlVulnerabilities`, `xmlVulnDep` types, `convertVulnerabilities` function, add `Vulnerabilities` field to `xmlHealth`

**Step 1:** Add types and converter.

**Step 2:** Run: `GOWORK=off go build ./...` → success

**Step 3:** Commit: `feat(health): add XML types for vulnerability results`

---

### Task 6: Wire into tool_code_health.go

**Files:**
- Modify: `cmd/go-code/tool_code_health.go` — refactor freshness/vuln block to share `client`, add vuln check, update `buildHealthXML` signature to accept `*freshness.VulnResult`

**Key change:** Restructure the freshness+vuln block so `client` and `allDeps` are computed once and shared:

```
manifests := freshness.DiscoverManifests(root)
if len(manifests) > 0 {
    client := &http.Client{Timeout: 30s}
    allDeps := freshness.CollectDeps(manifests)
    // 1. Freshness check
    // 2. Vulnerability check
    // 3. Go runtime check
    // 4. Recompute score once
}
```

**Step 1:** Refactor and wire.

**Step 2:** Run: `GOWORK=off go build ./... && GOWORK=off go test ./internal/freshness/... ./internal/compare/... -count=1` → PASS

**Step 3:** Commit: `feat(health): wire vulnerability check into code_health tool`

---

### Task 7: Lint, Deploy, and Verify

**Step 1:** Run: `GOWORK=off make lint` — no new issues from our changes.

**Step 2:** Run: `GOWORK=off go test ./... -count=1` — all PASS.

**Step 3:** Deploy: `docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code`

**Step 4:** Verify via MCP: call `code_health` on `/home/krolik/src/go-code` and confirm `<vulnerabilities>` section appears.

---

### Task 8: Update Tool Description

**Files:**
- Modify: `cmd/go-code/tool_code_health.go` — update Description to mention "vulnerability security"

**Step 1:** Update description string.

**Step 2:** Commit: `docs: update code_health description to mention vulnerability scanning`
