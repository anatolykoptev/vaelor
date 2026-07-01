package main

import (
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/webanalyze"
)

func formatTechnologies(sb *strings.Builder, techs []webanalyze.Technology) {
	fmt.Fprintf(sb, "<technologies count=\"%d\">", len(techs))
	for _, t := range techs {
		cat := strings.Join(t.Categories, ", ")
		ver := ""
		if t.Version != nil {
			ver = fmt.Sprintf(" version=%q", *t.Version)
		}
		fmt.Fprintf(sb, "<tech category=%q name=%q confidence=\"%d\"%s/>",
			cat, t.Name, t.Confidence, ver)
	}
	sb.WriteString("</technologies>")
}

func formatSEO(sb *strings.Builder, seo webanalyze.SeoReport) {
	fmt.Fprintf(sb, "<seo score=\"%d\">", seo.Score)
	if seo.OG.Title != "" {
		fmt.Fprintf(sb, "<og title=%q description=%q image=%q type=%q/>",
			seo.OG.Title, seo.OG.Description, seo.OG.Image, seo.OG.Type)
	}
	if seo.Twitter.Card != "" {
		fmt.Fprintf(sb, "<twitter card=%q site=%q/>",
			seo.Twitter.Card, seo.Twitter.Site)
	}
	if seo.Canonical != nil && *seo.Canonical != "" {
		fmt.Fprintf(sb, "<canonical url=%q/>", *seo.Canonical)
	}
	if seo.Description != "" {
		fmt.Fprintf(sb, "<description>%s</description>", seo.Description)
	}
	if len(seo.JsonLD) > 0 {
		fmt.Fprintf(sb, "<json_ld count=\"%d\">", len(seo.JsonLD))
		for _, j := range seo.JsonLD {
			fmt.Fprintf(sb, "<schema type=%q/>", j.SchemaType)
		}
		sb.WriteString("</json_ld>")
	}
	for _, h := range seo.Hreflang {
		fmt.Fprintf(sb, "<hreflang lang=%q href=%q/>", h.Lang, h.Href)
	}
	if seo.Robots != "" {
		fmt.Fprintf(sb, "<robots>%s</robots>", seo.Robots)
	}
	sb.WriteString("</seo>")
}

func formatPerformance(sb *strings.Builder, p webanalyze.PerformanceReport) {
	sb.WriteString("<performance>")
	if p.Compression != "" {
		fmt.Fprintf(sb, "<compression>%s</compression>", p.Compression)
	}
	if p.CacheControl != "" {
		fmt.Fprintf(sb, "<cache_control>%s</cache_control>", p.CacheControl)
	}
	if p.HTTP3Supported {
		sb.WriteString("<http3 supported=\"true\"/>")
	}
	hints := len(p.Preload) + len(p.Prefetch) + len(p.Preconnect)
	if hints > 0 {
		fmt.Fprintf(sb, "<resource_hints preload=\"%d\" prefetch=\"%d\" preconnect=\"%d\"/>",
			len(p.Preload), len(p.Prefetch), len(p.Preconnect))
	}
	if p.ImagesTotal > 0 {
		fmt.Fprintf(sb, "<lazy_loading lazy=\"%d\" total=\"%d\"/>",
			p.ImagesLazy, p.ImagesTotal)
	}
	if p.InlineStyleCount > 0 {
		fmt.Fprintf(sb, "<inline_css count=\"%d\" bytes=\"%d\"/>",
			p.InlineStyleCount, p.InlineStyleBytes)
	}
	sb.WriteString("</performance>")
}

func formatAccessibility(sb *strings.Builder, a webanalyze.AccessibilityReport) {
	fmt.Fprintf(sb, "<accessibility score=\"%d\">", a.Score)
	if a.Lang != "" {
		fmt.Fprintf(sb, "<lang>%s</lang>", a.Lang)
	}
	total := a.ImagesWithAlt + a.ImagesEmptyAlt + a.ImagesNoAlt
	if total > 0 {
		fmt.Fprintf(sb, "<alt_text with_alt=\"%d\" empty_alt=\"%d\" no_alt=\"%d\"/>",
			a.ImagesWithAlt, a.ImagesEmptyAlt, a.ImagesNoAlt)
	}
	fmt.Fprintf(sb, "<headings h1=\"%d\" skip=\"%t\"/>", a.H1Count, a.HeadingSkip)
	if a.Landmarks > 0 {
		fmt.Fprintf(sb, "<landmarks count=\"%d\"/>", a.Landmarks)
	}
	if a.InputsTotal > 0 {
		fmt.Fprintf(sb, "<form_labels labeled=\"%d\" total=\"%d\"/>",
			a.InputsWithLabel, a.InputsTotal)
	}
	sb.WriteString("</accessibility>")
}

func formatContent(sb *strings.Builder, c webanalyze.ContentReport) {
	sb.WriteString("<content>")
	fmt.Fprintf(sb, "<links internal=\"%d\" external=\"%d\"/>",
		c.InternalLinks, c.ExternalLinks)
	if len(c.ExternalDomains) > 0 {
		fmt.Fprintf(sb, "<external_domains count=\"%d\">%s</external_domains>",
			len(c.ExternalDomains), strings.Join(c.ExternalDomains, ", "))
	}
	fmt.Fprintf(sb, "<word_count>%d</word_count>", c.WordCount)
	for _, iframe := range c.Iframes {
		fmt.Fprintf(sb, "<iframe src=%q platform=%q/>", iframe.Src, iframe.Platform)
	}
	sb.WriteString("</content>")
}

func formatMedia(sb *strings.Builder, m webanalyze.MediaReport) {
	sb.WriteString("<media>")
	fmt.Fprintf(sb, "<images total=\"%d\" srcset=\"%d\" picture=\"%d\">",
		m.ImagesTotal, m.SrcsetCount, m.PictureCount)
	for ext, count := range m.ImageFormats {
		fmt.Fprintf(sb, "<format name=%q count=\"%d\"/>", ext, count)
	}
	sb.WriteString("</images>")
	if len(m.ImageCDNs) > 0 {
		fmt.Fprintf(sb, "<image_cdns>%s</image_cdns>",
			strings.Join(m.ImageCDNs, ", "))
	}
	for _, v := range m.Videos {
		fmt.Fprintf(sb, "<video src=%q platform=%q/>", v.Src, v.Platform)
	}
	for _, a := range m.Audio {
		fmt.Fprintf(sb, "<audio src=%q platform=%q/>", a.Src, a.Platform)
	}
	sb.WriteString("</media>")
}

func formatExtras(sb *strings.Builder, f webanalyze.FontsReport, p webanalyze.PwaReport, a webanalyze.ApiReport) {
	if len(f.GoogleFonts) > 0 || f.AdobeFonts || f.FontFaceCount > 0 {
		sb.WriteString("<fonts>")
		if len(f.GoogleFonts) > 0 {
			fmt.Fprintf(sb, "<google_fonts>%s</google_fonts>",
				strings.Join(f.GoogleFonts, ", "))
		}
		if f.AdobeFonts {
			sb.WriteString("<adobe_fonts>true</adobe_fonts>")
		}
		if f.FontFaceCount > 0 {
			fmt.Fprintf(sb, "<font_face count=\"%d\" families=%q/>",
				f.FontFaceCount, strings.Join(f.FontFamilies, ", "))
		}
		sb.WriteString("</fonts>")
	}
	if p.ManifestURL != "" || p.HasServiceWorker {
		fmt.Fprintf(sb, "<pwa is_pwa=\"%t\" manifest=%q sw=\"%t\" theme=%q/>",
			p.IsPWA, p.ManifestURL, p.HasServiceWorker, p.ThemeColor)
	}
	hasAPI := len(a.Endpoints) > 0 || a.GraphQLDetected || a.NextData || len(a.FormActions) > 0 || len(a.WebSocketURLs) > 0
	if hasAPI {
		sb.WriteString("<api_discovery>")
		for _, ep := range a.Endpoints {
			fmt.Fprintf(sb, "<endpoint url=%q source=%q/>", ep.URL, ep.Source)
		}
		if a.GraphQLDetected {
			sb.WriteString("<graphql detected=\"true\"/>")
		}
		if a.NextData {
			sb.WriteString("<framework_data next=\"true\"/>")
		}
		if a.NuxtData {
			sb.WriteString("<framework_data nuxt=\"true\"/>")
		}
		for _, ws := range a.WebSocketURLs {
			fmt.Fprintf(sb, "<websocket url=%q/>", ws)
		}
		for _, action := range a.FormActions {
			fmt.Fprintf(sb, "<form_action url=%q/>", action)
		}
		sb.WriteString("</api_discovery>")
	}
}
