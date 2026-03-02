package explore

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	readmeMaxBytes    = 2048
	readmeMaxChars    = 500
	readmeMaxSentences = 3
)

// readmeExcerpt extracts the first meaningful sentences from a README file.
func readmeExcerpt(root string) string {
	names := []string{"README.md", "readme.md", "Readme.md"}
	var data []byte
	for _, name := range names {
		var err error
		data, err = os.ReadFile(filepath.Join(root, name))
		if err == nil {
			break
		}
	}
	if len(data) == 0 {
		return ""
	}
	if len(data) > readmeMaxBytes {
		data = data[:readmeMaxBytes]
	}

	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		// Skip title line.
		if strings.HasPrefix(trimmed, "# ") {
			continue
		}
		// Skip badge lines.
		if strings.HasPrefix(trimmed, "[!") || strings.HasPrefix(trimmed, "[![") {
			continue
		}
		// Skip empty lines and HTML comments.
		if trimmed == "" || strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		lines = append(lines, trimmed)
	}

	text := strings.Join(lines, " ")
	return extractSentences(text, readmeMaxSentences, readmeMaxChars)
}

// extractSentences returns up to n sentences from text, capped at maxChars.
func extractSentences(text string, n, maxChars int) string {
	var result strings.Builder
	count := 0
	runes := []rune(text)

	for i := 0; i < len(runes) && count < n; i++ {
		result.WriteRune(runes[i])
		if result.Len() >= maxChars {
			break
		}
		// Sentence boundary: period followed by space+uppercase or newline.
		if runes[i] == '.' && i+1 < len(runes) {
			next := runes[i+1]
			if next == '\n' || (next == ' ' && i+2 < len(runes) && unicode.IsUpper(runes[i+2])) {
				count++
			}
		}
	}

	return strings.TrimSpace(result.String())
}
