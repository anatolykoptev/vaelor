package filewatcher

import (
	"path/filepath"
)

// EventPath is a distinct type for event file/directory paths.
// It prevents accidentally passing event paths where other path types are expected.
type EventPath string

// NewEventPath creates a new EventPath from a string.
func NewEventPath(path string) EventPath {
	return EventPath(path)
}

// Get returns the underlying string value.
func (ep EventPath) Get() string {
	return string(ep)
}

// IsZero returns true if the EventPath is empty.
func (ep EventPath) IsZero() bool {
	return ep == ""
}

// String returns the string representation.
func (ep EventPath) String() string {
	return string(ep)
}

// Base returns the last element of the path.
// Example: EventPath("/home/user/file.go").Base() returns "file.go".
func (ep EventPath) Base() string {
	return filepath.Base(string(ep))
}

// Dir returns all but the last element of the path.
// Example: EventPath("/home/user/file.go").Dir() returns EventPath("/home/user").
func (ep EventPath) Dir() EventPath {
	return NewEventPath(filepath.Dir(string(ep)))
}

// Ext returns the file extension of the path.
// Example: EventPath("/home/user/file.go").Ext() returns ".go".
func (ep EventPath) Ext() string {
	return filepath.Ext(string(ep))
}

// Join appends the given elements to the path.
// Example: EventPath("/home/user").Join("docs", "readme.md") returns EventPath("/home/user/docs/readme.md").
func (ep EventPath) Join(elem ...string) EventPath {
	all := make([]string, 0, len(elem)+1)
	all = append(all, string(ep))
	all = append(all, elem...)

	return NewEventPath(filepath.Join(all...))
}

// RootPath is a distinct type for root directory paths during filesystem walking.
// It prevents accidentally passing event paths or other paths where root paths are expected.
type RootPath string

// NewRootPath creates a new RootPath from a string.
func NewRootPath(path string) RootPath {
	return RootPath(path)
}

// Get returns the underlying string value.
func (rp RootPath) Get() string {
	return string(rp)
}

// IsZero returns true if the RootPath is empty.
func (rp RootPath) IsZero() bool {
	return rp == ""
}

// String returns the string representation.
func (rp RootPath) String() string {
	return string(rp)
}

// DebounceKey is a distinct type for debouncer keys (typically file paths).
// It ensures debounce keys are not mixed with other path-like strings.
type DebounceKey string

// NewDebounceKey creates a new DebounceKey from a string.
func NewDebounceKey(key string) DebounceKey {
	return DebounceKey(key)
}

// Get returns the underlying string value.
func (dk DebounceKey) Get() string {
	return string(dk)
}

// IsZero returns true if the DebounceKey is empty.
func (dk DebounceKey) IsZero() bool {
	return dk == ""
}

// String returns the string representation.
func (dk DebounceKey) String() string {
	return string(dk)
}

// LogSubstring is a distinct type for log substring assertions in tests.
type LogSubstring string

// NewLogSubstring creates a new LogSubstring from a string.
func NewLogSubstring(s string) LogSubstring {
	return LogSubstring(s)
}

// Get returns the underlying string value.
func (ls LogSubstring) Get() string {
	return string(ls)
}

// String returns the string representation of LogSubstring.
func (ls LogSubstring) String() string {
	return string(ls)
}

// TempDir is a distinct type for temporary directory paths in tests.
type TempDir string

// NewTempDir creates a new TempDir from a string.
func NewTempDir(path string) TempDir {
	return TempDir(path)
}

// Get returns the underlying string value.
func (td TempDir) Get() string {
	return string(td)
}

// String returns the string representation.
func (td TempDir) String() string {
	return string(td)
}

// OpString is a distinct type for operation names (e.g., "fsnotify", "middleware").
type OpString string

// NewOpString creates a new OpString from a string.
func NewOpString(op string) OpString {
	return OpString(op)
}

// Get returns the underlying string value.
func (o OpString) Get() string {
	return string(o)
}

// String returns the string representation.
func (o OpString) String() string {
	return string(o)
}
