package brain

import (
	"os"
	"path/filepath"
	"testing"
)

// demoBrainDir is the synthetic brain shared by Go and Playwright tests.
const demoBrainDir = "../../tests/fixtures/demo-brain"

// copyDemoBrain copies the demo brain fixture into a temp dir so tests never
// write index files into the repository.
func copyDemoBrain(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(demoBrainDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(demoBrainDir, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, DirPermissions)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, FilePermissions)
	})
	if err != nil {
		t.Fatalf("copy demo brain: %v", err)
	}
	return dst
}

func TestDemoBrainReindexesOfflineWithLSI(t *testing.T) {
	b := New(copyDemoBrain(t))
	b.SetEmbedOptions(EmbedOptions{Provider: EmbedLSI})

	if err := b.RebuildIndex(); err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}
	idx, err := b.LoadFullIndex()
	if err != nil || idx == nil {
		t.Fatalf("LoadFullIndex: %v", err)
	}
	if idx.TotalDocs < 80 {
		t.Errorf("TotalDocs = %d, want >= 80", idx.TotalDocs)
	}

	vi, err := b.LoadVecIndex()
	if err != nil || vi == nil {
		t.Fatalf("LoadVecIndex: %v", err)
	}
	if vi.Provider != string(EmbedLSI) {
		t.Errorf("Provider = %q, want lsi", vi.Provider)
	}
	if len(vi.ChunkVecs) != idx.TotalChunks {
		t.Errorf("vectors = %d, chunks = %d; every chunk should be embedded", len(vi.ChunkVecs), idx.TotalChunks)
	}

	files, err := b.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	cats := map[string]bool{}
	for _, f := range files {
		cats[filepath.Dir(f)] = true
	}
	for _, c := range []string{"identity", "opinions", "style", "decisions", "knowledge", "tasks", "vault"} {
		if !cats[c] {
			t.Errorf("demo brain has no %s notes", c)
		}
	}
}
