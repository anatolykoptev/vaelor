package filewatcher

import (
	"encoding"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// Op represents a file system operation type.
//
//nolint:recvcheck // UnmarshalText/UnmarshalJSON must have pointer receiver to modify the receiver.
type Op int

// String representations for Op values.
const (
	opStringCreate = "CREATE"
	opStringWrite  = "WRITE"
	opStringRemove = "REMOVE"
	opStringRename = "RENAME"
)

const (
	// Create indicates a file or directory was created.
	Create Op = iota + 1
	// Write indicates a file was modified.
	Write
	// Remove indicates a file or directory was removed.
	Remove
	// Rename indicates a file or directory was renamed.
	Rename
)

// Compile-time interface check: Op implements encoding.TextMarshaler and
// encoding.TextUnmarshaler for JSON, XML, YAML, and other serialization.
var (
	_ encoding.TextMarshaler   = (*Op)(nil)
	_ encoding.TextUnmarshaler = (*Op)(nil)
)

// String returns a human-readable representation of the operation.
func (op Op) String() string {
	switch op {
	case Create:
		return opStringCreate
	case Write:
		return opStringWrite
	case Remove:
		return opStringRemove
	case Rename:
		return opStringRename
	default:
		return fmt.Sprintf("UNKNOWN(%d)", op)
	}
}

// MarshalText implements encoding.TextMarshaler.
func (op Op) MarshalText() ([]byte, error) {
	return []byte(op.String()), nil
}

// MarshalJSON implements json.Marshaler.
func (op Op) MarshalJSON() ([]byte, error) {
	//nolint:wrapcheck // JSON serialization of enum string — no meaningful wrap
	return json.Marshal(op.String())
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (op *Op) UnmarshalText(text []byte) error {
	switch string(text) {
	case opStringCreate:
		*op = Create
	case opStringWrite:
		*op = Write
	case opStringRemove:
		*op = Remove
	case opStringRename:
		*op = Rename
	default:
		return fmt.Errorf("unknown operation: %q: %w", text, ErrUnknownOp)
	}

	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (op *Op) UnmarshalJSON(data []byte) error {
	// Remove surrounding quotes from JSON string
	str := string(data)
	if len(str) >= 2 && str[0] == '"' && str[len(str)-1] == '"' {
		str = str[1 : len(str)-1]
	}

	return op.UnmarshalText([]byte(str))
}

// Event represents a file system change event.
type Event struct {
	// Path is the absolute path of the file or directory that changed.
	Path string `json:"path"`
	// Op is the operation that occurred.
	Op Op `json:"op"`
	// Timestamp is when the event was detected.
	Timestamp time.Time `json:"timestamp"`
	// IsDir indicates whether the event is for a directory (true) or file (false).
	IsDir bool `json:"isDir"`
	// Size is the file size in bytes, if available. Zero if the information
	// could not be obtained (e.g., file already removed, or lazy mode enabled).
	Size int64 `json:"size"`
	// ModTime is the file modification time, if available. Zero time.Time if
	// the information could not be obtained.
	ModTime time.Time `json:"modTime,omitzero"`
	// Hash is the hex-encoded SHA-256 content hash, if available.
	// Only populated when the watcher is created with WithContentHashing().
	// Empty string if hashing is disabled or the file could not be read
	// (e.g., directory, removed file, or permission denied).
	Hash string `json:"hash,omitempty"`
}

// String returns a human-readable representation of the event.
func (e Event) String() string {
	return fmt.Sprintf("%s %s at %s", e.Op, e.Path, e.Timestamp.Format(time.RFC3339))
}

// LogValue implements slog.LogValuer for structured logging.
func (e Event) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("path", e.Path),
		slog.String("op", e.Op.String()),
		slog.Time("timestamp", e.Timestamp),
		slog.Bool("isDir", e.IsDir),
		slog.Int64("size", e.Size),
		slog.Time("modTime", e.ModTime),
		slog.String("hash", e.Hash),
	)
}

// GetPath returns the event path as a phantom type for type-safe usage.
func (e Event) GetPath() EventPath {
	return NewEventPath(e.Path)
}
