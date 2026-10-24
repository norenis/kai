package brain

import (
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// VaultCategory is the reserved, PIN-gated brain category. Its notes must not
// appear in lists, search results or visualisations.
const VaultCategory = "vault"

// IsVaultPath reports whether a brain-relative path lies in the vault.
func IsVaultPath(p string) bool {
	root, _, _ := strings.Cut(strings.TrimPrefix(path.Clean("/"+p), "/"), "/")
	return root == VaultCategory
}

// Title returns the display title of a note: its first level-one heading
// ("# Title"), or else the filename in sentence case. content may still
// carry YAML front matter. Headings inside fenced code blocks are ignored.
func Title(p, content string) string {
	inFence := false
	for _, line := range strings.Split(stripFrontMatter(content), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(t, "# ") {
			h := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(t[2:]), "#"))
			if h != "" {
				return h
			}
		}
	}
	return FilenameTitle(p)
}

// FilenameTitle turns "knowledge/usb-c_power-notes.md" into "Usb c power notes".
func FilenameTitle(p string) string {
	name := strings.TrimSuffix(path.Base(p), ".md")
	name = strings.Join(strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' }), " ")
	if name == "" {
		return p
	}
	r, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(r)) + name[size:]
}

// DisplayTitle returns the indexed title, falling back to the filename for
// indexes built before titles were stored.
func (d DocEntry) DisplayTitle() string {
	if d.Title != "" {
		return d.Title
	}
	return FilenameTitle(d.Path)
}

func stripFrontMatter(content string) string {
	s := strings.TrimLeft(content, "\ufeff")
	if !strings.HasPrefix(s, "---") {
		return content
	}
	rest := s[3:]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 || strings.TrimSpace(rest[:nl]) != "" {
		return content
	}
	rest = rest[nl+1:]
	for {
		nl = strings.IndexByte(rest, '\n')
		line := rest
		if nl >= 0 {
			line = rest[:nl]
		}
		if strings.TrimSpace(line) == "---" {
			if nl < 0 {
				return ""
			}
			return rest[nl+1:]
		}
		if nl < 0 {
			return "" // unterminated front matter: nothing but metadata
		}
		rest = rest[nl+1:]
	}
}

// BackfillTitles stores titles for index entries written before titles
// existed, so upgraded brains show real titles without a full reindex. It
// reads each affected note once and only rewrites the BM25 index; vectors
// are untouched. Returns how many entries were filled.
func (b *FileBrain) BackfillTitles() (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	idx, err := b.loadFullIndex()
	if err != nil || idx == nil {
		return 0, err
	}
	filled := 0
	for p, d := range idx.Documents {
		if d.Title != "" {
			continue
		}
		bf, err := b.load(p)
		if err != nil {
			continue
		}
		d.Title = Title(p, bf.Content)
		idx.Documents[p] = d
		filled++
	}
	if filled == 0 {
		return 0, nil
	}
	if err := b.saveFullIndex(idx); err != nil {
		return 0, err
	}
	b.indexCache = idx
	b.indexCacheAt = time.Now()
	return filled, nil
}
