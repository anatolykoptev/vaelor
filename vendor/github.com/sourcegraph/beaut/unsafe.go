package beaut

import "unsafe"

// only OK to use in a read-only context
func unsafeGetUnderlyingByteSlice(s string) []byte {
	ptr := unsafe.StringData(s)
	return unsafe.Slice(ptr, len(s))
}
