package main

import (
	"errors"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/webanalyze"
)

func strptr(s string) *string { return &s }

// benignAnalyzeFixture exercises most sections of the site_analyze detect
// formatter with values that contain NO XML-hostile characters, so the
// pre-migration hand-rolled output is itself well-formed and decodable.
// Maps (ImageFormats) carry a single entry so element order is deterministic.
func benignAnalyzeFixture() *webanalyze.AnalyzeResponse {
	return &webanalyze.AnalyzeResponse{
		URL:    "https://example.com",
		Status: 200,
		Technologies: []webanalyze.Technology{
			{Name: "React", Categories: []string{"JavaScript frameworks"}, Confidence: 100, Version: strptr("18.2.0")},
			{Name: "nginx", Categories: []string{"Web servers"}, Confidence: 90},
		},
		Meta:   webanalyze.Meta{Generator: "Next.js", Server: "nginx", Title: "Example Site"},
		Assets: webanalyze.Assets{Scripts: []string{"a.js", "b.js"}, Stylesheets: []string{"a.css"}},
		SEO: webanalyze.SeoReport{
			Score:       80,
			OG:          webanalyze.OGTags{Title: "OG Title", Description: "OG Desc", Image: "https://example.com/og.png", Type: "website"},
			Twitter:     webanalyze.TwitterCard{Card: "summary", Site: "@example"},
			Canonical:   strptr("https://example.com/"),
			Description: "A friendly example site",
			JsonLD:      []webanalyze.JsonLDEntry{{SchemaType: "Organization"}},
			Hreflang:    []webanalyze.HreflangEntry{{Lang: "en", Href: "https://example.com/en"}},
			Robots:      "index, follow",
		},
		Performance: webanalyze.PerformanceReport{
			Compression:      "gzip",
			CacheControl:     "max-age=3600",
			HTTP3Supported:   true,
			Preload:          []webanalyze.ResourceHint{{Href: "x", AsType: "script"}},
			Preconnect:       []string{"https://cdn.example.com"},
			ImagesTotal:      10,
			ImagesLazy:       6,
			InlineStyleCount: 2,
			InlineStyleBytes: 512,
		},
		Accessibility: webanalyze.AccessibilityReport{
			Lang:            "en",
			ImagesWithAlt:   8,
			ImagesEmptyAlt:  1,
			ImagesNoAlt:     1,
			H1Count:         1,
			HeadingSkip:     false,
			Landmarks:       5,
			InputsTotal:     3,
			InputsWithLabel: 3,
			Score:           92,
		},
		Content: webanalyze.ContentReport{
			InternalLinks:   20,
			ExternalLinks:   4,
			ExternalDomains: []string{"github.com"},
			WordCount:       1200,
			Iframes:         []webanalyze.IframeInfo{{Src: "https://youtube.com/embed/x", Platform: "youtube"}},
		},
		Media: webanalyze.MediaReport{
			ImagesTotal:  10,
			ImageFormats: map[string]int{"webp": 7},
			SrcsetCount:  5,
			PictureCount: 2,
			ImageCDNs:    []string{"cloudflare"},
			Videos:       []webanalyze.VideoInfo{{Src: "https://vimeo.com/1", Platform: "vimeo"}},
			Audio:        []webanalyze.AudioInfo{{Src: "https://example.com/a.mp3", Platform: "html5"}},
		},
		Fonts: webanalyze.FontsReport{
			GoogleFonts:   []string{"Inter"},
			AdobeFonts:    true,
			FontFaceCount: 3,
			FontFamilies:  []string{"Inter", "Roboto"},
		},
		PWA: webanalyze.PwaReport{ManifestURL: "https://example.com/manifest.json", HasServiceWorker: true, ThemeColor: "black", IsPWA: true},
		API: webanalyze.ApiReport{
			Endpoints:       []webanalyze.ApiEndpoint{{URL: "https://example.com/api", Method: "GET", Source: "fetch"}},
			GraphQLDetected: true,
			NextData:        true,
			NuxtData:        true,
			FormActions:     []string{"https://example.com/submit"},
			WebSocketURLs:   []string{"wss://example.com/ws"},
		},
	}
}

// hostileAnalyzeFixture puts XML-hostile characters where the prior formatter
// used %q on an attribute -- notably <meta title>, which routinely holds a
// marketing title with ampersands and quotes. The pre-migration output is
// MALFORMED XML.
func hostileAnalyzeFixture() *webanalyze.AnalyzeResponse {
	return &webanalyze.AnalyzeResponse{
		URL:    "https://shop.example.com",
		Status: 200,
		Meta:   webanalyze.Meta{Generator: "WooCommerce", Server: "Apache", Title: `Tom & Jerry "Show" <b>`},
	}
}

// benignCrawlFixture exercises the site_crawl formatter with one success page
// (markdown -> CDATA) and one error page, all benign.
func benignCrawlFixture() *webanalyze.CrawlResponse {
	return &webanalyze.CrawlResponse{
		Summary: webanalyze.CrawlSummary{PagesCrawled: 2, Errors: 1, ElapsedMs: 1234},
		Pages: []webanalyze.CrawlPage{
			{URL: "https://example.com/", Status: 200, Depth: 0, Title: "Home", Markdown: "# Home\n\nWelcome", ContentLength: 42, LinksFound: 5},
			{URL: "https://example.com/broken", Depth: 1, Error: strptr("timeout")},
		},
	}
}

// TestSiteAnalyze_DetectStructurallyEquivalentToBaseline proves the migrated
// (xml.Marshal) detect output is structurally identical to the recorded
// pre-migration output across every section.
func TestSiteAnalyze_DetectStructurallyEquivalentToBaseline(t *testing.T) {
	current := readGolden(t, "site_detect_benign_current.xml")
	migrated := formatDetectResponse(benignAnalyzeFixture())
	assertXMLEquivalent(t, current, migrated)
}

// TestSiteAnalyze_BaselineHostileMetaIsMalformed documents bug #1: the
// hand-rolled formatter emitted malformed XML for a <meta title> carrying
// ampersands, quotes and angle brackets (it used %q, not XML escaping).
func TestSiteAnalyze_BaselineHostileMetaIsMalformed(t *testing.T) {
	assertNotWellFormed(t, readGolden(t, "site_detect_hostile_current.xml"))
}

// TestSiteAnalyze_HostileMetaEscaped proves bug #1 is fixed: the migrated
// output is well-formed and the <meta title> attribute round-trips to its exact
// original value.
func TestSiteAnalyze_HostileMetaEscaped(t *testing.T) {
	migrated := formatDetectResponse(hostileAnalyzeFixture())
	assertAttrRoundTrips(t, migrated, "response/site/meta", "title", `Tom & Jerry "Show" <b>`)
}

// TestSiteCrawl_StructurallyEquivalentToBaseline proves the migrated site_crawl
// output (including the two page shapes and the CDATA content) is structurally
// identical to the recorded pre-migration output.
func TestSiteCrawl_StructurallyEquivalentToBaseline(t *testing.T) {
	current := readGolden(t, "site_crawl_benign_current.xml")
	migrated := formatCrawlResponse(benignCrawlFixture())
	assertXMLEquivalent(t, current, migrated)
}

// benignFullSuccessOutput / benignFullErrorOutput drive the full-mode formatter
// (migrated in #262 but never given equivalence coverage -- the gap this
// increment closes). Both reuse benignAnalyzeFixture so the <site> head is the
// same buildSiteHead output the detect golden already validates; the goldens
// (site_full_*_benign.xml) are derived from the pre-migration detect golden's
// head plus the verified pre-migration <sources>/<hint> tail, so they carry the
// same pre-migration authority as the detect baseline. A single-entry Languages
// map keeps the success-branch output deterministic.
func benignFullSuccessOutput() string {
	return formatFullResponse(
		benignAnalyzeFixture(),
		"/tmp/sites/example.com",
		&webanalyze.SourceStats{Files: 12, Languages: map[string]int{"ts": 12}},
		nil,
	)
}

func benignFullErrorOutput() string {
	// stats == nil takes the else branch -> <sources files="0" reason=...>.
	return formatFullResponse(benignAnalyzeFixture(), "/tmp/sites/example.com", nil, nil)
}

// TestSiteAnalyze_FullStructurallyEquivalentToBaseline proves the migrated
// full-mode output is structurally identical to the recorded baseline across
// both <sources> branches: the success branch (path + <language> + <hint>) and
// the error branch (files="0" + reason, no <hint>).
func TestSiteAnalyze_FullStructurallyEquivalentToBaseline(t *testing.T) {
	t.Run("success_sources", func(t *testing.T) {
		assertXMLEquivalent(t, readGolden(t, "site_full_success_benign.xml"), benignFullSuccessOutput())
	})
	t.Run("error_reason", func(t *testing.T) {
		assertXMLEquivalent(t, readGolden(t, "site_full_error_benign.xml"), benignFullErrorOutput())
	})
}

// TestSiteAnalyze_FullHostileEscaped proves escaping-by-construction on both
// full-mode <sources> branches: the pre-migration formatter used %q for the
// reason/path attributes, so a hostile extract error or source path would have
// been malformed; the migrated attributes round-trip to their exact values.
func TestSiteAnalyze_FullHostileEscaped(t *testing.T) {
	errOut := formatFullResponse(benignAnalyzeFixture(), "", nil, errors.New(`extract failed: bad & <path> "x"`))
	assertAttrRoundTrips(t, errOut, "response/site/sources", "reason", `extract failed: bad & <path> "x"`)

	okOut := formatFullResponse(
		benignAnalyzeFixture(),
		`/tmp/a & b "x"`,
		&webanalyze.SourceStats{Files: 3, Languages: map[string]int{"go": 3}},
		nil,
	)
	assertAttrRoundTrips(t, okOut, "response/site/sources", "path", `/tmp/a & b "x"`)
}
