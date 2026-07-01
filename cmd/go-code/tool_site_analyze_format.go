package main

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/webanalyze"
)

// buildSiteHead builds the shared <site> head (technologies through extras)
// common to both detect and full mode. Callers append the mode-specific tail
// (meta+assets, or sources+hint).
func buildSiteHead(resp *webanalyze.AnalyzeResponse) siteXML {
	return siteXML{
		URL:           resp.URL,
		Status:        resp.Status,
		Technologies:  buildTechnologies(resp.Technologies),
		SEO:           buildSEO(resp.SEO),
		Performance:   buildPerformance(resp.Performance),
		Accessibility: buildAccessibility(resp.Accessibility),
		Content:       buildContent(resp.Content),
		Media:         buildMedia(resp.Media),
		Fonts:         buildFonts(resp.Fonts),
		PWA:           buildPWA(resp.PWA),
		API:           buildAPI(resp.API),
	}
}

func buildTechnologies(techs []webanalyze.Technology) techsXML {
	out := techsXML{Count: len(techs)}
	for _, t := range techs {
		out.Items = append(out.Items, techXML{
			Category:   strings.Join(t.Categories, ", "),
			Name:       t.Name,
			Confidence: t.Confidence,
			Version:    t.Version,
		})
	}
	return out
}

func buildSEO(seo webanalyze.SeoReport) seoXML {
	out := seoXML{Score: seo.Score, Description: seo.Description, Robots: seo.Robots}
	if seo.OG.Title != "" {
		out.OG = &ogXML{Title: seo.OG.Title, Description: seo.OG.Description, Image: seo.OG.Image, Type: seo.OG.Type}
	}
	if seo.Twitter.Card != "" {
		out.Twitter = &twitterXML{Card: seo.Twitter.Card, Site: seo.Twitter.Site}
	}
	if seo.Canonical != nil && *seo.Canonical != "" {
		out.Canonical = &canonicalXML{URL: *seo.Canonical}
	}
	if len(seo.JsonLD) > 0 {
		j := &jsonLDXML{Count: len(seo.JsonLD)}
		for _, e := range seo.JsonLD {
			j.Schemas = append(j.Schemas, schemaXML{Type: e.SchemaType})
		}
		out.JsonLD = j
	}
	for _, h := range seo.Hreflang {
		out.Hreflang = append(out.Hreflang, hreflangXML{Lang: h.Lang, Href: h.Href})
	}
	return out
}

func buildPerformance(p webanalyze.PerformanceReport) perfXML {
	out := perfXML{Compression: p.Compression, CacheControl: p.CacheControl}
	if p.HTTP3Supported {
		out.HTTP3 = &http3XML{Supported: "true"}
	}
	if hints := len(p.Preload) + len(p.Prefetch) + len(p.Preconnect); hints > 0 {
		out.ResourceHints = &resourceHintsXML{
			Preload:    len(p.Preload),
			Prefetch:   len(p.Prefetch),
			Preconnect: len(p.Preconnect),
		}
	}
	if p.ImagesTotal > 0 {
		out.LazyLoading = &lazyLoadingXML{Lazy: p.ImagesLazy, Total: p.ImagesTotal}
	}
	if p.InlineStyleCount > 0 {
		out.InlineCSS = &inlineCSSXML{Count: p.InlineStyleCount, Bytes: p.InlineStyleBytes}
	}
	return out
}

func buildAccessibility(a webanalyze.AccessibilityReport) a11yXML {
	out := a11yXML{Score: a.Score, Lang: a.Lang}
	if total := a.ImagesWithAlt + a.ImagesEmptyAlt + a.ImagesNoAlt; total > 0 {
		out.AltText = &altTextXML{WithAlt: a.ImagesWithAlt, EmptyAlt: a.ImagesEmptyAlt, NoAlt: a.ImagesNoAlt}
	}
	out.Headings = headingsXML{H1: a.H1Count, Skip: a.HeadingSkip}
	if a.Landmarks > 0 {
		out.Landmarks = &landmarksXML{Count: a.Landmarks}
	}
	if a.InputsTotal > 0 {
		out.FormLabels = &formLabelsXML{Labeled: a.InputsWithLabel, Total: a.InputsTotal}
	}
	return out
}

func buildContent(c webanalyze.ContentReport) contentXML {
	out := contentXML{
		Links:     linksXML{Internal: c.InternalLinks, External: c.ExternalLinks},
		WordCount: c.WordCount,
	}
	if len(c.ExternalDomains) > 0 {
		out.ExternalDomains = &externalDomainsXML{Count: len(c.ExternalDomains), Value: strings.Join(c.ExternalDomains, ", ")}
	}
	for _, f := range c.Iframes {
		out.Iframes = append(out.Iframes, srcPlatformXML{Src: f.Src, Platform: f.Platform})
	}
	return out
}

func buildMedia(m webanalyze.MediaReport) mediaXML {
	out := mediaXML{Images: imagesXML{Total: m.ImagesTotal, Srcset: m.SrcsetCount, Picture: m.PictureCount}}
	for ext, count := range m.ImageFormats {
		out.Images.Formats = append(out.Images.Formats, imageFormatXML{Name: ext, Count: count})
	}
	if len(m.ImageCDNs) > 0 {
		out.ImageCDNs = strings.Join(m.ImageCDNs, ", ")
	}
	for _, v := range m.Videos {
		out.Videos = append(out.Videos, srcPlatformXML{Src: v.Src, Platform: v.Platform})
	}
	for _, a := range m.Audio {
		out.Audio = append(out.Audio, srcPlatformXML{Src: a.Src, Platform: a.Platform})
	}
	return out
}

func buildFonts(f webanalyze.FontsReport) *fontsXML {
	if len(f.GoogleFonts) == 0 && !f.AdobeFonts && f.FontFaceCount == 0 {
		return nil
	}
	out := &fontsXML{}
	if len(f.GoogleFonts) > 0 {
		out.GoogleFonts = strings.Join(f.GoogleFonts, ", ")
	}
	if f.AdobeFonts {
		out.AdobeFonts = "true"
	}
	if f.FontFaceCount > 0 {
		out.FontFace = &fontFaceXML{Count: f.FontFaceCount, Families: strings.Join(f.FontFamilies, ", ")}
	}
	return out
}

func buildPWA(p webanalyze.PwaReport) *pwaXML {
	if p.ManifestURL == "" && !p.HasServiceWorker {
		return nil
	}
	return &pwaXML{IsPWA: p.IsPWA, Manifest: p.ManifestURL, SW: p.HasServiceWorker, Theme: p.ThemeColor}
}

func buildAPI(a webanalyze.ApiReport) *apiXML {
	hasAPI := len(a.Endpoints) > 0 || a.GraphQLDetected || a.NextData || len(a.FormActions) > 0 || len(a.WebSocketURLs) > 0
	if !hasAPI {
		return nil
	}
	out := &apiXML{}
	for _, ep := range a.Endpoints {
		out.Endpoints = append(out.Endpoints, endpointXML{URL: ep.URL, Source: ep.Source})
	}
	if a.GraphQLDetected {
		out.GraphQL = &graphqlXML{Detected: "true"}
	}
	if a.NextData {
		out.Framework = append(out.Framework, fwDataXML{Next: "true"})
	}
	if a.NuxtData {
		out.Framework = append(out.Framework, fwDataXML{Nuxt: "true"})
	}
	for _, ws := range a.WebSocketURLs {
		out.WebSockets = append(out.WebSockets, wsXML{URL: ws})
	}
	for _, action := range a.FormActions {
		out.FormActions = append(out.FormActions, formActionXML{URL: action})
	}
	return out
}
