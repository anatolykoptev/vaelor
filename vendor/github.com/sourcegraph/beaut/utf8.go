package beaut

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/sourcegraph/beaut/lib/knownwf"
)

type UTF8String struct {
	rawValue string
}

func NewUTF8String(rawValue string) (UTF8String, error) {
	if utf8.ValidString(rawValue) {
		return UTF8String{rawValue: rawValue}, nil
	}
	return UTF8String{}, NewNotWellFormedUTF8Error(rawValue)
}

type NotWellFormedUTF8Error struct {
	// data MUST NOT be modified, since we expose a public
	// API that does a zero-copy string->[]byte conversion
	// and it is unsafe to modify the underlying byte slice.
	data []byte
}

func NewNotWellFormedUTF8Error(s string) NotWellFormedUTF8Error {
	bytes := unsafeGetUnderlyingByteSlice(s)
	return NotWellFormedUTF8Error{bytes}
}

func NewNotWellFormedUTF8ErrorFromBytes(s []byte) NotWellFormedUTF8Error {
	return NotWellFormedUTF8Error{s}
}

var _ error = NotWellFormedUTF8Error{}

func (e NotWellFormedUTF8Error) Error() string {
	suffix := ""
	if len(e.data) > 50 {
		suffix = " (truncated)"
	}
	return fmt.Sprintf("data is not well-formed UTF-8: %+.50q%s", e.data, suffix)
}

func NewUTF8StringUnchecked(rawValue string, _ knownwf.UTF8Reason) UTF8String {
	return UTF8String{rawValue: rawValue}
}

func (s UTF8String) RawValue() string {
	return s.rawValue
}

var _ simpleContainer = UTF8String{}
var _ byteIndexable = UTF8String{}
var _ utf8Sliceable[UTF8String] = UTF8String{}

func (s UTF8String) Len() int {
	return len(s.rawValue)
}

func (s UTF8String) IsEmpty() bool {
	return s.rawValue == ""
}

func (s UTF8String) ByteAt(i ByteIndex) byte {
	return s.rawValue[i]
}

// RuneAt returns the rune at the given byte index.
//
// If the byte index is not a valid UTF-8 code point start index,
// returns -1 for the length.
func (s UTF8String) RuneAt(i ByteIndex) (_ rune, runeLength int32) {
	bytes := unsafeGetUnderlyingByteSlice(s.rawValue)
	return findRuneAtIndex(bytes, i)
}

func (s UTF8String) Slice(start ByteIndex, end ByteIndex) string {
	return s.rawValue[start:end]
}

func (s UTF8String) SliceUTF8(start ByteIndex, end ByteIndex) (UTF8String, error) {
	bytes := unsafeGetUnderlyingByteSlice(s.rawValue)
	if err := checkSafelySliceableUTF8(bytes, start, end); err != nil {
		return UTF8String{}, err
	}
	return UTF8String{rawValue: s.rawValue[start:end]}, nil
}

func (s UTF8String) MustSliceUTF8(start ByteIndex, end ByteIndex) UTF8String {
	slice, err := s.SliceUTF8(start, end)
	if err != nil {
		panic(err)
	}
	return slice
}

type InvalidCodepointStartIndexError struct {
	Byte  byte
	Index ByteIndex
}

func (e InvalidCodepointStartIndexError) Error() string {
	return fmt.Sprintf("byte '%x' at index %d is not a valid start byte for a codepoint", e.Byte, e.Index)
}

var _ fmt.Stringer = UTF8String{}
var _ semigroup[UTF8String] = UTF8String{}

func (s UTF8String) String() string {
	return s.rawValue
}

func (s UTF8String) Combine(other UTF8String) UTF8String {
	return UTF8String{s.rawValue + other.rawValue}
}

func (s UTF8String) Join(others ...UTF8String) UTF8String {
	b := strings.Builder{}
	b.WriteString(s.rawValue)
	for _, other := range others {
		b.WriteString(other.rawValue)
	}
	return NewUTF8StringUnchecked(b.String(), knownwf.UTF8TypeInvariant)
}

// ToUTF8Bytes copies the underlying buffer into a new byte slice.
func (s UTF8String) ToUTF8Bytes() UTF8Bytes {
	return NewUTF8BytesUnchecked([]byte(s.rawValue), knownwf.UTF8TypeInvariant)
}

type UTF8Bytes struct {
	rawValue []byte
}

func NewUTF8Bytes(rawValue []byte) (_ UTF8Bytes, err error) {
	if utf8.Valid(rawValue) {
		return UTF8Bytes{rawValue: rawValue}, nil
	}
	return UTF8Bytes{}, NotWellFormedUTF8Error{rawValue}
}

func NewUTF8BytesUnchecked(rawValue []byte, _ knownwf.UTF8Reason) UTF8Bytes {
	return UTF8Bytes{rawValue: rawValue}
}

// RawValueNoCopy returns the raw UTF-8 encoded bytes of the string.
//
// SAFETY: The caller MUST NOT modify the returned value in a way
// that would break the type's invariants.
func (s *UTF8Bytes) RawValueNoCopy() []byte {
	return s.rawValue
}

// RawValueCopy returns a copy of the underlying byte slice.
func (s *UTF8Bytes) RawValueCopy() []byte {
	return slices.Clone(s.rawValue)
}

var _ simpleContainer = &UTF8Bytes{}
var _ byteIndexable = &UTF8Bytes{}
var _ utf8Sliceable[UTF8Bytes] = &UTF8Bytes{}

func (s *UTF8Bytes) Len() int {
	return len(s.rawValue)
}

func (s *UTF8Bytes) IsEmpty() bool {
	return len(s.rawValue) == 0
}

func (s *UTF8Bytes) ByteAt(i ByteIndex) byte {
	return s.rawValue[i]
}

// RuneAt returns the rune at the given byte index.
//
// If the byte index is not a valid UTF-8 code point start index,
// returns -1 for the length.
func (s *UTF8Bytes) RuneAt(i ByteIndex) (_ rune, runeLength int32) {
	return findRuneAtIndex(s.rawValue, i)
}

func (s *UTF8Bytes) SliceUTF8(start ByteIndex, end ByteIndex) (UTF8Bytes, error) {
	if err := checkSafelySliceableUTF8(s.rawValue, start, end); err != nil {
		return UTF8Bytes{}, err
	}
	return NewUTF8BytesUnchecked(s.rawValue[start:end], knownwf.UTF8TypeInvariant), nil
}

func (s *UTF8Bytes) MustSliceUTF8(start ByteIndex, end ByteIndex) UTF8Bytes {
	slice, err := s.SliceUTF8(start, end)
	if err != nil {
		panic(err)
	}
	return slice
}

var _ fmt.Stringer = &UTF8Bytes{}
var _ semigroup[UTF8Bytes] = &UTF8Bytes{}

func (s *UTF8Bytes) String() string {
	return string(s.rawValue)
}

// Combine concatenates the given UTF-8 byte slices into a single UTF-8 byte slice.
func (s *UTF8Bytes) Combine(other UTF8Bytes) UTF8Bytes {
	buf := make([]byte, len(s.rawValue)+len(other.rawValue))
	copy(buf, s.rawValue)
	copy(buf[len(s.rawValue):], other.rawValue)
	return NewUTF8BytesUnchecked(buf, knownwf.UTF8TypeInvariant)
}

// Join concatenates the given UTF-8 byte slices into a single UTF-8 byte slice.
func (s *UTF8Bytes) Join(others ...UTF8Bytes) UTF8Bytes {
	totalLen := len(s.rawValue)
	for _, other := range others {
		totalLen += len(other.rawValue)
	}
	buf := make([]byte, totalLen)
	copy(buf, s.rawValue)
	start := len(s.rawValue)
	for _, other := range others {
		copy(buf[start:], other.rawValue)
		start += len(other.rawValue)
	}
	return NewUTF8BytesUnchecked(buf, knownwf.UTF8TypeInvariant)
}

// ToUTF8String copies the underlying buffer into a new string.
func (s *UTF8Bytes) ToUTF8String() UTF8String {
	return NewUTF8StringUnchecked(string(s.rawValue), knownwf.UTF8TypeInvariant)
}

func (s *UTF8Bytes) MustAppendASCIIByte(b byte) {
	if b > 0x7f {
		panic(fmt.Sprintf("byte is not valid ASCII: %x", b))
	}
	s.rawValue = append(s.rawValue, b)
}

func (s *UTF8Bytes) MustAppendRune(r rune) {
	runeBytes, numBytes := explodeRune(r)
	if numBytes == -1 {
		panic(fmt.Sprintf("rune is not valid UTF-8: %x", r))
	}
	for i := int32(0); i < numBytes; i++ {
		s.rawValue = append(s.rawValue, runeBytes[i])
	}
}
