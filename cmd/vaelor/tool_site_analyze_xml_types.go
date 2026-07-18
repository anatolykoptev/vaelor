package main

import "encoding/xml"

// ---- site_analyze / site_crawl XML types ----
//
// Migrated from hand-rolled fmt.Fprintf formatters onto encoding/xml.Marshal so
// escaping and well-formedness are correct BY CONSTRUCTION. The prior
// formatters used %q for every attribute (Go quoting, NOT XML escaping) and raw
// %s for several text nodes, silently producing malformed XML whenever a value
// carried <, & or " -- e.g. the <meta generator=.. server=.. title=..>
// attributes (pr-review-council #260/#261), which routinely hold marketing
// titles with ampersands and quotes.
//
// Empty elements serialize as long-form <x></x> (xml.Marshal never self-closes),
// which is decoder-equivalent to the prior <x/> and consistent with the
// repo_analyze/code_compare seam.

// ---- site_analyze ----

type siteAnalyzeRespXML struct {
	XMLName xml.Name `xml:"response"`
	Tool    string   `xml:"tool,attr"`
	Site    siteXML  `xml:"site"`
}

type siteXML struct {
	URL           string     `xml:"url,attr"`
	Status        int        `xml:"status,attr"`
	Technologies  techsXML   `xml:"technologies"`
	SEO           seoXML     `xml:"seo"`
	Performance   perfXML    `xml:"performance"`
	Accessibility a11yXML    `xml:"accessibility"`
	Content       contentXML `xml:"content"`
	Media         mediaXML   `xml:"media"`
	Fonts         *fontsXML  `xml:"fonts,omitempty"`
	PWA           *pwaXML    `xml:"pwa,omitempty"`
	API           *apiXML    `xml:"api_discovery,omitempty"`
	// detect-mode tail:
	Meta   *metaXML   `xml:"meta,omitempty"`
	Assets *assetsXML `xml:"assets,omitempty"`
	// full-mode tail:
	Sources *sourcesXML `xml:"sources,omitempty"`
	Hint    string      `xml:"hint,omitempty"`
}

type techsXML struct {
	Count int       `xml:"count,attr"`
	Items []techXML `xml:"tech"`
}

type techXML struct {
	Category   string  `xml:"category,attr"`
	Name       string  `xml:"name,attr"`
	Confidence int     `xml:"confidence,attr"`
	Version    *string `xml:"version,attr,omitempty"`
}

type seoXML struct {
	Score       int           `xml:"score,attr"`
	OG          *ogXML        `xml:"og,omitempty"`
	Twitter     *twitterXML   `xml:"twitter,omitempty"`
	Canonical   *canonicalXML `xml:"canonical,omitempty"`
	Description string        `xml:"description,omitempty"`
	JsonLD      *jsonLDXML    `xml:"json_ld,omitempty"`
	Hreflang    []hreflangXML `xml:"hreflang,omitempty"`
	Robots      string        `xml:"robots,omitempty"`
}

type ogXML struct {
	Title       string `xml:"title,attr"`
	Description string `xml:"description,attr"`
	Image       string `xml:"image,attr"`
	Type        string `xml:"type,attr"`
}

type twitterXML struct {
	Card string `xml:"card,attr"`
	Site string `xml:"site,attr"`
}

type canonicalXML struct {
	URL string `xml:"url,attr"`
}

type jsonLDXML struct {
	Count   int         `xml:"count,attr"`
	Schemas []schemaXML `xml:"schema"`
}

type schemaXML struct {
	Type string `xml:"type,attr"`
}

type hreflangXML struct {
	Lang string `xml:"lang,attr"`
	Href string `xml:"href,attr"`
}

type perfXML struct {
	Compression   string            `xml:"compression,omitempty"`
	CacheControl  string            `xml:"cache_control,omitempty"`
	HTTP3         *http3XML         `xml:"http3,omitempty"`
	ResourceHints *resourceHintsXML `xml:"resource_hints,omitempty"`
	LazyLoading   *lazyLoadingXML   `xml:"lazy_loading,omitempty"`
	InlineCSS     *inlineCSSXML     `xml:"inline_css,omitempty"`
}

type http3XML struct {
	Supported string `xml:"supported,attr"`
}

type resourceHintsXML struct {
	Preload    int `xml:"preload,attr"`
	Prefetch   int `xml:"prefetch,attr"`
	Preconnect int `xml:"preconnect,attr"`
}

type lazyLoadingXML struct {
	Lazy  int `xml:"lazy,attr"`
	Total int `xml:"total,attr"`
}

type inlineCSSXML struct {
	Count int `xml:"count,attr"`
	Bytes int `xml:"bytes,attr"`
}

type a11yXML struct {
	Score      int            `xml:"score,attr"`
	Lang       string         `xml:"lang,omitempty"`
	AltText    *altTextXML    `xml:"alt_text,omitempty"`
	Headings   headingsXML    `xml:"headings"`
	Landmarks  *landmarksXML  `xml:"landmarks,omitempty"`
	FormLabels *formLabelsXML `xml:"form_labels,omitempty"`
}

type altTextXML struct {
	WithAlt  int `xml:"with_alt,attr"`
	EmptyAlt int `xml:"empty_alt,attr"`
	NoAlt    int `xml:"no_alt,attr"`
}

type headingsXML struct {
	H1   int  `xml:"h1,attr"`
	Skip bool `xml:"skip,attr"`
}

type landmarksXML struct {
	Count int `xml:"count,attr"`
}

type formLabelsXML struct {
	Labeled int `xml:"labeled,attr"`
	Total   int `xml:"total,attr"`
}

type contentXML struct {
	Links           linksXML            `xml:"links"`
	ExternalDomains *externalDomainsXML `xml:"external_domains,omitempty"`
	WordCount       int                 `xml:"word_count"`
	Iframes         []srcPlatformXML    `xml:"iframe,omitempty"`
}

type linksXML struct {
	Internal int `xml:"internal,attr"`
	External int `xml:"external,attr"`
}

type externalDomainsXML struct {
	Count int    `xml:"count,attr"`
	Value string `xml:",chardata"`
}

// srcPlatformXML is the shared {src, platform} shape used by iframe/video/audio.
type srcPlatformXML struct {
	Src      string `xml:"src,attr"`
	Platform string `xml:"platform,attr"`
}

type mediaXML struct {
	Images    imagesXML        `xml:"images"`
	ImageCDNs string           `xml:"image_cdns,omitempty"`
	Videos    []srcPlatformXML `xml:"video,omitempty"`
	Audio     []srcPlatformXML `xml:"audio,omitempty"`
}

type imagesXML struct {
	Total   int              `xml:"total,attr"`
	Srcset  int              `xml:"srcset,attr"`
	Picture int              `xml:"picture,attr"`
	Formats []imageFormatXML `xml:"format"`
}

type imageFormatXML struct {
	Name  string `xml:"name,attr"`
	Count int    `xml:"count,attr"`
}

type fontsXML struct {
	GoogleFonts string       `xml:"google_fonts,omitempty"`
	AdobeFonts  string       `xml:"adobe_fonts,omitempty"`
	FontFace    *fontFaceXML `xml:"font_face,omitempty"`
}

type fontFaceXML struct {
	Count    int    `xml:"count,attr"`
	Families string `xml:"families,attr"`
}

type pwaXML struct {
	IsPWA    bool   `xml:"is_pwa,attr"`
	Manifest string `xml:"manifest,attr"`
	SW       bool   `xml:"sw,attr"`
	Theme    string `xml:"theme,attr"`
}

type apiXML struct {
	Endpoints   []endpointXML   `xml:"endpoint,omitempty"`
	GraphQL     *graphqlXML     `xml:"graphql,omitempty"`
	Framework   []fwDataXML     `xml:"framework_data,omitempty"`
	WebSockets  []wsXML         `xml:"websocket,omitempty"`
	FormActions []formActionXML `xml:"form_action,omitempty"`
}

type endpointXML struct {
	URL    string `xml:"url,attr"`
	Source string `xml:"source,attr"`
}

type graphqlXML struct {
	Detected string `xml:"detected,attr"`
}

// fwDataXML models a <framework_data> element; only the set flag renders. Both
// next and nuxt are emitted as separate elements via the apiXML.Framework slice
// (encoding/xml rejects two struct fields sharing one element tag).
type fwDataXML struct {
	Next string `xml:"next,attr,omitempty"`
	Nuxt string `xml:"nuxt,attr,omitempty"`
}

type wsXML struct {
	URL string `xml:"url,attr"`
}

type formActionXML struct {
	URL string `xml:"url,attr"`
}

type metaXML struct {
	Generator string `xml:"generator,attr"`
	Server    string `xml:"server,attr"`
	Title     string `xml:"title,attr"`
}

type assetsXML struct {
	Scripts     int `xml:"scripts,attr"`
	Stylesheets int `xml:"stylesheets,attr"`
}

type sourcesXML struct {
	Path      string        `xml:"path,attr,omitempty"`
	Files     int           `xml:"files,attr"`
	Reason    string        `xml:"reason,attr,omitempty"`
	Languages []languageXML `xml:"language,omitempty"`
}

type languageXML struct {
	Name  string `xml:"name,attr"`
	Files int    `xml:"files,attr"`
}

// ---- site_crawl ----

type siteCrawlRespXML struct {
	XMLName xml.Name        `xml:"response"`
	Tool    string          `xml:"tool,attr"`
	Summary crawlSummaryXML `xml:"summary"`
	Pages   []crawlPageXML  `xml:"page"`
}

type crawlSummaryXML struct {
	Pages     int    `xml:"pages,attr"`
	Errors    int    `xml:"errors,attr"`
	ElapsedMs uint64 `xml:"elapsed_ms,attr"`
}

// crawlPageXML models both page shapes with pointer attrs: an error page emits
// url/depth/error; a success page emits url/status/depth/title/links/bytes plus
// an optional <content> CDATA child. Non-nil pointers render even for zero
// values (so a success page keeps title="" / links="0"); nil pointers omit.
type crawlPageXML struct {
	URL     string    `xml:"url,attr"`
	Status  *uint16   `xml:"status,attr,omitempty"`
	Depth   uint32    `xml:"depth,attr"`
	Error   *string   `xml:"error,attr,omitempty"`
	Title   *string   `xml:"title,attr,omitempty"`
	Links   *int      `xml:"links,attr,omitempty"`
	Bytes   *int      `xml:"bytes,attr,omitempty"`
	Content *xmlCDATA `xml:"content,omitempty"`
}
