package beaut

func explodeRune(r rune) (_ [4]byte, numBytes int32) {
	if r < 0x80 {
		return [4]byte{byte(r), 0, 0, 0}, 1
	}
	if r < 0x800 {
		return [4]byte{
			byte(r>>6) | 0b11000000,
			byte(r) | 0b10000000,
			0,
			0,
		}, 2
	}
	if r < 0x10000 {
		return [4]byte{
			byte(r>>12) | 0b11100000,
			byte(r>>6) | 0b10000000,
			byte(r) | 0b10000000,
			0,
		}, 3
	}
	return [4]byte{
		byte(r>>18) | 0b11110000,
		byte(r>>12) | 0b10000000,
		byte(r>>6) | 0b10000000,
		byte(r) | 0b10000000,
	}, 4
}

// Pre-condition: The byte must be from a string of well-formed UTF-8.
func isValidStartByteForCodepoint(b byte) bool {
	return (b >> 6) != 0b10
}
