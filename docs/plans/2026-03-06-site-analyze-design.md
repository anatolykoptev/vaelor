# site_analyze — Frontend Intelligence Design

## Goal

Analyze any website's technology stack and extract frontend source code for further analysis with existing go-code tools (explore, symbol_search, dep_graph).

## Architecture

Two-repo change:

1. **ox-browser** (Rust, :8901) — new `POST /analyze` endpoint: fetch page, detect technologies via Wappalyzer fingerprints, discover JS/CSS assets
2. **go-code** (Go, :8897) — new `site_analyze` MCP tool: calls ox-browser `/analyze`, optionally downloads bundles + extracts source maps, saves to workspace

```
Claude → site_analyze(url, mode=full) → go-code
  go-code → POST ox-browser:8901/analyze {url}
  ox-browser:
    ├── fetch-smart (wreq + CF bypass)
    ├── wappalyzer fingerprint (JSON DB, 3000+ techs)
    ├── extra signals (meta, headers, CSS classes, JS globals)
    └── asset discovery (<script>, <link> URLs)
  ← {technologies, meta, assets, html}

  go-code (mode=full):
    ├── download JS/CSS bundles via ox-browser /fetch
    ├── check //# sourceMappingURL → download .map
    ├── parse sourcemap JSON → write original files
    └── save to workspace dir
  ← XML response with tech stack + source path

Claude → explore(repo=workspace_path) → analyze extracted code
```

## ox-browser Changes (Rust)

### New crate: `ox-fingerprint`

Wappalyzer-compatible technology detection:

- Load fingerprint DB from `AliasIO/wappalyzer` JSON format (embedded at build time)
- Match against: HTTP headers, HTML body patterns, meta tags, script src, cookies
- Categories: CMS, JS framework, CSS framework, analytics, CDN, server, language, security
- Output: `Vec<Technology { name, category, version, confidence }>`

### New endpoint: `POST /analyze`

```json
// Request
{ "url": "https://example.com" }

// Response
{
  "url": "https://example.com",
  "status": 200,
  "technologies": [
    {"name": "React", "category": "js-framework", "version": "18.2", "confidence": 100},
    {"name": "Next.js", "category": "js-framework", "version": "14", "confidence": 100},
    {"name": "Tailwind CSS", "category": "css-framework", "confidence": 75}
  ],
  "meta": {
    "generator": "",
    "server": "nginx",
    "powered_by": ""
  },
  "assets": {
    "scripts": ["/_next/static/chunks/main.js", "..."],
    "stylesheets": ["/_next/static/css/app.css"],
    "sourcemaps": ["/_next/static/chunks/main.js.map"]
  },
  "method": "direct",
  "cf_detected": false,
  "elapsed_ms": 450
}
```

Uses existing `Page` for DOM queries (select, meta_tags) + existing `fetch-smart` for CF bypass.

### Fingerprint DB

Use `AliasIO/wappalyzer` `technologies/` JSON files (MIT licensed, community-maintained):
- 3000+ technology definitions
- Patterns: `headers`, `html`, `scripts`, `meta`, `cookies`, `js` globals
- Categories with IDs
- Embed via `include_str!` at build time, auto-update via CI

### Extra Signals (beyond Wappalyzer)

CSS class pattern detection (not in Wappalyzer DB):
- Tailwind: `class="flex p-4 text-sm bg-blue-500"` → regex `\b(flex|p-\d|text-\w+|bg-\w+-\d+)\b`
- Bootstrap: `class="container row col-md-*"` → regex `\b(container|row|col-(sm|md|lg|xl)-\d+)\b`
- MUI: `class="MuiButton-root"` → regex `\bMui[A-Z]\w+-`

JS framework globals (not always in Wappalyzer):
- Next.js: `__NEXT_DATA__`, `__next`
- Nuxt: `__NUXT__`, `__nuxt`
- Remix: `__remixContext`
- Svelte: `__svelte`

## go-code Changes (Go)

### New package: `internal/webanalyze/`

| File | Purpose | ~Lines |
|------|---------|--------|
| `client.go` | HTTP client for ox-browser `/analyze` and `/fetch` | 60 |
| `sourcemap.go` | Parse `.map` JSON, write original source files to disk | 80 |
| `assets.go` | Download JS/CSS bundles, find sourceMappingURL references | 70 |

### New tool: `site_analyze`

| File | Purpose | ~Lines |
|------|---------|--------|
| `cmd/go-code/tool_site_analyze.go` | MCP tool handler | 100 |

**Input:**
```json
{
  "url": "https://example.com",
  "mode": "detect"
}
```
- `url` (required): Website URL to analyze
- `mode` (optional): `detect` (default, tech stack only) or `full` (detect + download sources)

**Output (mode=detect):**
```xml
<response tool="site_analyze">
  <site url="https://example.com" status="200">
    <technologies count="5">
      <tech category="js-framework" name="React" version="18.2" confidence="100"/>
      <tech category="css-framework" name="Tailwind CSS" confidence="75"/>
      <tech category="analytics" name="Google Analytics"/>
      <tech category="cdn" name="Cloudflare"/>
      <tech category="server" name="nginx"/>
    </technologies>
    <assets scripts="12" stylesheets="4" sourcemaps="2"/>
  </site>
</response>
```

**Output (mode=full):** adds extracted sources:
```xml
<response tool="site_analyze">
  <site url="https://example.com" status="200">
    <technologies count="5">...</technologies>
    <sources path="/tmp/go-code-workspace/example.com" files="47">
      <language name="typescript" files="38"/>
      <language name="javascript" files="9"/>
    </sources>
    <hint>Use explore, symbol_search, or dep_graph with repo="/tmp/go-code-workspace/example.com"</hint>
  </site>
</response>
```

### Source Map Extraction (mode=full)

1. For each `<script src="...">` — download via `POST ox-browser:8901/fetch`
2. Check last line for `//# sourceMappingURL=...`
3. If found — download `.map` file
4. Parse JSON: `sources` (file paths) + `sourcesContent` (original code)
5. Write files to `workspace/domain/` preserving directory structure
6. Count languages by extension

No external dependency needed — source map format is simple JSON:
```json
{
  "version": 3,
  "sources": ["src/App.tsx", "src/components/Header.tsx"],
  "sourcesContent": ["import React...", "export const Header..."],
  "mappings": "..."
}
```

## Configuration

### go-code (docker-compose.yml)
```yaml
environment:
  - OX_BROWSER_URL=http://ox-browser:8901
```

### go-code config.go
```go
OxBrowserURL string  // env: OX_BROWSER_URL
```

## Dependencies

- **ox-browser**: no new external crates (JSON parsing via serde, DOM via dom_query — already present)
- **go-code**: no new Go dependencies (HTTP client + JSON parsing already available)
- **Wappalyzer DB**: embedded JSON, ~500KB after minification

## Limitations

- Source map extraction only works if `.map` files are publicly accessible (many prod sites disable them)
- Tech detection is static HTML-based — won't catch JS-only rendered frameworks without source analysis
- ox-browser `fetch-smart` handles Cloudflare but not all bot protection (Akamai, PerimeterX)
- Asset download is sequential per site to avoid rate limiting

## Testing

- ox-browser: unit tests for fingerprint matching (known HTML → expected techs)
- go-code: unit tests for sourcemap parsing (JSON → file tree)
- Integration: `site_analyze url=https://github.com mode=detect` should detect Ruby on Rails + React
