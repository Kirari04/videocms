package storage

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

var ErrInvalidKey = errors.New("invalid storage key")

// Key is a validated, slash-separated object key. Keys are always relative to
// the root configured by a storage adapter.
type Key struct {
	value string
}

func ParseKey(value string) (Key, error) {
	if value == "" || strings.ContainsRune(value, 0) || strings.Contains(value, `\`) {
		return Key{}, fmt.Errorf("%w: %q", ErrInvalidKey, value)
	}
	if strings.HasPrefix(value, "/") || path.Clean(value) != value {
		return Key{}, fmt.Errorf("%w: %q", ErrInvalidKey, value)
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return Key{}, fmt.Errorf("%w: %q", ErrInvalidKey, value)
		}
	}
	return Key{value: value}, nil
}

func JoinKey(segments ...string) (Key, error) {
	if len(segments) == 0 {
		return Key{}, fmt.Errorf("%w: no segments", ErrInvalidKey)
	}
	for _, segment := range segments {
		if segment == "" || strings.ContainsAny(segment, `/\`) || segment == "." || segment == ".." {
			return Key{}, fmt.Errorf("%w: segment %q", ErrInvalidKey, segment)
		}
	}
	return ParseKey(strings.Join(segments, "/"))
}

func (k Key) String() string {
	return k.value
}

func (k Key) IsZero() bool {
	return k.value == ""
}
