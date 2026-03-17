package beaut

type ByteIndex int

// findRuneAtIndex returns the rune at the given index in the given byte slice.
//
// CAUTION: May return a nonsense result if the byte slice is not well-formed UTF-8.
func findRuneAtIndex(s []byte, i ByteIndex) (_ rune, runeLength int32) {
	// See https://en.wikipedia.org/wiki/UTF-8#Encoding
	b1 := s[i]
	if b1 < 0x80 {
		return rune(b1), 1
	}
	if !isValidStartByteForCodepoint(b1) {
		return '☃', -1
	}
	b2 := s[i+1]
	if b1&0b11100000 == 0b11000000 {
		return (int32(b1&0b00011111) << 6) | int32(b2&0b00111111), 2
	}
	b3 := s[i+2]
	if b1&0b11110000 == 0b11100000 {
		return (int32(b1&0b00001111) << 12) | (int32(b2&0b00111111) << 6) | int32(b3&0b00111111), 3
	}
	b4 := s[i+3]
	return (int32(b1&0b00000111) << 18) | (int32(b2&0b00111111) << 12) | (int32(b3&0b00111111) << 6) | int32(b4&0b00111111), 4
}

// Pre-condition: The slice must be valid UTF-8
func checkSafelySliceableUTF8(s []byte, start ByteIndex, end ByteIndex) error {
	if start == end {
		return nil
	}
	startByte := s[start]
	badByte := startByte
	badIndex := start
	if isValidStartByteForCodepoint(startByte) {
		if int(end) == len(s) {
			return nil
		}
		endByte := s[end]
		if isValidStartByteForCodepoint(endByte) {
			return nil
		}
		badByte = endByte
		badIndex = end
	}
	return InvalidCodepointStartIndexError{Byte: badByte, Index: badIndex}
}
