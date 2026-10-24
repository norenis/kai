package retriever

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/norenis/kai/internal/brain"
)

func setupTestBrain(t *testing.T) *brain.FileBrain {
	t.Helper()
	dir := t.TempDir()
	b := brain.New(dir)

	files := map[string]string{
		"identity/profile.md": `---
tags: [identity, background]
confidence: high
source: direct
updated: 2026-03-25
---

Kyle is a software engineer who prefers Go.
`,
		"opinions/golang.md": `---
tags: [go, languages, backend, cli]
confidence: high
source: conversation
updated: 2026-03-25
---

Kyle prefers Go for CLI tools and backend services.
`,
		"opinions/architecture.md": `---
tags: [architecture, microservices, backend]
confidence: medium
source: conversation
updated: 2026-03-25
---

Kyle prefers monorepos for small teams.
`,
	}

	for relPath, content := range files {
		fullPath := filepath.Join(dir, relPath)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, []byte(content), 0644)
	}

	b.RebuildIndex()
	return b
}

func TestRetrieve(t *testing.T) {
	b := setupTestBrain(t)
	r := New(b, 10)

	results, err := r.Retrieve(context.Background(), "what do I think about Go backend", 5)
	if err != nil {
		t.Fatalf("Retrieve() error: %v", err)
	}

	if len(results) < 1 {
		t.Fatal("expected at least 1 result")
	}

	// identity/profile.md should always be included
	hasProfile := false
	for _, bf := range results {
		if bf.Path == "identity/profile.md" {
			hasProfile = true
		}
	}
	if !hasProfile {
		t.Error("identity/profile.md should always be included")
	}
}

func TestRetrieveStopWords(t *testing.T) {
	b := setupTestBrain(t)
	r := New(b, 10)

	// "the" and "is" should be stripped as stop words
	results, err := r.Retrieve(context.Background(), "the go is great", 5)
	if err != nil {
		t.Fatalf("Retrieve() error: %v", err)
	}

	// Should still find go-related files despite stop words
	found := false
	for _, bf := range results {
		if bf.Path == "opinions/golang.md" {
			found = true
		}
	}
	if !found {
		t.Error("expected to find opinions/golang.md despite stop words")
	}
}

func TestRetrieveNeverReturnsVault(t *testing.T) {
	dir := t.TempDir()
	b := brain.New(dir)
	os.MkdirAll(filepath.Join(dir, "opinions"), 0755)
	os.WriteFile(filepath.Join(dir, "opinions", "golang.md"), []byte("---\ntags: [go, backend]\nconfidence: high\nsource: direct\nupdated: 2026-03-25\n---\n\nGo is great for backend services.\n"), 0644)
	vault := filepath.Join(dir, "vault", "bank.md")
	os.MkdirAll(filepath.Dir(vault), 0755)
	os.WriteFile(vault, []byte("---\ntags: [go, backend, bank]\nconfidence: high\nsource: direct\nupdated: 2026-03-25\n---\n\nGo backend bank account number 0000 for the backend.\n"), 0644)
	if err := b.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	r := New(b, 10)
	for _, q := range []string{"go backend bank account", "bank"} {
		results, err := r.Retrieve(context.Background(), q, 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, bf := range results {
			if brain.IsVaultPath(bf.Path) {
				t.Errorf("query %q returned vault file %s", q, bf.Path)
			}
		}
	}
}
