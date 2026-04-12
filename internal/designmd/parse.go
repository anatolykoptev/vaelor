// internal/designmd/parse.go
package designmd

import (
	"regexp"
	"strings"
)

type Section struct {
	Title     string
	Body      string
	StartLine int
}

type BrandMeta struct {
	Vibe    string   `json:"vibe"`
	Colors  []string `json:"colors"`
	BestFor string   `json:"best_for"`
}

var (
	sectionRe  = regexp.MustCompile(`(?m)^## \d+\\?\.\s+(.+)$`)
	hexColorRe = regexp.MustCompile(`#[0-9a-fA-F]{6}`)
)

func SplitSections(content string) []Section {
	matches := sectionRe.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}

	lineOf := func(bytePos int) int {
		n := 1
		for i, ch := range content {
			if i >= bytePos {
				break
			}
			if ch == '\n' {
				n++
			}
		}
		return n
	}

	allNames := sectionRe.FindAllStringSubmatch(content, -1)
	var sections []Section

	for i, loc := range matches {
		start := loc[0]
		end := len(content)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := strings.TrimRight(content[start:end], "\n\r ")
		sections = append(sections, Section{
			Title:     allNames[i][1],
			Body:      body,
			StartLine: lineOf(start),
		})
	}
	return sections
}

func ExtractMeta(content string) BrandMeta {
	sections := SplitSections(content)
	var meta BrandMeta

	for _, s := range sections {
		lower := strings.ToLower(s.Title)
		switch {
		case strings.Contains(lower, "visual theme"):
			bodyLines := strings.SplitN(s.Body, "\n", 2)
			if len(bodyLines) < 2 {
				continue
			}
			text := strings.TrimSpace(bodyLines[1])
			if idx := strings.Index(text, ". "); idx > 0 {
				meta.Vibe = text[:idx+1]
			} else if strings.HasSuffix(text, ".") {
				meta.Vibe = text
			} else if nl := strings.Index(text, "\n"); nl > 0 {
				meta.Vibe = strings.TrimRight(text[:nl], ". ") + "."
			}

		case strings.Contains(lower, "color palette"):
			found := hexColorRe.FindAllString(s.Body, -1)
			seen := make(map[string]bool)
			for _, c := range found {
				c = strings.ToLower(c)
				if !seen[c] {
					seen[c] = true
					meta.Colors = append(meta.Colors, c)
				}
				if len(meta.Colors) >= 3 {
					break
				}
			}

		case strings.Contains(lower, "agent prompt"):
			parts := strings.SplitN(s.Body, "\n", 2)
			if len(parts) < 2 {
				continue
			}
			meta.BestFor = strings.TrimSpace(parts[1])
			if nl := strings.Index(meta.BestFor, "\n"); nl > 0 {
				meta.BestFor = meta.BestFor[:nl]
			}
			meta.BestFor = strings.TrimPrefix(meta.BestFor, "Best for: ")
		}
	}
	return meta
}
