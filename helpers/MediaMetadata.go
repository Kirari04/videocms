package helpers

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SafeMediaLabel uses a generated label when container metadata cannot be
// represented safely in an HLS quoted attribute or exceeds its storage bound.
// Valid Unicode names (including commas inside quoted attributes) are kept.
func SafeMediaLabel(value, fallback string, maxRunes int) string {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRunes || strings.ContainsRune(value, '"') {
		return fallback
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fallback
		}
	}
	return value
}
