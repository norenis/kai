package brain

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSnippetNeverSplitsRunes(t *testing.T) {
	texts := []string{
		"Nước dùng phở cần xương bò ninh lâu, ít nhất sáu tiếng, vớt bọt thường xuyên để nước trong. Gừng và hành nướng cháy vỏ.",
		"Cà phê phin ☕ cần bột xay vừa 🙂🙂🙂 không quá mịn để nước chảy đều từng giọt, rót đầy nước và đậy nắp.",
		"日本語の文章も途中で切れてはいけません。漢字とひらがなとカタカナ。",
		"plain ascii text with words that goes on for a while and then some more words at the end",
	}
	for _, text := range texts {
		for max := 1; max < utf8.RuneCountInString(text)+3; max++ {
			for off := 0; off < len(text); off += 3 {
				s := Snippet(text, off, len(text)-off, max)
				if !utf8.ValidString(s) || strings.ContainsRune(s, utf8.RuneError) {
					t.Fatalf("invalid UTF-8 for max=%d off=%d: %q", max, off, s)
				}
				if n := utf8.RuneCountInString(strings.TrimSuffix(s, "…")); n > max {
					t.Fatalf("snippet has %d runes, max %d: %q", n, max, s)
				}
			}
		}
	}
}

func TestSnippetPrefersWordBoundary(t *testing.T) {
	s := Snippet("Nước dùng phở cần xương bò ninh lâu", 0, 0, 20)
	if s != "Nước dùng phở cần…" {
		t.Errorf("got %q", s)
	}
	if got := Snippet("short text", 0, 0, 200); got != "short text" {
		t.Errorf("got %q", got)
	}
	// Offsets inside a character move to its start.
	text := "ăn phở"
	if got := Snippet(text, 1, len(text)-1, 100); got != text {
		t.Errorf("mid-rune offset: %q", got)
	}
	// Out-of-range offset falls back to the whole text.
	if got := Snippet("abc", 99, 5, 100); got != "abc" {
		t.Errorf("bad offset: %q", got)
	}
}
