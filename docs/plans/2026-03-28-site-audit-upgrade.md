# go-code Site Analysis Upgrade Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Upgrade go-code site analysis from basic tech detection to full audit with scoring, browser metrics (CWV), and actionable findings.

**Architecture:** Three incremental phases. Phase 1 syncs types with ox-browser v2. Phase 2 adds `site_audit` tool proxying ox-browser `/site-audit`. Phase 3 adds `site_metrics` tool via go-browser for Core Web Vitals.

**Tech Stack:** Go 1.26+, go-browser (rod), ox-browser REST API, go-code MCP tools.

---

## Current State

| What | go-code has | ox-browser has | Gap |
|------|------------|---------------|-----|
| Tech detection | ✅ site_analyze → /analyze | ✅ | Synced |
| Performance report | ❌ Missing 4 new fields | ✅ speculation_rules, font_preloads, image formats | types.go outdated |
| Accessibility report | ❌ Missing 3 new fields | ✅ decorative, skip_link, reduced_motion | types.go outdated |
| Site audit (scoring) | ❌ No tool | ✅ /site-audit with grades + findings | Not exposed |
| Core Web Vitals | ❌ | ❌ | Needs go-browser |
| Security audit | ❌ | ✅ Full security report | Not exposed |

## Phase 1: Sync Types + Expose site_audit (Quick Win)

### Task 1.1: Update types.go with new ox-browser fields

**Files:**
- Modify: `internal/webanalyze/types.go`

Add to `PerformanceReport`:
```go
HasSpeculationRules bool `json:"has_speculation_rules"`
FontPreloads        int  `json:"font_preloads"`
ImagesModernFormat  int  `json:"images_modern_format"`
ImagesLegacyFormat  int  `json:"images_legacy_format"`
Score               int  `json:"score"`
```

Add to `AccessibilityReport`:
```go
ImagesDecorative  int  `json:"images_decorative"`
HasSkipLink       bool `json:"has_skip_link"`
HasReducedMotion  bool `json:"has_reduced_motion"`
```

### Task 1.2: Add SiteAudit types

**Files:**
- Modify: `internal/webanalyze/types.go`

```go
// AuditFinding is a single finding from the site audit.
type AuditFinding struct {
    Severity string `json:"severity"`
    Category string `json:"category"`
    Message  string `json:"message"`
    Fix      string `json:"fix"`
}

// CategoryAudit is audit results for one category.
type CategoryAudit struct {
    Score    int            `json:"score"`
    Grade    string         `json:"grade"`
    Findings []AuditFinding `json:"findings"`
}

// SiteAuditResponse is the response from ox-browser /site-audit.
type SiteAuditResponse struct {
    URL          string                    `json:"url"`
    OverallScore int                       `json:"overall_score"`
    OverallGrade string                    `json:"overall_grade"`
    Categories   map[string]CategoryAudit  `json:"categories"`
    TopIssues    []AuditFinding            `json:"top_issues"`
    ElapsedMs    int                       `json:"elapsed_ms"`
}
```

### Task 1.3: Add SiteAudit client method

**Files:**
- Modify: `internal/webanalyze/client.go`

```go
// SiteAudit calls POST /site-audit on ox-browser.
func (c *Client) SiteAudit(ctx context.Context, url, focus string) (*SiteAuditResponse, error) {
    payload := map[string]string{"url": url}
    if focus != "" {
        payload["focus"] = focus
    }
    body, _ := json.Marshal(payload)
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/site-audit", bytes.NewReader(body))
    if err != nil {
        return nil, fmt.Errorf("create request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("site-audit request: %w", err)
    }
    defer resp.Body.Close()

    var result SiteAuditResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("decode response: %w", err)
    }
    return &result, nil
}
```

### Task 1.4: Create site_audit MCP tool

**Files:**
- Create: `cmd/go-code/tool_site_audit.go`

New MCP tool `site_audit` with parameters:
- `url` (required) — URL to audit
- `focus` (optional) — "all", "seo", "performance", "accessibility", "security"

Output: formatted text with scores, grades, top findings with fix code snippets.

### Task 1.5: Register tool + update format

**Files:**
- Modify: `cmd/go-code/register.go` — add `registerSiteAudit(server, cfg)`
- Create: `cmd/go-code/tool_site_audit_format.go` — XML/text formatting

---

## Phase 2: Core Web Vitals via go-browser

### Task 2.1: Add CWV collection to go-browser

**Files (in go-browser repo):**
- Create: `metrics.go` — `CollectCWV(url string) (*CWVReport, error)`

Implementation:
1. `browser.Render(url)` to navigate
2. `page.Evaluate(performanceObserverJS)` to inject CWV collectors
3. `time.Sleep(3 * time.Second)` for metrics settling
4. `page.Evaluate(collectResultsJS)` to harvest FCP, LCP, CLS, TBT
5. Return structured `CWVReport`

```go
type CWVReport struct {
    FCP float64 `json:"fcp_ms"`  // First Contentful Paint
    LCP float64 `json:"lcp_ms"`  // Largest Contentful Paint
    CLS float64 `json:"cls"`     // Cumulative Layout Shift
    TBT float64 `json:"tbt_ms"`  // Total Blocking Time
}
```

### Task 2.2: Add /metrics HTTP endpoint to go-browser server

**Files (in go-browser repo):**
- Modify: `serve.go` — add `POST /metrics` handler

### Task 2.3: Add site_metrics MCP tool to go-code

**Files (in go-code repo):**
- Create: `cmd/go-code/tool_site_metrics.go`
- Modify: `internal/webanalyze/client.go` — add `Metrics()` method calling go-browser `/metrics`

New tool `site_metrics` with parameters:
- `url` (required) — URL to measure
- Output: CWV report with Lighthouse-compatible grading

Grading thresholds (same as Lighthouse):
- FCP: Good < 1.8s, Needs Improvement < 3s, Poor > 3s
- LCP: Good < 2.5s, Needs Improvement < 4s, Poor > 4s
- CLS: Good < 0.1, Needs Improvement < 0.25, Poor > 0.25
- TBT: Good < 200ms, Needs Improvement < 600ms, Poor > 600ms

---

## Phase 3: Unified Site Intelligence

### Task 3.1: Merge audit + metrics into single tool

Enhance `site_audit` to optionally include CWV:
- `browser: false` (default) — fast HTTP+HTML audit via ox-browser (~1.5s)
- `browser: true` — also runs CWV via go-browser (~5-8s)

Browser metrics integrated into performance score:
- Performance = 40% HTTP checks + 60% CWV
- Overall = (SEO + Performance + Accessibility + Security) / 4

---

## Dependency Graph

```
Phase 1: Task 1.1 → 1.2 → 1.3 → 1.4 → 1.5 (sequential, same crate)

Phase 2: Task 2.1 (go-browser) → 2.2 (go-browser) → 2.3 (go-code)

Phase 3: Task 3.1 (after Phase 1 + 2)
```

## Environment Variables (new)

| Variable | Default | Notes |
|----------|---------|-------|
| `GO_BROWSER_URL` | optional | go-browser HTTP endpoint for CWV metrics |

## Notes

- Phase 1 is a quick win — just types + HTTP proxy, no new deps
- Phase 2 needs go-browser server running (currently library-only, server in `cmd/server/`)
- Phase 3 combines both into one cohesive experience
- All tools register only if their backend URL is configured (graceful degradation)
