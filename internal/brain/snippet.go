package brain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Snippet returns up to maxRunes characters of a chunk of content (offset and
// length in bytes, as stored in the index). It never splits a UTF-8
// character: chunk bounds are moved to character boundaries, and a long
// snippet is cut at the last word break within the limit, then marked "…".
func Snippet(content string, offset, length, maxRunes int) string {
	start, end := 0, len(content)
	if offset >= 0 && offset < len(content) {
		start = offset
		if length > 0 && offset+length < len(content) {
			end = offset + length
		}
	}
	for start > 0 && !utf8.RuneStart(content[start]) {
		start--
	}
	for end < len(content) && !utf8.RuneStart(content[end]) {
		end++
	}
	s := strings.TrimSpace(content[start:end])
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}

	// Byte index just past maxRunes characters.
	cut, n := 0, 0
	for i := range s {
		if n == maxRunes {
			cut = i
			break
		}
		n++
	}
	head := s[:cut]
	// Prefer a word boundary if one falls in the last third.
	if i := strings.LastIndexFunc(head, unicode.IsSpace); i > len(head)*2/3 {
		head = head[:i]
	}
	return strings.TrimRightFunc(head, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) }) + "…"
}
