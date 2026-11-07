package markdown

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Key returns the matching key for s: NFC-normalized and lowercased. It is the
// single source of truth for name matching (links.Key delegates to it).
func Key(s string) string { return strings.ToLower(norm.NFC.String(s)) }
