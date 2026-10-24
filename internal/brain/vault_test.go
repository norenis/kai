package brain

import (
	"os"
	"path/filepath"
	"testing"
)

// writeNote writes a brain note with front matter under dir.
func writeNote(t *testing.T, dir, rel, tags, confidence, updated, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), DirPermissions); err != nil {
		t.Fatal(err)
	}
	content := "---\ntags: [" + tags + "]\nconfidence: " + confidence + "\nsource: direct\nupdated: " + updated + "\n---\n\n" + body + "\n"
	if err := os.WriteFile(p, []byte(content), FilePermissions); err != nil {
		t.Fatal(err)
	}
}

func vaultBrain(t *testing.T) (*FileBrain, string) {
	t.Helper()
	dir := t.TempDir()
	body := "Bank account login details for the savings account and the backup codes list."
	writeNote(t, dir, "vault/bank.md", "bank, finance", "high", "2024-01-01", body)
	writeNote(t, dir, "knowledge/bank.md", "bank, finance", "high", "2024-01-01", body)
	writeNote(t, dir, "knowledge/other.md", "go", "medium", "2026-01-01", "Go services and testing notes for the backend.")
	b := New(dir)
	b.SetEmbedOptions(EmbedOptions{Provider: EmbedLSI})
	if err := b.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	return b, dir
}

func TestMaintenanceNeverTouchesVault(t *testing.T) {
	b, dir := vaultBrain(t)

	plan, err := b.Migrate(DefaultMigrateOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range plan.Actions {
		if touchesVault(a) {
			t.Errorf("migrate proposes %+v", a)
		}
	}

	plan, err = b.Reorganize(ReorgOptions{Mode: ModeAll, SimilarityThreshold: 0.5, SmallFileThreshold: 50, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range plan.Actions {
		if touchesVault(a) {
			t.Errorf("reorganize proposes %+v", a)
		}
	}

	// Even a hand-made plan cannot move a vault note.
	if err := b.ApplyReorgPlan(&ReorgPlan{Actions: []ReorgAction{{Type: ActionMove, SourcePaths: []string{"vault/bank.md"}, TargetPath: "knowledge/moved.md"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "vault", "bank.md")); err != nil {
		t.Errorf("vault note was moved: %v", err)
	}

	results, err := b.Decay(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if IsVaultPath(r.Path) {
			t.Errorf("decay touched %s", r.Path)
		}
	}
	if bf, _ := b.Load("vault/bank.md"); bf == nil || bf.Confidence != ConfidenceHigh {
		t.Error("vault note confidence changed")
	}
}
