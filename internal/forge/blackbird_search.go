package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
	"github.com/anatolykoptev/vaelor/internal/cache"
)

// BlackbirdClient drives GitHub's web code search (the "Blackbird" engine —
// symbol:/is:/NOT/regex syntax and full repository coverage). The fast path
// is a plain authenticated HTTP GET (Accept: application/json) carrying the
// session cookie pulled once from the go-wowa browser profile — ~0.3s vs ~7s
// for an in-page evaluate. The go-wowa chrome_interact seam remains as:
//   - the credential provider (get_cookies on init + on logged_in:false), and
//   - the fallback transport when GitHub rejects the bare-cookie request.
//
// Disabled unless GO_WOWA_BASE_URL is configured.
type BlackbirdClient struct {
	wowaURL string
	session string
	secret  string
	http    *http.Client // go-wowa control plane
	ghHTTP  *http.Client // github.com data plane (no redirect following)
	ghBase  string       // default https://github.com — injectable in tests
	cache   *kitcache.Cache

	cookieHeader string // lazily pulled from the browser profile
	userAgent    string

	mu       sync.Mutex // serializes calls — protects the session from bursts
	lastCall time.Time
}

const (
	bbDefaultSession  = "github-search"
	bbGitHubBase      = "https://github.com"
	bbTimeout         = 45 * time.Second
	bbHTTPTimeout     = 20 * time.Second
	bbMinInterval     = 2 * time.Second // rate floor — protects the session from burst-triggered captchas
	bbSearchCacheTTL  = 15 * time.Minute
	bbDefaultPageSize = 10 // GitHub code search page size
)

// BlackbirdOption configures a BlackbirdClient.
type BlackbirdOption func(*BlackbirdClient)

// WithBlackbirdCache sets the cache used by BlackbirdClient.
func WithBlackbirdCache(c *kitcache.Cache) BlackbirdOption {
	return func(b *BlackbirdClient) { b.cache = c }
}

// NewBlackbirdClient creates a client for GitHub's web search via go-wowa.
// Empty session defaults to "github-search"; secret is sent as
// X-Internal-Secret when non-empty (go-wowa soft-auth middleware).
func NewBlackbirdClient(wowaURL, session, secret string, opts ...BlackbirdOption) *BlackbirdClient {
	wowaURL = strings.TrimSuffix(strings.TrimSpace(wowaURL), "/")
	if wowaURL == "" {
		return nil
	}
	if session == "" {
		session = bbDefaultSession
	}
	b := &BlackbirdClient{
		wowaURL: wowaURL,
		session: session,
		secret:  secret,
		http:    &http.Client{Timeout: bbTimeout},
		ghBase:  bbGitHubBase,
		ghHTTP: &http.Client{
			Timeout: bbHTTPTimeout,
			// Never follow redirects — a 3xx to /login is the logged-out
			// signal; following it would mask the state as a 200 HTML page.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// WithBlackbird attaches a Blackbird client used for engine=blackbird
// searches and engine=auto last-resort coverage.
func WithBlackbird(b *BlackbirdClient) GitHubForgeOption {
	return func(g *GitHubForge) { g.bb = b }
}

// errBlackbirdLoggedOut is returned when the browser session lost its
// github.com login — an operational state fixed by re-login over VNC, not a
// code defect.
var errBlackbirdLoggedOut = errors.New("github web session not logged in — re-login via go-wowa VNC")

// bbFetchResult is what the in-page evaluate script returns.
type bbFetchResult struct {
	Status    int    `json:"status"`
	Payload   string `json:"payload,omitempty"` // blackbirdSearchRoute JSON text
	Error     string `json:"error,omitempty"`
	LandedURL string `json:"landed_url,omitempty"`
}

// bbRoute is the payload.blackbirdSearchRoute portion of the embedded data.
// Field names verified against a live capture (github.com/search?type=code).
type bbRoute struct {
	Results     []bbCodeResult `json:"results"`
	ResultCount int            `json:"result_count"`
	PageCount   int            `json:"page_count"`
	Page        int            `json:"page"`
	LoggedIn    *bool          `json:"logged_in"`
	WarnLimited bool           `json:"warn_limited_results"`
	Errors      []struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"errors"`
}

// bbCodeResult is one code-search hit inside blackbirdSearchRoute.results.
type bbCodeResult struct {
	RepoNWO        string      `json:"repo_nwo"` // flat "owner/name"
	Path           string      `json:"path"`
	LanguageName   string      `json:"language_name"`
	RefName        string      `json:"ref_name"` // "refs/heads/main"
	CommitSHA      string      `json:"commit_sha"`
	BlobSHA        string      `json:"blob_sha"`
	LineNumber     int         `json:"line_number"` // primary match line
	RepoIsArchived bool        `json:"repo_is_archived"`
	Snippets       []bbSnippet `json:"snippets"`
	MatchedSymbols []bbSymbol  `json:"matched_symbols"`
}

// bbSnippet is one highlighted fragment group. Lines[i] is the HTML-marked-up
// source of file line StartingLine+i; <mark> tags wrap matched terms.
type bbSnippet struct {
	Format       string   `json:"format"` // SNIPPET_FORMAT_HTML
	Lines        []string `json:"lines"`
	StartingLine int      `json:"starting_line_number"`
	EndingLine   int      `json:"ending_line_number"`
	JumpToLine   int      `json:"jump_to_line_number"`
}

// bbSymbol is a symbol-search hit's metadata (populated for symbol: queries).
type bbSymbol struct {
	FullyQualifiedName string `json:"fully_qualified_name"`
	Kind               string `json:"kind"` // e.g. SYMBOL_KIND_METHOD_DEF
}

// searchCodeBlackbird runs a code search through the github.com web UI.
// maxKeep bounds returned results; context expansion reuses the GitHub
// contents machinery.
func (g *GitHubForge) searchCodeBlackbird(ctx context.Context, query string, repos []string, opt SearchCodeOptions, maxKeep int) (CodeSearchResult, error) {
	if g.bb == nil {
		return CodeSearchResult{}, errors.New("blackbird engine not configured")
	}

	q := buildBlackbirdQuery(query, repos, opt)

	key := cache.Key("bb:code:search", q, strconv.Itoa(maxKeep), strconv.Itoa(opt.MaxResults), strconv.Itoa(opt.MinStars), strconv.Itoa(opt.ContextLines), strconv.Itoa(opt.ContextResults))
	result, err := cacheGetOrLoadJSONWithTTL(g.bb.cache, ctx, key, bbSearchCacheTTL, func(ctx context.Context) (CodeSearchResult, error) {
		res, err := g.bb.collectBlackbirdResults(ctx, q, maxKeep, opt.MaxResults)
		if err != nil {
			return CodeSearchResult{}, err
		}
		if opt.MinStars > 0 {
			filtered, ferr := g.filterByMinStars(ctx, res.Results, opt.MinStars)
			if ferr == nil {
				res.Results = filtered
				res.Total = len(filtered)
			}
		}
		g.expandCodeSearchContext(ctx, res.Results, searchQueryTerms(query), opt.ContextLines, opt.ContextResults)
		return res, nil
	})
	if err != nil {
		return CodeSearchResult{}, err
	}
	result.Query = q
	return result, nil
}

// buildBlackbirdQuery maps the tool's structured filters onto blackbird
// syntax. The raw query text passes through verbatim — symbol:, is:, NOT,
// parens and /regex/ are exactly why this engine exists.
func buildBlackbirdQuery(query string, repos []string, opt SearchCodeOptions) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(query))
	for _, repo := range repos {
		fmt.Fprintf(&b, " repo:%s", repo)
	}
	if opt.Language != "" {
		fmt.Fprintf(&b, " lang:%s", opt.Language)
	}
	for _, ext := range opt.FileExtensions {
		ext = strings.TrimPrefix(strings.TrimSpace(ext), ".")
		if ext != "" {
			fmt.Fprintf(&b, " path:/\\.%s$/", regexp.QuoteMeta(ext))
		}
	}
	for _, repo := range opt.ExcludeRepos {
		fmt.Fprintf(&b, " -repo:%s", repo)
	}
	for _, p := range opt.ExcludePaths {
		fmt.Fprintf(&b, " -path:%s", p)
	}
	return strings.TrimSpace(b.String())
}

// collectBlackbirdResults fetches search pages until maxKeep is filled or
// results run out.
func (b *BlackbirdClient) collectBlackbirdResults(ctx context.Context, q string, maxKeep, maxResults int) (CodeSearchResult, error) {
	want := maxKeep
	if want <= 0 {
		want = maxResults
	}

	var out CodeSearchResult
	seen := make(map[string]struct{})
	for page := 1; ; page++ {
		if ctx.Err() != nil {
			return CodeSearchResult{}, ctx.Err()
		}
		route, err := b.fetchSearchPage(ctx, q, page)
		if err != nil {
			return CodeSearchResult{}, err
		}
		if out.Total == 0 {
			out.Total = route.ResultCount
		}
		if len(route.Results) == 0 {
			break
		}
		for _, item := range route.Results {
			r, ok := convertBBResult(item)
			if !ok {
				continue
			}
			k := r.Repo + "\x00" + r.Path
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			out.Results = append(out.Results, r)
			if want > 0 && len(out.Results) >= want {
				return out, nil
			}
		}
		if page >= route.PageCount {
			break
		}
	}
	return out, nil
}

// fetchSearchPage returns one blackbird search page. Order of attempts:
//  1. plain HTTP with the cached session cookie (fast path),
//  2. on logged_out: refresh the cookie from the browser profile, retry once,
//  3. on transport-level rejection: in-page evaluate via go-wowa (the tab is a
//     real browser — GitHub fingerprints differently there).
func (b *BlackbirdClient) fetchSearchPage(ctx context.Context, q string, page int) (bbRoute, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if d := time.Since(b.lastCall); d < bbMinInterval {
		select {
		case <-ctx.Done():
			return bbRoute{}, ctx.Err()
		case <-time.After(bbMinInterval - d):
		}
	}
	b.lastCall = time.Now()

	if b.cookieHeader == "" {
		if err := b.pullCookies(ctx); err != nil {
			return bbRoute{}, err
		}
	}

	searchURL := b.searchURL(q, page)
	route, err := b.fetchSearchHTTP(ctx, searchURL)
	if errors.Is(err, errBlackbirdLoggedOut) {
		// Cookie may be stale — refresh it once from the profile, retry once.
		if cerr := b.pullCookies(ctx); cerr == nil {
			route, err = b.fetchSearchHTTP(ctx, searchURL)
		}
		if errors.Is(err, errBlackbirdLoggedOut) {
			return bbRoute{}, err
		}
	}
	if err != nil {
		// Plain HTTP got fingerprinted/rejected — fall back to the real
		// browser tab, which carries TLS fingerprint + full cookie jar.
		return b.fetchSearchPageWowa(ctx, searchURL)
	}
	return route, nil
}

func (b *BlackbirdClient) searchURL(q string, page int) string {
	u := b.ghBase + "/search?q=" + urlQueryEscape(q) + "&type=code"
	if page > 1 {
		u += "&p=" + strconv.Itoa(page)
	}
	return u
}

// bbCookie is one cookie object returned by the get_cookies action.
type bbCookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	HTTPOnly bool   `json:"http_only"`
}

// pullCookies reads the github.com cookie jar + User-Agent from the browser
// profile via one get_cookies + evaluate pair, and caches the Cookie header.
func (b *BlackbirdClient) pullCookies(ctx context.Context) error {
	raws, err := b.interactActions(ctx, b.ghBase+"/", []map[string]any{
		{"type": "get_cookies"},
		{"type": "evaluate", "script": "navigator.userAgent"},
	})
	if err != nil {
		return err
	}
	for _, raw := range raws {
		var cookies []bbCookie
		if err := json.Unmarshal(raw, &cookies); err == nil && len(cookies) > 0 {
			var sb strings.Builder
			for _, c := range cookies {
				if !strings.Contains(c.Domain, "github.com") || c.Name == "" {
					continue
				}
				if sb.Len() > 0 {
					sb.WriteString("; ")
				}
				sb.WriteString(c.Name)
				sb.WriteByte('=')
				sb.WriteString(c.Value)
			}
			if s := sb.String(); s != "" {
				b.cookieHeader = s
			}
			continue
		}
		var ua string
		if err := json.Unmarshal(raw, &ua); err == nil && strings.Contains(ua, "Mozilla") {
			b.userAgent = ua
		}
	}
	if b.cookieHeader == "" {
		return errors.New("no github.com cookies in browser profile")
	}
	return nil
}

// fetchSearchHTTP performs the bare-cookie GET. Accept: application/json makes
// github.com/search return the blackbirdSearchRoute payload as JSON directly —
// no HTML scraping. A 3xx or logged_in:false maps to errBlackbirdLoggedOut.
func (b *BlackbirdClient) fetchSearchHTTP(ctx context.Context, searchURL string) (bbRoute, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return bbRoute{}, fmt.Errorf("build blackbird request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if b.userAgent != "" {
		req.Header.Set("User-Agent", b.userAgent)
	}
	if b.cookieHeader != "" {
		req.Header.Set("Cookie", b.cookieHeader)
	}

	resp, err := b.ghHTTP.Do(req)
	if err != nil {
		return bbRoute{}, fmt.Errorf("blackbird http: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		if loc := resp.Header.Get("Location"); strings.Contains(loc, "/login") {
			return bbRoute{}, errBlackbirdLoggedOut
		}
	}
	if resp.StatusCode != http.StatusOK {
		return bbRoute{}, fmt.Errorf("blackbird search returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return bbRoute{}, fmt.Errorf("read blackbird response: %w", err)
	}
	route, err := decodeBBPayload(body)
	if err != nil {
		return bbRoute{}, err
	}
	if route.LoggedIn != nil && !*route.LoggedIn {
		return bbRoute{}, errBlackbirdLoggedOut
	}
	if route.WarnLimited {
		return bbRoute{}, errors.New("blackbird search rate-limited (warn_limited_results)")
	}
	return route, nil
}

// decodeBBPayload parses a search response body: JSON when
// Accept: application/json was honored, else the embeddedData script embedded
// in the HTML page.
func decodeBBPayload(body []byte) (bbRoute, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return bbRoute{}, errors.New("blackbird search returned empty body")
	}
	var payload json.RawMessage
	if trimmed[0] == '{' {
		var j struct {
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(trimmed, &j); err != nil {
			return bbRoute{}, fmt.Errorf("decode blackbird json: %w", err)
		}
		payload = j.Payload
	} else {
		m := bbEmbeddedDataRe.FindSubmatch(body)
		if m == nil {
			return bbRoute{}, errors.New("no embeddedData in blackbird html response")
		}
		var j struct {
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(m[1], &j); err != nil {
			return bbRoute{}, fmt.Errorf("decode blackbird embeddedData: %w", err)
		}
		payload = j.Payload
	}
	var p struct {
		Route bbRoute `json:"blackbirdSearchRoute"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return bbRoute{}, fmt.Errorf("decode blackbird route: %w", err)
	}
	return p.Route, nil
}

var bbEmbeddedDataRe = regexp.MustCompile(
	`<script type="application/json" data-target="react-app\.embeddedData">(.*?)</script>`)

// fetchSearchPageWowa navigates the pinned tab to the search URL and extracts
// the embedded payload in-page — the fallback transport for when bare-cookie
// HTTP gets fingerprinted.
func (b *BlackbirdClient) fetchSearchPageWowa(ctx context.Context, searchURL string) (bbRoute, error) {
	script := `(() => {
  const el = document.querySelector('script[type="application/json"][data-target="react-app.embeddedData"]');
  if (!el) return {status: 0, error: "embeddedData script not found", landed_url: location.href};
  let j;
  try { j = JSON.parse(el.textContent); } catch (e) { return {status: 0, error: "embeddedData parse: " + e, landed_url: location.href}; }
  const route = ((j && j.payload) || {}).blackbirdSearchRoute || {};
  return {status: 200, landed_url: location.href, payload: JSON.stringify(route)};
})()`

	fr, err := b.interactEvaluate(ctx, searchURL, script)
	if err != nil {
		return bbRoute{}, err
	}
	if strings.Contains(fr.LandedURL, "/login") {
		return bbRoute{}, errBlackbirdLoggedOut
	}
	if fr.Status != http.StatusOK {
		return bbRoute{}, fmt.Errorf("blackbird search returned %d (%s)", fr.Status, fr.Error)
	}
	if fr.Payload == "" {
		return bbRoute{}, errors.New("blackbird search returned empty payload")
	}

	var route bbRoute
	if err := json.Unmarshal([]byte(fr.Payload), &route); err != nil {
		return bbRoute{}, fmt.Errorf("decode blackbird payload: %w", err)
	}
	// logged_in travels inside the route object — an explicit false means the
	// session cookie died; absent (nil) means the field moved, treat as live.
	if route.LoggedIn != nil && !*route.LoggedIn {
		return bbRoute{}, errBlackbirdLoggedOut
	}
	if route.WarnLimited {
		return bbRoute{}, errors.New("blackbird search rate-limited (warn_limited_results)")
	}
	return route, nil
}

// wowaInteractResponse is the go-wowa /api/v1/chrome/interact envelope.
type wowaInteractResponse struct {
	Status  string `json:"status"`
	Actions []struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	} `json:"actions"`
	ErrorCode string `json:"error_code,omitempty"`
}

// interactEvaluate issues one chrome_interact call: navigate to url, run the
// evaluate script, return its data. Callers must hold b.mu (fetchSearchPage
// serializes all session traffic).
func (b *BlackbirdClient) interactEvaluate(ctx context.Context, url, script string) (bbFetchResult, error) {
	raws, err := b.interactActions(ctx, url, []map[string]any{
		{"type": "evaluate", "script": script},
	})
	if err != nil {
		return bbFetchResult{}, err
	}
	for _, raw := range raws {
		var fr bbFetchResult
		if err := json.Unmarshal(raw, &fr); err == nil {
			return fr, nil
		}
	}
	return bbFetchResult{}, errors.New("wowa evaluate returned no data")
}

// interactActions POSTs one chrome_interact request and returns the ok
// action payloads in order. Caller must hold b.mu.
func (b *BlackbirdClient) interactActions(ctx context.Context, url string, actions []map[string]any) ([]json.RawMessage, error) {
	body, err := json.Marshal(map[string]any{
		"url":     url,
		"mode":    "default", // mandatory: empty/unrecognized => incognito context, empty cookie jar
		"session": b.session,
		"actions": actions,
	})
	if err != nil {
		return nil, fmt.Errorf("build interact request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.wowaURL+"/api/v1/chrome/interact", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build wowa request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if b.secret != "" {
		req.Header.Set("X-Internal-Secret", b.secret)
	}

	resp, err := b.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wowa interact: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wowa interact returned %d", resp.StatusCode)
	}

	var wr wowaInteractResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return nil, fmt.Errorf("decode wowa response: %w", err)
	}
	if wr.Status != "ok" {
		return nil, fmt.Errorf("wowa interact failed: %s", wr.ErrorCode)
	}
	out := make([]json.RawMessage, 0, len(wr.Actions))
	for _, a := range wr.Actions {
		if a.OK && len(a.Data) > 0 {
			out = append(out, a.Data)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("wowa interact returned no action data")
	}
	return out, nil
}

var (
	bbAnyTag     = regexp.MustCompile(`<[^>]*>`)
	bbMarkedText = regexp.MustCompile(`<mark>(.*?)</mark>`)
)

// bbStripHTML removes tags and unescapes entities from a blackbird snippet
// line, recovering the verbatim source text.
func bbStripHTML(s string) string {
	return html.UnescapeString(bbAnyTag.ReplaceAllString(s, ""))
}

// convertBBResult maps a blackbird code result into a CodeResult.
func convertBBResult(item bbCodeResult) (CodeResult, bool) {
	repo := item.RepoNWO
	if repo == "" || item.Path == "" {
		return CodeResult{}, false
	}

	r := CodeResult{
		Path:   item.Path,
		Repo:   repo,
		Engine: "blackbird",
		Commit: item.CommitSHA,
	}
	if item.LineNumber > 0 {
		r.Lines = appendLine(r.Lines, item.LineNumber)
	}
	for _, s := range item.MatchedSymbols {
		if s.FullyQualifiedName != "" {
			r.Matched = appendUnique(r.Matched, s.FullyQualifiedName)
		}
	}

	for _, sn := range item.Snippets {
		var frag strings.Builder
		for i, raw := range sn.Lines {
			lineNo := sn.StartingLine + i
			for _, mm := range bbMarkedText.FindAllStringSubmatch(raw, -1) {
				if t := strings.TrimSpace(bbStripHTML(mm[1])); t != "" {
					r.Matched = appendUnique(r.Matched, t)
				}
			}
			clean := strings.TrimSpace(bbStripHTML(raw))
			if clean != "" {
				frag.WriteString(clean)
				frag.WriteByte('\n')
			}
			if strings.Contains(raw, "<mark>") && lineNo > 0 {
				r.Lines = appendLine(r.Lines, lineNo)
			}
		}
		content := strings.TrimSpace(frag.String())
		if content == "" {
			continue
		}
		if r.rawFrag == "" {
			r.rawFrag = content
		}
		r.Content = joinFragment(r.Content, content)
	}
	if r.Content == "" {
		r.Content = "File: " + item.Path
	}

	ref := item.CommitSHA
	if ref == "" {
		ref = strings.TrimPrefix(item.RefName, "refs/heads/")
	}
	if ref == "" {
		ref = "HEAD"
	}
	r.URL = "https://github.com/" + repo + "/blob/" + ref + "/" + item.Path
	if len(r.Lines) > 0 {
		r.URL += "#L" + strconv.Itoa(r.Lines[0])
	}
	return r, true
}
