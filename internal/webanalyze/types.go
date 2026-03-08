package webanalyze

// SeoReport holds SEO analysis data from ox-browser.
type SeoReport struct {
	Score       int             `json:"score"`
	OG          OGTags          `json:"og"`
	Twitter     TwitterCard     `json:"twitter"`
	JsonLD      []JsonLDEntry   `json:"json_ld"`
	Canonical   *string         `json:"canonical,omitempty"`
	Hreflang    []HreflangEntry `json:"hreflang"`
	Robots      string          `json:"robots"`
	Description string          `json:"description"`
	Keywords    string          `json:"keywords"`
	Favicon     *string         `json:"favicon,omitempty"`
}

// OGTags holds Open Graph metadata.
type OGTags struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Type        string `json:"og_type"`
	URL         string `json:"url"`
	SiteName    string `json:"site_name"`
}

// TwitterCard holds Twitter Card metadata.
type TwitterCard struct {
	Card        string `json:"card"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Site        string `json:"site"`
}

// JsonLDEntry is a single JSON-LD block.
type JsonLDEntry struct {
	SchemaType string `json:"schema_type"`
	Raw        string `json:"raw"`
}

// HreflangEntry is a language alternative.
type HreflangEntry struct {
	Lang string `json:"lang"`
	Href string `json:"href"`
}

// PerformanceReport holds performance hints from ox-browser.
type PerformanceReport struct {
	Compression      string         `json:"compression"`
	CacheControl     string         `json:"cache_control"`
	ETag             string         `json:"etag"`
	Expires          string         `json:"expires"`
	Age              string         `json:"age"`
	HTTP3Supported   bool           `json:"http3_supported"`
	Preload          []ResourceHint `json:"preload"`
	Prefetch         []ResourceHint `json:"prefetch"`
	Preconnect       []string       `json:"preconnect"`
	ImagesTotal      int            `json:"images_total"`
	ImagesLazy       int            `json:"images_lazy"`
	InlineStyleCount int            `json:"inline_styles_count"`
	InlineStyleBytes int            `json:"inline_styles_bytes"`
}

// ResourceHint is a preload/prefetch entry.
type ResourceHint struct {
	Href   string `json:"href"`
	AsType string `json:"as_type"`
}

// AccessibilityReport holds accessibility audit data from ox-browser.
type AccessibilityReport struct {
	Lang            string        `json:"lang"`
	ImagesWithAlt   int           `json:"images_with_alt"`
	ImagesEmptyAlt  int           `json:"images_empty_alt"`
	ImagesNoAlt     int           `json:"images_no_alt"`
	H1Count         int           `json:"h1_count"`
	Headings        []HeadingInfo `json:"headings"`
	HeadingSkip     bool          `json:"heading_skip"`
	Landmarks       int           `json:"landmarks"`
	InputsTotal     int           `json:"inputs_total"`
	InputsWithLabel int           `json:"inputs_with_label"`
	Score           int           `json:"score"`
}

// HeadingInfo is a heading element in the document.
type HeadingInfo struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

// ContentReport holds content analysis data from ox-browser.
type ContentReport struct {
	InternalLinks   int          `json:"internal_links"`
	ExternalLinks   int          `json:"external_links"`
	ExternalDomains []string     `json:"external_domains"`
	WordCount       int          `json:"word_count"`
	Iframes         []IframeInfo `json:"iframes"`
}

// IframeInfo is an embedded iframe.
type IframeInfo struct {
	Src      string `json:"src"`
	Platform string `json:"platform"`
}

// MediaReport holds media analysis data from ox-browser.
type MediaReport struct {
	ImagesTotal  int            `json:"images_total"`
	ImageFormats map[string]int `json:"image_formats"`
	SrcsetCount  int            `json:"srcset_count"`
	PictureCount int            `json:"picture_count"`
	ImageCDNs    []string       `json:"image_cdns"`
	Videos       []VideoInfo    `json:"videos"`
	Audio        []AudioInfo    `json:"audio"`
}

// VideoInfo is a video element.
type VideoInfo struct {
	Src      string `json:"src"`
	Platform string `json:"platform"`
}

// AudioInfo is an audio element.
type AudioInfo struct {
	Src      string `json:"src"`
	Platform string `json:"platform"`
}

// FontsReport holds font analysis data from ox-browser.
type FontsReport struct {
	GoogleFonts   []string `json:"google_fonts"`
	AdobeFonts    bool     `json:"adobe_fonts"`
	FontFaceCount int      `json:"font_face_count"`
	FontFamilies  []string `json:"font_families"`
}

// PwaReport holds PWA detection data from ox-browser.
type PwaReport struct {
	ManifestURL      string `json:"manifest_url"`
	HasServiceWorker bool   `json:"has_service_worker"`
	ThemeColor       string `json:"theme_color"`
	AppleTouchIcon   string `json:"apple_touch_icon"`
	IsPWA            bool   `json:"is_pwa"`
}

// ApiReport holds API discovery data from ox-browser.
type ApiReport struct {
	Endpoints       []ApiEndpoint `json:"endpoints"`
	GraphQLDetected bool          `json:"graphql_detected"`
	NextData        bool          `json:"next_data"`
	NuxtData        bool          `json:"nuxt_data"`
	FormActions     []string      `json:"form_actions"`
	WebSocketURLs   []string      `json:"websocket_urls"`
}

// ApiEndpoint is a discovered API endpoint.
type ApiEndpoint struct {
	URL    string `json:"url"`
	Method string `json:"method"`
	Source string `json:"source"`
}

// CrawlPage is a single page result from the crawler SSE stream.
type CrawlPage struct {
	URL           string  `json:"url"`
	Status        uint16  `json:"status"`
	Depth         uint32  `json:"depth"`
	Title         string  `json:"title"`
	Markdown      string  `json:"markdown"`
	ContentLength int     `json:"content_length"`
	LinksFound    int     `json:"links_found"`
	ElapsedMs     uint64  `json:"elapsed_ms"`
	Error         *string `json:"error,omitempty"`
}

// CrawlSummary is the final SSE event from the crawler.
type CrawlSummary struct {
	PagesCrawled int    `json:"pages_crawled"`
	Errors       int    `json:"errors"`
	ElapsedMs    uint64 `json:"elapsed_ms"`
}

// CrawlResponse aggregates all pages and the summary from a crawl.
type CrawlResponse struct {
	Pages   []CrawlPage  `json:"pages"`
	Summary CrawlSummary `json:"summary"`
}
