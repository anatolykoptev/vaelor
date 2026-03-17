package beaut

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/sourcegraph/beaut/lib/knownwf"
)

type AbsolutePath struct {
	rawValue string
}

func NewAbsolutePath(rawValue string) (AbsolutePath, error) {
	if filepath.IsAbs(rawValue) {
		return AbsolutePath{rawValue: rawValue}, nil
	}
	return AbsolutePath{}, NotAbsolutePathError{data: rawValue}
}

func NewAbsolutePathUnchecked(rawValue string, _ knownwf.AbsPathReason) AbsolutePath {
	return AbsolutePath{rawValue: rawValue}
}

func (ap *AbsolutePath) RawValue() string {
	return ap.rawValue
}

var _ fmt.Stringer = &AbsolutePath{}
var _ json.Marshaler = &AbsolutePath{}
var _ json.Unmarshaler = &AbsolutePath{}
var _ semigroupAction[AbsolutePath, RelativePath] = &AbsolutePath{}

func (ap *AbsolutePath) String() string {
	return ap.rawValue
}

func (ap *AbsolutePath) MarshalJSON() ([]byte, error) {
	return json.Marshal(ap.rawValue)
}

func (ap *AbsolutePath) UnmarshalJSON(bytes []byte) error {
	var buf string
	if err := json.Unmarshal(bytes, &buf); err != nil {
		return err
	}
	p, err := NewAbsolutePath(buf)
	if err != nil {
		return err
	}
	*ap = p
	return nil
}

func (ap *AbsolutePath) Combine(other RelativePath) AbsolutePath {
	return NewAbsolutePathUnchecked(filepath.Join(ap.rawValue, other.rawValue), knownwf.AbsPathTypeInvariant)
}

func (ap *AbsolutePath) Join(others ...RelativePath) AbsolutePath {
	bs := make([]string, 0, len(others)+1)
	bs = append(bs, ap.rawValue)
	for _, other := range others {
		bs = append(bs, other.rawValue)
	}
	return NewAbsolutePathUnchecked(filepath.Join(bs...), knownwf.AbsPathTypeInvariant)
}

type NotAbsolutePathError struct{ data string }

var _ error = NotAbsolutePathError{}

func (n NotAbsolutePathError) Error() string {
	suffix := ""
	if len(n.data) > 20 {
		suffix = " (truncated)"
	}
	return fmt.Sprintf("expected absolute path but got: %+.20q%s", n.data, suffix)
}

type RelativePath struct {
	rawValue string
}

func NewRelativePath(rawValue string) (RelativePath, error) {
	if !filepath.IsAbs(rawValue) {
		return RelativePath{rawValue: rawValue}, nil
	}
	return RelativePath{}, NotRelativePathError{rawValue}
}

func NewRelativePathUnchecked(rawValue string, _ knownwf.RelPathReason) RelativePath {
	return RelativePath{rawValue: rawValue}
}

func (rp *RelativePath) RawValue() string {
	return rp.rawValue
}

var _ fmt.Stringer = &RelativePath{}
var _ json.Marshaler = &RelativePath{}
var _ json.Unmarshaler = &RelativePath{}
var _ semigroup[RelativePath] = &RelativePath{}

func (rp *RelativePath) String() string {
	return rp.rawValue
}

func (rp *RelativePath) MarshalJSON() ([]byte, error) {
	return json.Marshal(rp.rawValue)
}

func (rp *RelativePath) UnmarshalJSON(bytes []byte) error {
	var buf string
	if err := json.Unmarshal(bytes, &buf); err != nil {
		return err
	}
	p, err := NewRelativePath(buf)
	if err != nil {
		return err
	}
	*rp = p
	return nil
}

func (rp *RelativePath) Combine(other RelativePath) RelativePath {
	return NewRelativePathUnchecked(filepath.Join(rp.rawValue, other.rawValue), knownwf.RelPathTypeInvariant)
}

func (rp *RelativePath) Join(others ...RelativePath) RelativePath {
	bs := make([]string, 0, len(others)+1)
	bs = append(bs, rp.rawValue)
	for _, other := range others {
		bs = append(bs, other.rawValue)
	}
	return NewRelativePathUnchecked(filepath.Join(bs...), knownwf.RelPathTypeInvariant)
}

type NotRelativePathError struct{ data string }

var _ error = NotRelativePathError{}

func (n NotRelativePathError) Error() string {
	suffix := ""
	if len(n.data) > 20 {
		suffix = " (truncated)"
	}
	return fmt.Sprintf("expected relative path but got: %+.20q%s", n.data, suffix)
}
