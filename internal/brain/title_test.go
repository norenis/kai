package brain

import "testing"

func TestTitle(t *testing.T) {
	cases := []struct {
		name, path, content, want string
	}{
		{"heading", "knowledge/go.md", "Intro line.\n\n# Table driven tests\n\nBody", "Table driven tests"},
		{"closing hashes", "knowledge/go.md", "# Context patterns ##\n", "Context patterns"},
		{"vietnamese heading", "knowledge/pho.md", "# Nấu phở bò tại nhà\n", "Nấu phở bò tại nhà"},
		{"front matter then heading", "a/b.md", "---\ntags: [x]\nupdated: 2026-10-01\n---\n\n# From body\n", "From body"},
		{"no heading", "knowledge/usb-c_power-notes.md", "Just text.\n## Section only\n", "Usb c power notes"},
		{"front matter only", "decisions/use-sqlite.md", "---\ntags: [db]\n---\n", "Use sqlite"},
		{"unterminated front matter", "style/naming.md", "---\ntags: [x]\n# not a heading\n", "Naming"},
		{"heading in code fence", "knowledge/md.md", "```md\n# fenced\n```\n# Real title\n", "Real title"},
		{"empty heading", "knowledge/empty-title.md", "# \n#\n", "Empty title"},
		{"date filename", "tasks/2026-10-19.md", "- [ ] item\n", "2026 10 19"},
		{"accented filename", "knowledge/éte.md", "", "Éte"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Title(c.path, c.content); got != c.want {
				t.Errorf("Title(%q) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}

func TestIsVaultPath(t *testing.T) {
	for p, want := range map[string]bool{
		"vault/codes.md":       true,
		"vault":                true,
		"/vault/x.md":          true,
		"knowledge/vault.md":   false,
		"vaults/x.md":          false,
		"knowledge/../vault/x": true,
	} {
		if got := IsVaultPath(p); got != want {
			t.Errorf("IsVaultPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestIndexStoresTitles(t *testing.T) {
	b := New(copyDemoBrain(t))
	b.SetEmbedOptions(EmbedOptions{Provider: EmbedLSI})
	if err := b.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	idx, err := b.LoadFullIndex()
	if err != nil {
		t.Fatal(err)
	}
	doc := idx.Documents["knowledge/table-driven-tests-in-go.md"]
	if doc.Title != "Table driven tests in Go" {
		t.Errorf("indexed title = %q", doc.Title)
	}
	if got := (DocEntry{Path: "knowledge/old-index-entry.md"}).DisplayTitle(); got != "Old index entry" {
		t.Errorf("DisplayTitle fallback = %q", got)
	}
}
