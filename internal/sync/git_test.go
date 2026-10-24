package sync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushUpgradesOldGitIgnore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	ctx := context.Background()
	g := NewGitBackend(dir, "", "main")
	run := func(args ...string) string {
		t.Helper()
		out, err := g.git(ctx, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	// A brain synced by an older version: its .gitignore predates
	// clusters.json, which therefore got committed.
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("config", "commit.gpgsign", "false")
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("index.json\nvectors.bin.gz\nmy-scratch/\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "knowledge"), 0o755)
	os.WriteFile(filepath.Join(dir, "knowledge", "a.md"), []byte("# A\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "clusters.json"), []byte("{}"), 0o644)
	run("add", "-A")
	run("commit", "-q", "-m", "old sync")

	if err := g.Push(ctx, dir); err != nil {
		t.Fatalf("Push: %v", err)
	}

	ignore, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	for _, want := range []string{"my-scratch/", "clusters.json", "index_full.json.gz"} {
		if !strings.Contains(string(ignore), want+"\n") {
			t.Errorf(".gitignore missing %q:\n%s", want, ignore)
		}
	}
	if strings.Count(string(ignore), "vectors.bin.gz") != 1 {
		t.Errorf("duplicate entries:\n%s", ignore)
	}
	if tracked := run("ls-files"); strings.Contains(tracked, "clusters.json") {
		t.Errorf("clusters.json still tracked:\n%s", tracked)
	}
	if _, err := os.Stat(filepath.Join(dir, "clusters.json")); err != nil {
		t.Error("clusters.json was deleted from disk")
	}
	if !strings.Contains(run("ls-files"), "knowledge/a.md") {
		t.Error("notes must stay tracked")
	}
}
