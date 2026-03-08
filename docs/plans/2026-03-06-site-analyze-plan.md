# site_analyze Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `site_analyze` MCP tool to go-code that detects website technology stacks and extracts frontend source code via ox-browser.

**Architecture:** ox-browser (Rust :8901) gets a new `POST /analyze` endpoint with Wappalyzer-compatible fingerprinting. go-code (Go :8897) gets a new `site_analyze` MCP tool that calls ox-browser, optionally downloads JS bundles, extracts source maps, and saves sources for analysis with existing tools.

**Tech Stack:** Rust (ox-browser: axum, dom_query, serde), Go (go-code: net/http, encoding/json), Wappalyzer fingerprint DB (JSON)

---

## Task 1: ox-browser — Fingerprint Database Loader

**Files:**
- Create: `/path/to/repos/src/ox-browser/crates/security/src/fingerprint.rs`
- Modify: `/path/to/repos/src/ox-browser/crates/security/src/lib.rs`
- Modify: `/path/to/repos/src/ox-browser/crates/security/Cargo.toml`
- Test: inline `#[cfg(test)]` in `fingerprint.rs`
- Create: `/path/to/repos/src/ox-browser/crates/security/src/fingerprints.json`

**Context:** The `security` crate exists but is empty. We'll use it for tech fingerprinting. The fingerprint DB is a simplified Wappalyzer-compatible JSON embedded at build time.

**Step 1: Add serde dependencies to security crate**

Edit `/path/to/repos/src/ox-browser/crates/security/Cargo.toml`:
```toml
[package]
name = "ox-security"
version.workspace = true
edition.workspace = true

[dependencies]
serde = { version = "1", features = ["derive"] }
serde_json = "1"
regex = "1"
tracing.workspace = true
```

**Step 2: Create fingerprints.json with 30 key technologies**

Create `/path/to/repos/src/ox-browser/crates/security/src/fingerprints.json`. This is a subset of Wappalyzer format covering the most common technologies:

```json
{
  "categories": {
    "1": "CMS",
    "12": "JS Framework",
    "18": "Web Server",
    "22": "Web Framework",
    "47": "CSS Framework",
    "10": "Analytics",
    "31": "CDN",
    "27": "Programming Language"
  },
  "technologies": {
    "WordPress": {
      "cats": [1],
      "html": ["wp-content", "wp-includes"],
      "headers": { "X-Powered-By": "WordPress" },
      "meta": { "generator": "WordPress" },
      "scripts": ["wp-includes/js"]
    },
    "React": {
      "cats": [12],
      "html": ["data-reactroot", "data-reactid", "__REACT_DEVTOOLS"],
      "scripts": ["react\\.production\\.min\\.js", "react-dom"]
    },
    "Vue.js": {
      "cats": [12],
      "html": ["data-v-", "Vue\\.js"],
      "scripts": ["vue\\.runtime", "vue\\.global"]
    },
    "Angular": {
      "cats": [12],
      "html": ["ng-version", "ng-app", "_nghost"],
      "scripts": ["angular", "zone\\.js"]
    },
    "Next.js": {
      "cats": [22],
      "html": ["__NEXT_DATA__", "_next/static"],
      "headers": { "X-Powered-By": "Next.js" },
      "scripts": ["_next/static"]
    },
    "Nuxt.js": {
      "cats": [22],
      "html": ["__NUXT__", "_nuxt/"],
      "scripts": ["_nuxt/"]
    },
    "Svelte": {
      "cats": [12],
      "html": ["svelte-", "__svelte"]
    },
    "jQuery": {
      "cats": [12],
      "scripts": ["jquery[.-]"]
    },
    "Bootstrap": {
      "cats": [47],
      "html": ["bootstrap\\.min\\.css", "bootstrap\\.min\\.js"],
      "scripts": ["bootstrap"]
    },
    "Tailwind CSS": {
      "cats": [47],
      "html": ["tailwindcss", "tailwind\\.min\\.css"]
    },
    "Google Analytics": {
      "cats": [10],
      "html": ["google-analytics\\.com/analytics", "googletagmanager\\.com/gtag"],
      "scripts": ["google-analytics", "googletagmanager"]
    },
    "Google Tag Manager": {
      "cats": [10],
      "html": ["googletagmanager\\.com/gtm"],
      "scripts": ["googletagmanager\\.com/gtm"]
    },
    "nginx": {
      "cats": [18],
      "headers": { "Server": "nginx" }
    },
    "Apache": {
      "cats": [18],
      "headers": { "Server": "Apache" }
    },
    "Cloudflare": {
      "cats": [31],
      "headers": { "cf-ray": "", "Server": "cloudflare" }
    },
    "Vercel": {
      "cats": [31],
      "headers": { "X-Vercel-Id": "", "Server": "Vercel" }
    },
    "Netlify": {
      "cats": [31],
      "headers": { "X-NF-Request-ID": "", "Server": "Netlify" }
    },
    "PHP": {
      "cats": [27],
      "headers": { "X-Powered-By": "PHP" }
    },
    "Express": {
      "cats": [22],
      "headers": { "X-Powered-By": "Express" }
    },
    "Django": {
      "cats": [22],
      "headers": { "X-Frame-Options": "DENY" },
      "html": ["csrfmiddlewaretoken", "django"]
    },
    "Ruby on Rails": {
      "cats": [22],
      "headers": { "X-Request-Id": "", "X-Runtime": "" },
      "meta": { "csrf-param": "authenticity_token" }
    },
    "Shopify": {
      "cats": [1],
      "html": ["cdn\\.shopify\\.com", "Shopify\\.theme"],
      "scripts": ["cdn\\.shopify\\.com"],
      "headers": { "X-ShopId": "" }
    },
    "Wix": {
      "cats": [1],
      "html": ["wix\\.com", "_wixCIDX"],
      "scripts": ["static\\.parastorage\\.com"]
    },
    "Gatsby": {
      "cats": [22],
      "html": ["___gatsby", "gatsby-"],
      "meta": { "generator": "Gatsby" }
    },
    "Remix": {
      "cats": [22],
      "html": ["__remixContext"]
    },
    "Material UI": {
      "cats": [47],
      "html": ["MuiButton", "MuiPaper", "MuiTypography"]
    },
    "Font Awesome": {
      "cats": [47],
      "html": ["font-awesome", "fontawesome"],
      "scripts": ["fontawesome"]
    },
    "Hotjar": {
      "cats": [10],
      "scripts": ["static\\.hotjar\\.com"]
    },
    "Stripe": {
      "cats": [10],
      "scripts": ["js\\.stripe\\.com"]
    },
    "Sentry": {
      "cats": [10],
      "scripts": ["browser\\.sentry-cdn\\.com", "sentry\\.io"]
    }
  }
}
```

**Step 3: Write fingerprint.rs with tests**

Create `/path/to/repos/src/ox-browser/crates/security/src/fingerprint.rs`:

```rust
//! Wappalyzer-compatible technology fingerprinting.

use std::collections::HashMap;
use serde::Deserialize;
use regex::RegexBuilder;

const DB_JSON: &str = include_str!("fingerprints.json");

#[derive(Debug, Clone)]
pub struct Detection {
    pub name: String,
    pub category: String,
    pub confidence: u8,
}

#[derive(Deserialize)]
struct FingerprintDB {
    categories: HashMap<String, String>,
    technologies: HashMap<String, TechDef>,
}

#[derive(Deserialize)]
struct TechDef {
    cats: Vec<u32>,
    #[serde(default)]
    html: Vec<String>,
    #[serde(default)]
    headers: HashMap<String, String>,
    #[serde(default)]
    meta: HashMap<String, String>,
    #[serde(default)]
    scripts: Vec<String>,
}

/// Fingerprinter matches HTML + headers against the embedded Wappalyzer DB.
pub struct Fingerprinter {
    db: FingerprintDB,
}

impl Fingerprinter {
    /// Load the embedded fingerprint database.
    pub fn new() -> Self {
        let db: FingerprintDB = serde_json::from_str(DB_JSON)
            .expect("embedded fingerprints.json is valid");
        Self { db }
    }

    /// Detect technologies from HTTP headers and HTML body.
    /// `headers` should be lowercase key → value.
    /// `meta_tags` should be name/property → content.
    pub fn detect(
        &self,
        headers: &HashMap<String, String>,
        html: &str,
        meta_tags: &HashMap<String, String>,
        script_srcs: &[String],
    ) -> Vec<Detection> {
        let mut results = Vec::new();
        let html_lower = html.to_lowercase();

        for (name, def) in &self.db.technologies {
            let mut confidence: u8 = 0;

            // Match HTML patterns.
            for pattern in &def.html {
                if let Ok(re) = RegexBuilder::new(pattern).case_insensitive(true).build() {
                    if re.is_match(&html_lower) {
                        confidence = confidence.saturating_add(50);
                        break;
                    }
                } else if html_lower.contains(&pattern.to_lowercase()) {
                    confidence = confidence.saturating_add(50);
                    break;
                }
            }

            // Match headers.
            for (hdr_name, hdr_pattern) in &def.headers {
                let hdr_lower = hdr_name.to_lowercase();
                if let Some(val) = headers.get(&hdr_lower) {
                    if hdr_pattern.is_empty() || val.to_lowercase().contains(&hdr_pattern.to_lowercase()) {
                        confidence = confidence.saturating_add(50);
                        break;
                    }
                }
            }

            // Match meta tags.
            for (meta_name, meta_pattern) in &def.meta {
                if let Some(content) = meta_tags.get(&meta_name.to_lowercase()) {
                    if content.to_lowercase().contains(&meta_pattern.to_lowercase()) {
                        confidence = confidence.saturating_add(25);
                        break;
                    }
                }
            }

            // Match script sources.
            for pattern in &def.scripts {
                if let Ok(re) = RegexBuilder::new(pattern).case_insensitive(true).build() {
                    for src in script_srcs {
                        if re.is_match(src) {
                            confidence = confidence.saturating_add(25);
                            break;
                        }
                    }
                }
                if confidence > 0 { break; }
            }

            if confidence > 0 {
                let cat_id = def.cats.first().copied().unwrap_or(0).to_string();
                let category = self.db.categories
                    .get(&cat_id)
                    .cloned()
                    .unwrap_or_else(|| "Other".into());
                results.push(Detection {
                    name: name.clone(),
                    category,
                    confidence: confidence.min(100),
                });
            }
        }

        results.sort_by(|a, b| b.confidence.cmp(&a.confidence));
        results
    }
}

impl Default for Fingerprinter {
    fn default() -> Self { Self::new() }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn empty_headers() -> HashMap<String, String> { HashMap::new() }
    fn empty_meta() -> HashMap<String, String> { HashMap::new() }

    #[test]
    fn detect_react_from_html() {
        let fp = Fingerprinter::new();
        let html = r#"<div id="root" data-reactroot="">Hello</div>"#;
        let results = fp.detect(&empty_headers(), html, &empty_meta(), &[]);
        assert!(results.iter().any(|d| d.name == "React"), "expected React, got: {:?}", results);
    }

    #[test]
    fn detect_nextjs_from_html() {
        let fp = Fingerprinter::new();
        let html = r#"<script id="__NEXT_DATA__" type="application/json">{}</script>"#;
        let results = fp.detect(&empty_headers(), html, &empty_meta(), &[]);
        assert!(results.iter().any(|d| d.name == "Next.js"), "expected Next.js, got: {:?}", results);
    }

    #[test]
    fn detect_nginx_from_headers() {
        let fp = Fingerprinter::new();
        let mut headers = HashMap::new();
        headers.insert("server".into(), "nginx/1.25.3".into());
        let results = fp.detect(&headers, "", &empty_meta(), &[]);
        assert!(results.iter().any(|d| d.name == "nginx"), "expected nginx, got: {:?}", results);
    }

    #[test]
    fn detect_cloudflare_from_headers() {
        let fp = Fingerprinter::new();
        let mut headers = HashMap::new();
        headers.insert("cf-ray".into(), "abc123".into());
        let results = fp.detect(&headers, "", &empty_meta(), &[]);
        assert!(results.iter().any(|d| d.name == "Cloudflare"), "expected Cloudflare, got: {:?}", results);
    }

    #[test]
    fn detect_wordpress_from_meta() {
        let fp = Fingerprinter::new();
        let mut meta = HashMap::new();
        meta.insert("generator".into(), "WordPress 6.5".into());
        let results = fp.detect(&empty_headers(), "", &meta, &[]);
        assert!(results.iter().any(|d| d.name == "WordPress"), "expected WordPress, got: {:?}", results);
    }

    #[test]
    fn detect_jquery_from_scripts() {
        let fp = Fingerprinter::new();
        let scripts = vec!["https://cdn.example.com/jquery-3.7.1.min.js".into()];
        let results = fp.detect(&empty_headers(), "", &empty_meta(), &scripts);
        assert!(results.iter().any(|d| d.name == "jQuery"), "expected jQuery, got: {:?}", results);
    }

    #[test]
    fn empty_input_returns_empty() {
        let fp = Fingerprinter::new();
        let results = fp.detect(&empty_headers(), "", &empty_meta(), &[]);
        assert!(results.is_empty());
    }

    #[test]
    fn multiple_techs_detected() {
        let fp = Fingerprinter::new();
        let html = r#"<div data-reactroot=""><script src="/_next/static/chunks/main.js"></script></div>"#;
        let mut headers = HashMap::new();
        headers.insert("server".into(), "nginx".into());
        let results = fp.detect(&headers, html, &empty_meta(), &[]);
        let names: Vec<&str> = results.iter().map(|d| d.name.as_str()).collect();
        assert!(names.contains(&"React"), "missing React in {:?}", names);
        assert!(names.contains(&"Next.js"), "missing Next.js in {:?}", names);
        assert!(names.contains(&"nginx"), "missing nginx in {:?}", names);
    }
}
```

**Step 4: Update security lib.rs**

```rust
pub mod fingerprint;
```

**Step 5: Run tests**

```bash
cd /path/to/repos/src/ox-browser && cargo test -p ox-security
```
Expected: 8 tests pass.

**Step 6: Commit**

```bash
cd /path/to/repos/src/ox-browser
git add crates/security/
git commit -m "feat(security): add Wappalyzer-compatible tech fingerprinting

30 technologies, 8 categories, regex + literal matching.
Embedded JSON DB at build time via include_str!.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 2: ox-browser — POST /analyze Endpoint

**Files:**
- Create: `/path/to/repos/src/ox-browser/crates/js/src/analyze.rs`
- Modify: `/path/to/repos/src/ox-browser/crates/js/src/lib.rs`
- Modify: `/path/to/repos/src/ox-browser/crates/js/Cargo.toml`
- Test: inline `#[cfg(test)]` in `analyze.rs`

**Context:** The `js` crate has `/fetch` and `/fetch-smart` endpoints. We add `/analyze` which uses `fetch-smart` internally, then runs fingerprinting + asset discovery on the response.

**Step 1: Add ox-security + ox-core dependencies to js crate**

Edit `/path/to/repos/src/ox-browser/crates/js/Cargo.toml` — add to `[dependencies]`:
```toml
ox-security = { path = "../security" }
ox-core = { path = "../core" }
```

**Step 2: Write analyze.rs**

Create `/path/to/repos/src/ox-browser/crates/js/src/analyze.rs`:

```rust
//! POST /analyze — fetch page, detect technologies, discover assets.

use std::collections::HashMap;
use std::time::Instant;

use axum::extract::State;
use axum::http::StatusCode;
use axum::Json;
use ox_core::Page;
use ox_http::detect_cloudflare;
use ox_security::fingerprint::{Detection, Fingerprinter};
use serde::{Deserialize, Serialize};

use crate::AppState;

#[derive(Deserialize)]
pub struct AnalyzeRequest {
    pub url: String,
}

#[derive(Serialize)]
pub struct AnalyzeResponse {
    pub url: String,
    pub status: u16,
    pub technologies: Vec<TechInfo>,
    pub meta: MetaInfo,
    pub assets: AssetInfo,
    pub method: String,
    pub cf_detected: bool,
    pub elapsed_ms: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

#[derive(Serialize)]
pub struct TechInfo {
    pub name: String,
    pub category: String,
    pub confidence: u8,
}

#[derive(Serialize)]
pub struct MetaInfo {
    pub generator: String,
    pub server: String,
    pub powered_by: String,
    pub title: String,
}

#[derive(Serialize)]
pub struct AssetInfo {
    pub scripts: Vec<String>,
    pub stylesheets: Vec<String>,
}

pub async fn analyze(
    State(state): State<AppState>,
    Json(req): Json<AnalyzeRequest>,
) -> (StatusCode, Json<AnalyzeResponse>) {
    let start = Instant::now();

    let resp = match state.http_client.get(&req.url).await {
        Ok(r) => r,
        Err(e) => {
            return (StatusCode::BAD_GATEWAY, Json(AnalyzeResponse {
                url: req.url, status: 0, technologies: vec![], assets: AssetInfo { scripts: vec![], stylesheets: vec![] },
                meta: MetaInfo { generator: String::new(), server: String::new(), powered_by: String::new(), title: String::new() },
                method: "direct".into(), cf_detected: false,
                elapsed_ms: start.elapsed().as_millis() as u64,
                error: Some(e.to_string()),
            }));
        }
    };

    let cf_detected = detect_cloudflare(&resp).is_some();
    let page = Page::new(resp.url.clone(), resp.status, &resp.body);

    // Build lowercase headers map.
    let headers: HashMap<String, String> = resp.headers.iter()
        .filter_map(|(k, v)| v.to_str().ok().map(|val| (k.to_string().to_lowercase(), val.to_owned())))
        .collect();

    // Extract meta tags as name→content map.
    let meta_tags: HashMap<String, String> = page.meta_tags().into_iter()
        .filter(|m| !m.name.is_empty())
        .map(|m| (m.name.to_lowercase(), m.content))
        .collect();

    // Extract script src URLs.
    let script_srcs: Vec<String> = page.select("script[src]").iter()
        .filter_map(|s| s.attr("src").map(|v| v.to_string()))
        .collect();

    // Extract stylesheet hrefs.
    let stylesheets: Vec<String> = page.select("link[rel='stylesheet'][href]").iter()
        .filter_map(|s| s.attr("href").map(|v| v.to_string()))
        .collect();

    // Run fingerprinting.
    let fingerprinter = Fingerprinter::new();
    let detections = fingerprinter.detect(&headers, &resp.body, &meta_tags, &script_srcs);

    let technologies: Vec<TechInfo> = detections.into_iter()
        .map(|d| TechInfo { name: d.name, category: d.category, confidence: d.confidence })
        .collect();

    // Extract meta info.
    let meta = MetaInfo {
        generator: meta_tags.get("generator").cloned().unwrap_or_default(),
        server: headers.get("server").cloned().unwrap_or_default(),
        powered_by: headers.get("x-powered-by").cloned().unwrap_or_default(),
        title: page.title(),
    };

    (StatusCode::OK, Json(AnalyzeResponse {
        url: req.url,
        status: resp.status,
        technologies,
        meta,
        assets: AssetInfo { scripts: script_srcs, stylesheets },
        method: "direct".into(),
        cf_detected,
        elapsed_ms: start.elapsed().as_millis() as u64,
        error: None,
    }))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn analyze_request_deserializes() {
        let json = r#"{"url": "https://example.com"}"#;
        let req: AnalyzeRequest = serde_json::from_str(json).unwrap();
        assert_eq!(req.url, "https://example.com");
    }

    #[test]
    fn analyze_response_serializes() {
        let resp = AnalyzeResponse {
            url: "https://example.com".into(),
            status: 200,
            technologies: vec![TechInfo { name: "React".into(), category: "JS Framework".into(), confidence: 100 }],
            meta: MetaInfo { generator: String::new(), server: "nginx".into(), powered_by: String::new(), title: "Test".into() },
            assets: AssetInfo { scripts: vec!["app.js".into()], stylesheets: vec!["style.css".into()] },
            method: "direct".into(),
            cf_detected: false,
            elapsed_ms: 500,
            error: None,
        };
        let json = serde_json::to_value(&resp).unwrap();
        assert_eq!(json["technologies"][0]["name"], "React");
        assert!(!json.as_object().unwrap().contains_key("error"));
    }
}
```

**Step 3: Register route in lib.rs**

Edit `/path/to/repos/src/ox-browser/crates/js/src/lib.rs` — add `mod analyze;` and the route:

```rust
mod analyze;
mod fetch;
mod fetch_smart;

// ... existing code ...

pub fn router(state: AppState) -> Router {
    Router::new()
        .route("/health", get(health))
        .route("/solve", post(solve))
        .route("/fetch", post(fetch::fetch))
        .route("/fetch-smart", post(fetch_smart::fetch_smart))
        .route("/analyze", post(analyze::analyze))
        .with_state(state)
}
```

**Step 4: Run tests**

```bash
cd /path/to/repos/src/ox-browser && cargo test -p ox-js
```

**Step 5: Build and deploy ox-browser**

```bash
cd /path/to/repos/deploy/example-server
docker compose build --no-cache ox-browser && docker compose up -d --no-deps --force-recreate ox-browser
```

**Step 6: Verify endpoint**

```bash
curl -s -X POST http://127.0.0.1:8901/analyze \
  -H 'Content-Type: application/json' \
  -d '{"url": "https://github.com"}' | jq '.technologies[:5]'
```

**Step 7: Commit**

```bash
cd /path/to/repos/src/ox-browser
git add crates/js/
git commit -m "feat(js): add POST /analyze endpoint for tech detection

Fetches page, runs Wappalyzer fingerprinting, discovers script/CSS assets.
Returns technologies with categories and confidence scores.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 3: go-code — ox-browser HTTP Client

**Files:**
- Create: `/path/to/repos/src/go-code/internal/webanalyze/client.go`
- Create: `/path/to/repos/src/go-code/internal/webanalyze/client_test.go`

**Context:** Simple HTTP client that calls ox-browser `/analyze` and `/fetch` endpoints. No external dependencies — uses stdlib `net/http` + `encoding/json`.

**Step 1: Write client_test.go**

```go
package webanalyze

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnalyze(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/analyze" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		resp := AnalyzeResponse{
			URL:    "https://example.com",
			Status: 200,
			Technologies: []Technology{
				{Name: "React", Category: "JS Framework", Confidence: 100},
			},
			Assets: Assets{
				Scripts:     []string{"app.js"},
				Stylesheets: []string{"style.css"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.Analyze(context.Background(), "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
	if len(resp.Technologies) != 1 || resp.Technologies[0].Name != "React" {
		t.Errorf("unexpected technologies: %v", resp.Technologies)
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := FetchResponse{Status: 200, Body: "hello"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.Fetch(context.Background(), "https://example.com/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body != "hello" {
		t.Errorf("expected body 'hello', got %q", resp.Body)
	}
}

func TestAnalyze_Error(t *testing.T) {
	c := NewClient("http://127.0.0.1:1") // connection refused
	_, err := c.Analyze(context.Background(), "https://example.com")
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}
```

**Step 2: Run tests — verify they fail**

```bash
cd /path/to/repos/src/go-code && go test ./internal/webanalyze/ -v
```
Expected: compilation error (package doesn't exist yet).

**Step 3: Write client.go**

```go
package webanalyze

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const clientTimeout = 30 * time.Second

// Client calls ox-browser HTTP API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates an ox-browser client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: clientTimeout},
	}
}

// Technology is a detected web technology.
type Technology struct {
	Name       string `json:"name"`
	Category   string `json:"category"`
	Confidence int    `json:"confidence"`
}

// Meta holds page metadata.
type Meta struct {
	Generator string `json:"generator"`
	Server    string `json:"server"`
	PoweredBy string `json:"powered_by"`
	Title     string `json:"title"`
}

// Assets holds discovered script and stylesheet URLs.
type Assets struct {
	Scripts     []string `json:"scripts"`
	Stylesheets []string `json:"stylesheets"`
}

// AnalyzeResponse is the response from ox-browser /analyze.
type AnalyzeResponse struct {
	URL          string       `json:"url"`
	Status       int          `json:"status"`
	Technologies []Technology `json:"technologies"`
	Meta         Meta         `json:"meta"`
	Assets       Assets       `json:"assets"`
	Method       string       `json:"method"`
	CFDetected   bool         `json:"cf_detected"`
	ElapsedMs    int          `json:"elapsed_ms"`
	Error        string       `json:"error,omitempty"`
}

// FetchResponse is the response from ox-browser /fetch.
type FetchResponse struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
	Error  string `json:"error,omitempty"`
}

// Analyze calls POST /analyze on ox-browser.
func (c *Client) Analyze(ctx context.Context, url string) (*AnalyzeResponse, error) {
	body, _ := json.Marshal(map[string]string{"url": url})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/analyze", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("analyze request: %w", err)
	}
	defer resp.Body.Close()

	var result AnalyzeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// Fetch calls POST /fetch on ox-browser to download a single URL.
func (c *Client) Fetch(ctx context.Context, url string) (*FetchResponse, error) {
	body, _ := json.Marshal(map[string]string{"url": url})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/fetch", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch request: %w", err)
	}
	defer resp.Body.Close()

	var result FetchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
```

**Step 4: Run tests**

```bash
cd /path/to/repos/src/go-code && go test ./internal/webanalyze/ -v
```
Expected: 3 tests pass.

**Step 5: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/webanalyze/
git commit -m "feat(webanalyze): add ox-browser HTTP client

Analyze() calls /analyze for tech detection.
Fetch() calls /fetch for downloading individual assets.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 4: go-code — Source Map Extractor

**Files:**
- Create: `/path/to/repos/src/go-code/internal/webanalyze/sourcemap.go`
- Create: `/path/to/repos/src/go-code/internal/webanalyze/sourcemap_test.go`

**Step 1: Write sourcemap_test.go**

```go
package webanalyze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSourceMap(t *testing.T) {
	raw := `{
		"version": 3,
		"sources": ["src/App.tsx", "src/utils/helper.ts"],
		"sourcesContent": ["import React from 'react';\n", "export function helper() {}\n"]
	}`
	sm, err := parseSourceMap([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(sm.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(sm.Sources))
	}
	if sm.Sources[0] != "src/App.tsx" {
		t.Errorf("expected src/App.tsx, got %s", sm.Sources[0])
	}
}

func TestWriteSourceTree(t *testing.T) {
	dir := t.TempDir()
	sm := &sourceMap{
		Sources:        []string{"src/App.tsx", "src/utils/helper.ts"},
		SourcesContent: []string{"import React from 'react';\n", "export function helper() {}\n"},
	}
	stats, err := writeSourceTree(dir, sm)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 2 {
		t.Errorf("expected 2 files, got %d", stats.Files)
	}
	// Verify files exist.
	data, err := os.ReadFile(filepath.Join(dir, "src", "App.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "import React from 'react';\n" {
		t.Errorf("unexpected content: %s", data)
	}
}

func TestParseSourceMap_Empty(t *testing.T) {
	raw := `{"version": 3, "sources": [], "sourcesContent": []}`
	sm, err := parseSourceMap([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(sm.Sources) != 0 {
		t.Errorf("expected 0 sources, got %d", len(sm.Sources))
	}
}

func TestWriteSourceTree_Mismatch(t *testing.T) {
	dir := t.TempDir()
	sm := &sourceMap{
		Sources:        []string{"a.js", "b.js"},
		SourcesContent: []string{"content"},
	}
	stats, err := writeSourceTree(dir, sm)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 1 {
		t.Errorf("expected 1 file (skipped mismatched), got %d", stats.Files)
	}
}

func TestFindSourceMapURL(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{"var x=1;\n//# sourceMappingURL=app.js.map", "app.js.map"},
		{"var x=1;\n//@ sourceMappingURL=old.js.map", "old.js.map"},
		{"var x=1;", ""},
		{"//# sourceMappingURL=data:application/json;base64,abc", ""},
	}
	for _, tt := range tests {
		got := findSourceMapURL(tt.body)
		if got != tt.want {
			t.Errorf("findSourceMapURL(%q) = %q, want %q", tt.body[:20], got, tt.want)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /path/to/repos/src/go-code && go test ./internal/webanalyze/ -run TestParse -v
```

**Step 3: Write sourcemap.go**

```go
package webanalyze

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type sourceMap struct {
	Version        int      `json:"version"`
	Sources        []string `json:"sources"`
	SourcesContent []string `json:"sourcesContent"`
}

// SourceStats holds counts of extracted source files.
type SourceStats struct {
	Files     int
	Languages map[string]int // extension → count
}

func parseSourceMap(data []byte) (*sourceMap, error) {
	var sm sourceMap
	if err := json.Unmarshal(data, &sm); err != nil {
		return nil, fmt.Errorf("parse sourcemap: %w", err)
	}
	return &sm, nil
}

func writeSourceTree(dir string, sm *sourceMap) (*SourceStats, error) {
	stats := &SourceStats{Languages: make(map[string]int)}
	for i, src := range sm.Sources {
		if i >= len(sm.SourcesContent) {
			break
		}
		content := sm.SourcesContent[i]
		if content == "" {
			continue
		}
		// Sanitize path: remove webpack:/// prefix, ../ traversal.
		clean := sanitizePath(src)
		if clean == "" {
			continue
		}
		fullPath := filepath.Join(dir, clean)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o640); err != nil {
			return nil, fmt.Errorf("write %s: %w", fullPath, err)
		}
		stats.Files++
		ext := strings.TrimPrefix(filepath.Ext(clean), ".")
		if ext != "" {
			stats.Languages[ext]++
		}
	}
	return stats, nil
}

// sanitizePath cleans webpack-style source paths.
func sanitizePath(p string) string {
	// Strip common prefixes.
	p = strings.TrimPrefix(p, "webpack:///")
	p = strings.TrimPrefix(p, "webpack://")
	p = strings.TrimPrefix(p, "./")
	// Block path traversal.
	if strings.Contains(p, "..") {
		return ""
	}
	// Skip node_modules.
	if strings.Contains(p, "node_modules/") {
		return ""
	}
	return p
}

// findSourceMapURL extracts the sourceMappingURL from JS content.
// Returns empty string if not found or if it's a data: URI.
func findSourceMapURL(body string) string {
	for _, prefix := range []string{"//# sourceMappingURL=", "//@ sourceMappingURL="} {
		idx := strings.LastIndex(body, prefix)
		if idx < 0 {
			continue
		}
		url := strings.TrimSpace(body[idx+len(prefix):])
		if nl := strings.IndexByte(url, '\n'); nl >= 0 {
			url = url[:nl]
		}
		// Skip data: URIs (too large, embedded).
		if strings.HasPrefix(url, "data:") {
			return ""
		}
		return url
	}
	return ""
}
```

**Step 4: Run tests**

```bash
cd /path/to/repos/src/go-code && go test ./internal/webanalyze/ -v
```
Expected: all tests pass.

**Step 5: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/webanalyze/sourcemap.go internal/webanalyze/sourcemap_test.go
git commit -m "feat(webanalyze): add source map parser and file extractor

Parses sourcemap JSON, sanitizes webpack paths, writes source tree.
Skips node_modules, data: URIs, path traversal.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 5: go-code — site_analyze MCP Tool

**Files:**
- Create: `/path/to/repos/src/go-code/cmd/go-code/tool_site_analyze.go`
- Modify: `/path/to/repos/src/go-code/cmd/go-code/config.go`
- Modify: `/path/to/repos/src/go-code/cmd/go-code/register.go`

**Step 1: Add OxBrowserURL to config.go**

Add field after `AutoIndexDirs`:
```go
	// OxBrowserURL is the base URL for ox-browser HTTP API (e.g. http://ox-browser:8901).
	// Empty means site_analyze tool is disabled.
	OxBrowserURL string
```

Add to `loadConfig()`:
```go
		OxBrowserURL:   env.Str("OX_BROWSER_URL", ""),
```

**Step 2: Write tool_site_analyze.go**

```go
package main

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/webanalyze"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SiteAnalyzeInput is the input schema for the site_analyze tool.
type SiteAnalyzeInput struct {
	URL  string `json:"url" jsonschema_description:"Website URL to analyze (e.g. https://example.com)"`
	Mode string `json:"mode,omitempty" jsonschema_description:"Analysis mode: detect (tech stack only, default) or full (detect + download source maps)"`
}

func registerSiteAnalyze(server *mcp.Server, cfg Config) {
	if cfg.OxBrowserURL == "" {
		return
	}
	client := webanalyze.NewClient(cfg.OxBrowserURL)
	workDir := cfg.WorkspaceDir

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "site_analyze",
		Description: "Analyze a website's technology stack and frontend code. " +
			"Detects CMS, JS frameworks, CSS frameworks, analytics, CDN, and server software. " +
			"In full mode, downloads JS bundles and extracts source maps for analysis " +
			"with explore, symbol_search, or dep_graph.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SiteAnalyzeInput) (*mcp.CallToolResult, error) {
		return handleSiteAnalyze(ctx, input, client, workDir)
	})
}

func handleSiteAnalyze(
	ctx context.Context, input SiteAnalyzeInput,
	client *webanalyze.Client, workDir string,
) (*mcp.CallToolResult, error) {
	if input.URL == "" {
		return errResult("url is required"), nil
	}

	resp, err := client.Analyze(ctx, input.URL)
	if err != nil {
		return errResult(fmt.Sprintf("analyze: %s", err)), nil
	}
	if resp.Error != "" {
		return errResult(fmt.Sprintf("ox-browser: %s", resp.Error)), nil
	}

	mode := input.Mode
	if mode == "" {
		mode = "detect"
	}

	if mode == "detect" {
		return textResult(formatDetectResponse(resp)), nil
	}

	// Full mode: download assets and extract source maps.
	domain := extractDomain(input.URL)
	outDir := filepath.Join(workDir, "sites", domain)

	stats, extractErr := extractSources(ctx, client, resp, outDir)
	return textResult(formatFullResponse(resp, outDir, stats, extractErr)), nil
}

func extractSources(
	ctx context.Context, client *webanalyze.Client,
	resp *webanalyze.AnalyzeResponse, outDir string,
) (*webanalyze.SourceStats, error) {
	totalStats := &webanalyze.SourceStats{Languages: make(map[string]int)}

	for _, scriptURL := range resp.Assets.Scripts {
		absURL := resolveURL(resp.URL, scriptURL)
		fetchResp, err := client.Fetch(ctx, absURL)
		if err != nil || fetchResp.Status != 200 {
			continue
		}

		mapURL := webanalyze.FindSourceMapURL(fetchResp.Body)
		if mapURL == "" {
			continue
		}

		absMapURL := resolveURL(absURL, mapURL)
		mapResp, err := client.Fetch(ctx, absMapURL)
		if err != nil || mapResp.Status != 200 {
			continue
		}

		sm, err := webanalyze.ParseSourceMap([]byte(mapResp.Body))
		if err != nil {
			continue
		}

		stats, err := webanalyze.WriteSourceTree(outDir, sm)
		if err != nil {
			continue
		}
		totalStats.Files += stats.Files
		for ext, count := range stats.Languages {
			totalStats.Languages[ext] += count
		}
	}
	return totalStats, nil
}

func formatDetectResponse(resp *webanalyze.AnalyzeResponse) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<response tool=\"site_analyze\">\n")
	fmt.Fprintf(&sb, "  <site url=\"%s\" status=\"%d\">\n", escapeXML(resp.URL), resp.Status)
	formatTechnologies(&sb, resp.Technologies)
	fmt.Fprintf(&sb, "    <meta generator=\"%s\" server=\"%s\" title=\"%s\"/>\n",
		escapeXML(resp.Meta.Generator), escapeXML(resp.Meta.Server), escapeXML(resp.Meta.Title))
	fmt.Fprintf(&sb, "    <assets scripts=\"%d\" stylesheets=\"%d\"/>\n",
		len(resp.Assets.Scripts), len(resp.Assets.Stylesheets))
	sb.WriteString("  </site>\n</response>")
	return sb.String()
}

func formatFullResponse(resp *webanalyze.AnalyzeResponse, outDir string, stats *webanalyze.SourceStats, extractErr error) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<response tool=\"site_analyze\">\n")
	fmt.Fprintf(&sb, "  <site url=\"%s\" status=\"%d\">\n", escapeXML(resp.URL), resp.Status)
	formatTechnologies(&sb, resp.Technologies)
	if stats != nil && stats.Files > 0 {
		fmt.Fprintf(&sb, "    <sources path=\"%s\" files=\"%d\">\n", escapeXML(outDir), stats.Files)
		for ext, count := range stats.Languages {
			fmt.Fprintf(&sb, "      <language name=\"%s\" files=\"%d\"/>\n", escapeXML(ext), count)
		}
		sb.WriteString("    </sources>\n")
		fmt.Fprintf(&sb, "    <hint>Use explore, symbol_search, or dep_graph with repo=\"%s\"</hint>\n", escapeXML(outDir))
	} else {
		msg := "No source maps found"
		if extractErr != nil {
			msg = extractErr.Error()
		}
		fmt.Fprintf(&sb, "    <sources files=\"0\" reason=\"%s\"/>\n", escapeXML(msg))
	}
	sb.WriteString("  </site>\n</response>")
	return sb.String()
}

func formatTechnologies(sb *strings.Builder, techs []webanalyze.Technology) {
	fmt.Fprintf(sb, "    <technologies count=\"%d\">\n", len(techs))
	for _, t := range techs {
		fmt.Fprintf(sb, "      <tech category=\"%s\" name=\"%s\" confidence=\"%d\"/>\n",
			escapeXML(t.Category), escapeXML(t.Name), t.Confidence)
	}
	sb.WriteString("    </technologies>\n")
}

func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "unknown"
	}
	return u.Hostname()
}

func resolveURL(base, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	u, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return u.ResolveReference(r).String()
}
```

**Step 3: Export sourcemap functions**

In `internal/webanalyze/sourcemap.go`, rename lowercase functions to exported:
- `parseSourceMap` → `ParseSourceMap`
- `writeSourceTree` → `WriteSourceTree`
- `findSourceMapURL` → `FindSourceMapURL`

Update tests accordingly.

**Step 4: Register in register.go**

Add after `registerSemanticSearch`:
```go
	registerSiteAnalyze(server, cfg)
```

**Step 5: Add OX_BROWSER_URL to docker-compose.yml**

In `go-code` environment section:
```yaml
      - OX_BROWSER_URL=http://ox-browser:8901
```

**Step 6: Build and test**

```bash
cd /path/to/repos/src/go-code && go build ./cmd/go-code/
go test ./internal/webanalyze/ ./cmd/go-code/ -v
```

**Step 7: Deploy**

```bash
cd /path/to/repos/deploy/example-server
docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
curl -s http://127.0.0.1:8897/health
```

**Step 8: Commit**

```bash
cd /path/to/repos/src/go-code
git add cmd/go-code/tool_site_analyze.go cmd/go-code/config.go cmd/go-code/register.go internal/webanalyze/
git commit -m "feat: add site_analyze MCP tool

Calls ox-browser /analyze for tech detection.
Full mode downloads JS bundles, extracts source maps,
saves original source code for analysis with existing tools.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 6: Integration Test

**Step 1: Test detect mode**

```bash
# Via MCP (reconnect first)
# Or via curl to ox-browser directly:
curl -s -X POST http://127.0.0.1:8901/analyze \
  -H 'Content-Type: application/json' \
  -d '{"url": "https://github.com"}' | jq '.technologies | length'
```
Expected: > 0 technologies detected.

**Step 2: Test site_analyze via MCP**

Use the go-code `site_analyze` tool:
```
site_analyze url="https://github.com" mode="detect"
```
Expected: XML response with technologies, meta, assets.

**Step 3: Test full mode on a site with source maps**

```
site_analyze url="https://excalidraw.com" mode="full"
```
Expected: technologies + extracted source files path (if source maps are public).

**Step 4: Test with CF-protected site**

```
site_analyze url="https://www.cloudflare.com" mode="detect"
```
Expected: should detect Cloudflare CDN, possibly other techs. ox-browser handles CF bypass.

**Step 5: Verify extracted sources work with explore**

If full mode extracted sources:
```
explore repo="/tmp/go-code-workspace/sites/excalidraw.com"
```
Expected: file tree, symbol counts, language breakdown of extracted TypeScript/JavaScript.
