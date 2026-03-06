package main

import (
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/webanalyze"
)

func formatTechnologies(sb *strings.Builder, techs []webanalyze.Technology) {
	fmt.Fprintf(sb, "    <technologies count=\"%d\">\n", len(techs))
	for _, t := range techs {
		cat := strings.Join(t.Categories, ", ")
		ver := ""
		if t.Version != nil {
			ver = fmt.Sprintf(" version=%q", *t.Version)
		}
		fmt.Fprintf(sb, "      <tech category=%q name=%q confidence=\"%d\"%s/>\n",
			cat, t.Name, t.Confidence, ver)
	}
	sb.WriteString("    </technologies>\n")
}

func formatSEO(sb *strings.Builder, seo webanalyze.SeoReport) {
	fmt.Fprintf(sb, "    <seo score=\"%d\">\n", seo.Score)
	if seo.OG.Title != "" {
		fmt.Fprintf(sb, "      <og title=%q description=%q image=%q type=%q/>\n",
			seo.OG.Title, seo.OG.Description, seo.OG.Image, seo.OG.Type)
	}
	if seo.Twitter.Card != "" {
		fmt.Fprintf(sb, "      <twitter card=%q site=%q/>\n",
			seo.Twitter.Card, seo.Twitter.Site)
	}
	if seo.Canonical != nil && *seo.Canonical != "" {
		fmt.Fprintf(sb, "      <canonical url=%q/>\n", *seo.Canonical)
	}
	if seo.Description != "" {
		fmt.Fprintf(sb, "      <description>%s</description>\n", seo.Description)
	}
	if len(seo.JsonLD) > 0 {
		fmt.Fprintf(sb, "      <json_ld count=\"%d\">\n", len(seo.JsonLD))
		for _, j := range seo.JsonLD {
			fmt.Fprintf(sb, "        <schema type=%q/>\n", j.SchemaType)
		}
		sb.WriteString("      </json_ld>\n")
	}
	for _, h := range seo.Hreflang {
		fmt.Fprintf(sb, "      <hreflang lang=%q href=%q/>\n", h.Lang, h.Href)
	}
	if seo.Robots != "" {
		fmt.Fprintf(sb, "      <robots>%s</robots>\n", seo.Robots)
	}
	sb.WriteString("    </seo>\n")
}

func formatPerformance(sb *strings.Builder, p webanalyze.PerformanceReport) {
	sb.WriteString("    <performance>\n")
	if p.Compression != "" {
		fmt.Fprintf(sb, "      <compression>%s</compression>\n", p.Compression)
	}
	if p.CacheControl != "" {
		fmt.Fprintf(sb, "      <cache_control>%s</cache_control>\n", p.CacheControl)
	}
	if p.HTTP3Supported {
		sb.WriteString("      <http3 supported=\"true\"/>\n")
	}
	hints := len(p.Preload) + len(p.Prefetch) + len(p.Preconnect)
	if hints > 0 {
		fmt.Fprintf(sb, "      <resource_hints preload=\"%d\" prefetch=\"%d\" preconnect=\"%d\"/>\n",
			len(p.Preload), len(p.Prefetch), len(p.Preconnect))
	}
	if p.ImagesTotal > 0 {
		fmt.Fprintf(sb, "      <lazy_loading lazy=\"%d\" total=\"%d\"/>\n",
			p.ImagesLazy, p.ImagesTotal)
	}
	if p.InlineStyleCount > 0 {
		fmt.Fprintf(sb, "      <inline_css count=\"%d\" bytes=\"%d\"/>\n",
			p.InlineStyleCount, p.InlineStyleBytes)
	}
	sb.WriteString("    </performance>\n")
}

func formatAccessibility(sb *strings.Builder, a webanalyze.AccessibilityReport) {
	fmt.Fprintf(sb, "    <accessibility score=\"%d\">\n", a.Score)
	if a.Lang != "" {
		fmt.Fprintf(sb, "      <lang>%s</lang>\n", a.Lang)
	}
	total := a.ImagesWithAlt + a.ImagesEmptyAlt + a.ImagesNoAlt
	if total > 0 {
		fmt.Fprintf(sb, "      <alt_text with_alt=\"%d\" empty_alt=\"%d\" no_alt=\"%d\"/>\n",
			a.ImagesWithAlt, a.ImagesEmptyAlt, a.ImagesNoAlt)
	}
	fmt.Fprintf(sb, "      <headings h1=\"%d\" skip=\"%t\"/>\n", a.H1Count, a.HeadingSkip)
	if a.Landmarks > 0 {
		fmt.Fprintf(sb, "      <landmarks count=\"%d\"/>\n", a.Landmarks)
	}
	if a.InputsTotal > 0 {
		fmt.Fprintf(sb, "      <form_labels labeled=\"%d\" total=\"%d\"/>\n",
			a.InputsWithLabel, a.InputsTotal)
	}
	sb.WriteString("    </accessibility>\n")
}

func formatContent(sb *strings.Builder, c webanalyze.ContentReport) {
	sb.WriteString("    <content>\n")
	fmt.Fprintf(sb, "      <links internal=\"%d\" external=\"%d\"/>\n",
		c.InternalLinks, c.ExternalLinks)
	if len(c.ExternalDomains) > 0 {
		fmt.Fprintf(sb, "      <external_domains count=\"%d\">%s</external_domains>\n",
			len(c.ExternalDomains), strings.Join(c.ExternalDomains, ", "))
	}
	fmt.Fprintf(sb, "      <word_count>%d</word_count>\n", c.WordCount)
	for _, iframe := range c.Iframes {
		fmt.Fprintf(sb, "      <iframe src=%q platform=%q/>\n", iframe.Src, iframe.Platform)
	}
	sb.WriteString("    </content>\n")
}

func formatMedia(sb *strings.Builder, m webanalyze.MediaReport) {
	sb.WriteString("    <media>\n")
	fmt.Fprintf(sb, "      <images total=\"%d\" srcset=\"%d\" picture=\"%d\">\n",
		m.ImagesTotal, m.SrcsetCount, m.PictureCount)
	for ext, count := range m.ImageFormats {
		fmt.Fprintf(sb, "        <format name=%q count=\"%d\"/>\n", ext, count)
	}
	sb.WriteString("      </images>\n")
	if len(m.ImageCDNs) > 0 {
		fmt.Fprintf(sb, "      <image_cdns>%s</image_cdns>\n",
			strings.Join(m.ImageCDNs, ", "))
	}
	for _, v := range m.Videos {
		fmt.Fprintf(sb, "      <video src=%q platform=%q/>\n", v.Src, v.Platform)
	}
	for _, a := range m.Audio {
		fmt.Fprintf(sb, "      <audio src=%q platform=%q/>\n", a.Src, a.Platform)
	}
	sb.WriteString("    </media>\n")
}

func formatExtras(sb *strings.Builder, f webanalyze.FontsReport, p webanalyze.PwaReport, a webanalyze.ApiReport) {
	if len(f.GoogleFonts) > 0 || f.AdobeFonts || f.FontFaceCount > 0 {
		sb.WriteString("    <fonts>\n")
		if len(f.GoogleFonts) > 0 {
			fmt.Fprintf(sb, "      <google_fonts>%s</google_fonts>\n",
				strings.Join(f.GoogleFonts, ", "))
		}
		if f.AdobeFonts {
			sb.WriteString("      <adobe_fonts>true</adobe_fonts>\n")
		}
		if f.FontFaceCount > 0 {
			fmt.Fprintf(sb, "      <font_face count=\"%d\" families=%q/>\n",
				f.FontFaceCount, strings.Join(f.FontFamilies, ", "))
		}
		sb.WriteString("    </fonts>\n")
	}
	if p.ManifestURL != "" || p.HasServiceWorker {
		fmt.Fprintf(sb, "    <pwa is_pwa=\"%t\" manifest=%q sw=\"%t\" theme=%q/>\n",
			p.IsPWA, p.ManifestURL, p.HasServiceWorker, p.ThemeColor)
	}
	hasAPI := len(a.Endpoints) > 0 || a.GraphQLDetected || a.NextData || len(a.FormActions) > 0 || len(a.WebSocketURLs) > 0
	if hasAPI {
		sb.WriteString("    <api_discovery>\n")
		for _, ep := range a.Endpoints {
			fmt.Fprintf(sb, "      <endpoint url=%q source=%q/>\n", ep.URL, ep.Source)
		}
		if a.GraphQLDetected {
			sb.WriteString("      <graphql detected=\"true\"/>\n")
		}
		if a.NextData {
			sb.WriteString("      <framework_data next=\"true\"/>\n")
		}
		if a.NuxtData {
			sb.WriteString("      <framework_data nuxt=\"true\"/>\n")
		}
		for _, ws := range a.WebSocketURLs {
			fmt.Fprintf(sb, "      <websocket url=%q/>\n", ws)
		}
		for _, action := range a.FormActions {
			fmt.Fprintf(sb, "      <form_action url=%q/>\n", action)
		}
		sb.WriteString("    </api_discovery>\n")
	}
}
