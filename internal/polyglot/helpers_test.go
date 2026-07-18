package polyglot

import (
	"time"

	"github.com/anatolykoptev/vaelor/internal/ingest"
)

func makeFile(relPath, lang string) *ingest.File {
	return &ingest.File{
		Path:     "/repo/" + relPath,
		RelPath:  relPath,
		Language: lang,
		Size:     100,
		ModTime:  time.Now(),
	}
}
