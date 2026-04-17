package preproc

import "bytes"

// Builder accumulates virtual code and a line-map from multiple source-block spans.
type Builder struct {
	lang    string
	buf     bytes.Buffer
	lineMap []uint32 // one entry per virtual line
}

// NewBuilder creates a Builder for the given language label.
func NewBuilder(lang string) *Builder {
	return &Builder{lang: lang}
}

// AppendBlock copies fullSrc[blockStart:blockEnd] into the virtual buffer and
// records line offsets so every virtual line in this block maps to the
// corresponding original 1-based line number in the full source.
//
// Parameters:
//   - fullSrc: the entire original source file bytes
//   - blockStart: byte offset in fullSrc where the content begins (inclusive)
//   - blockEnd: byte offset in fullSrc where the content ends (exclusive)
//
// Panics if blockEnd < blockStart or offsets are out of range.
func (b *Builder) AppendBlock(fullSrc []byte, blockStart, blockEnd int) {
	if blockEnd < blockStart {
		panic("preproc: blockEnd < blockStart")
	}
	if blockStart < 0 || blockEnd > len(fullSrc) {
		panic("preproc: block offsets out of range")
	}

	// Compute the 1-based line number of blockStart in fullSrc.
	// Count '\n' bytes before blockStart.
	firstOrigLine := uint32(1)
	for _, c := range fullSrc[:blockStart] {
		if c == '\n' {
			firstOrigLine++
		}
	}

	block := fullSrc[blockStart:blockEnd]
	b.buf.Write(block)

	// Build lineMap for this block. Each '\n' creates a new virtual line.
	origLine := firstOrigLine
	// First virtual line starts at firstOrigLine.
	b.lineMap = append(b.lineMap, origLine)
	for _, c := range block {
		if c == '\n' {
			origLine++
			b.lineMap = append(b.lineMap, origLine)
		}
	}
}

// AppendBlankLine appends a single "\n" with LineMap entry 0 (padding — maps
// to no original line). Use between blocks to keep virtual symbols separate.
func (b *Builder) AppendBlankLine() {
	b.buf.WriteByte('\n')
	b.lineMap = append(b.lineMap, 0)
}

// Build finalises and returns the VirtualSource. The Builder must not be used
// after Build is called.
func (b *Builder) Build() *VirtualSource {
	code := b.buf.Bytes()
	lineMap := b.lineMap

	// lineMap should already have one entry per virtual line (newlines create
	// transitions, so we have one entry per '\n' + 1 initial). Verify parity
	// and trim trailing phantom entry if needed.
	//
	// Strategy: countLines(code) should equal len(lineMap). If code is empty,
	// lineMap may be empty — normalise to a single 0 entry for an empty buffer.
	if len(code) == 0 {
		return &VirtualSource{Code: code, Lang: b.lang, LineMap: []uint32{}}
	}

	expected := countLines(code)
	if len(lineMap) > expected {
		lineMap = lineMap[:expected]
	}

	return &VirtualSource{
		Code:    code,
		Lang:    b.lang,
		LineMap: lineMap,
	}
}
