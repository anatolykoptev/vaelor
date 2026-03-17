// Package knownwf contains some types for documenting why one of
// the *Unchecked functions was used/why the input is well-formed.
package knownwf

// UTF8Reason is for documenting why a string is valid UTF-8 when
// using one of the *Unchecked functions.
//
// Feel free to create a PR for adding more reasons.
type UTF8Reason string

const (
	// UTF8DeserializedFromProtobufString should be used when the string
	// was obtained from Protobuf marshaling; the string type in Protobuf
	// is guaranteed to be valid UTF-8.
	UTF8DeserializedFromProtobufString UTF8Reason = "deserialized from protobuf string type"
	// UTF8DeserializedFromJSON should be used when the string
	// was deserialized using the standard encoding/json package
	// (or a package similar to it) which guarantees that
	// strings are coerced to valid UTF-8.
	UTF8DeserializedFromJSON UTF8Reason = "deserialized from JSON"
	// UTF8ExplicitlyChecked should be used when the string was checked earlier
	// using unicode.ValidString or similar.
	UTF8ExplicitlyChecked UTF8Reason = "explicitly checked"
	// UTF8TypeInvariant should be used when a new string or buffer is known
	// to be UTF-8 because the underlying data was validated to be UTF-8 earlier.
	UTF8TypeInvariant = "based on invariant of type"
	// UTF8OtherReason should be used when none of the other reasons apply.
	UTF8OtherReason = "other reason"
)

type AbsPathReason string

const (
	// AbsPathConstructedViaAbs should be used when filepath.Abs was used to make a path.
	AbsPathConstructedViaAbs AbsPathReason = "constructed via filepath.Abs"
	// AbsPathExplicitlyChecked should be used when filepath.IsAbs succeeded.
	AbsPathExplicitlyChecked AbsPathReason = "explicitly checked"
	// AbsPathTypeInvariant should be used when a new path is known
	// to be absolute based on the fact that the underlying data was
	// validated to be an absolute path earlier.
	AbsPathTypeInvariant = "based on invariant of type"
	// AbsPathOtherReason should be used when none of the other reasons apply.
	AbsPathOtherReason AbsPathReason = "other reason"
)

type RelPathReason string

const (
	// RelPathConstructedViaAbs should be used when filepath.Rel was used to make a path.
	RelPathConstructedViaAbs RelPathReason = "constructed via filepath.Abs"
	// RelPathExplicitlyChecked should be used when filepath.IsAbs failed.
	RelPathExplicitlyChecked RelPathReason = "explicitly checked"
	// RelPathTypeInvariant should be used when a new path is known
	// to be relative based on the fact that underlying data was
	// validated to be a relative path earlier.
	RelPathTypeInvariant = "based on invariant of type"
	// RelPathOtherReason should be used when none of the other reasons apply.
	RelPathOtherReason RelPathReason = "other reason"
)
