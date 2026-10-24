package sync

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// brainGitIgnore lists files excluded from brain git sync (derived data).
const brainGitIgnore = `index.json
index_full.json
index_full.json.gz
vectors.bin.gz
clusters.json
`

// derivedFiles lists the entries of brainGitIgnore.
func derivedFiles() []string {
	return strings.Fields(brainGitIgnore)
}

// ensureGitIgnore makes the brain's .gitignore list every derived file,
// keeping any lines the user added.
func (g *GitBackend) ensureGitIgnore() error {
	ignorePath := filepath.Join(g.brainPath, ".gitignore")
	existing, err := os.ReadFile(ignorePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read .gitignore: %w", err)
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, f := range derivedFiles() {
		if !have[f] {
			missing = append(missing, f)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += strings.Join(missing, "\n") + "\n"
	if err := os.WriteFile(ignorePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}
	return nil
}

// GitBackend syncs the brain directory using a nested git repository.
type GitBackend struct {
	brainPath string
	remote    string
	branch    string
}

// NewGitBackend creates a git sync backend.
func NewGitBackend(brainPath, remote, branch string) *GitBackend {
	if branch == "" {
		branch = "main"
	}
	return &GitBackend{brainPath: brainPath, remote: remote, branch: branch}
}

func (g *GitBackend) Name() string { return "git" }

// Init initializes a git repo inside the brain directory.
func (g *GitBackend) Init(ctx context.Context) error {
	gitDir := filepath.Join(g.brainPath, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		fmt.Println("Git repo already initialized in brain directory.")
	} else {
		if _, err := g.git(ctx, "init", "-b", g.branch); err != nil {
			return fmt.Errorf("git init: %w", err)
		}
		fmt.Println("Initialized git repo in brain directory.")
	}

	if err := g.ensureGitIgnore(); err != nil {
		return err
	}

	// Set up remote if configured.
	if g.remote != "" {
		// Check if remote exists.
		out, _ := g.git(ctx, "remote")
		if strings.Contains(out, "origin") {
			if _, err := g.git(ctx, "remote", "set-url", "origin", g.remote); err != nil {
				return fmt.Errorf("set remote: %w", err)
			}
		} else {
			if _, err := g.git(ctx, "remote", "add", "origin", g.remote); err != nil {
				return fmt.Errorf("add remote: %w", err)
			}
		}
		fmt.Printf("Remote set to %s\n", g.remote)
	}

	return nil
}

// Push commits all changes and pushes to remote (if configured).
func (g *GitBackend) Push(ctx context.Context, brainPath string) error {
	// Brains initialised by older versions miss newer derived files.
	if err := g.ensureGitIgnore(); err != nil {
		return err
	}
	// Stop tracking derived files that an older .gitignore let through.
	untrack := append([]string{"rm", "--cached", "--quiet", "--ignore-unmatch", "--"}, derivedFiles()...)
	if _, err := g.git(ctx, untrack...); err != nil {
		return fmt.Errorf("git rm --cached: %w", err)
	}

	// Stage all changes.
	if _, err := g.git(ctx, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// Check if there are changes to commit.
	status, err := g.git(ctx, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(status) == "" {
		fmt.Println("  No changes to commit.")
	} else {
		// Count changed files.
		lines := strings.Split(strings.TrimSpace(status), "\n")
		msg := fmt.Sprintf("brain sync: %d file(s) changed", len(lines))
		if _, err := g.git(ctx, "commit", "-m", msg); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}
		fmt.Printf("  Committed: %s\n", msg)
	}

	// Push to remote if configured.
	if g.remote != "" {
		if _, err := g.git(ctx, "push", "-u", "origin", g.branch); err != nil {
			return fmt.Errorf("git push: %w", err)
		}
		fmt.Println("  Pushed to remote.")
	}

	return nil
}

// Pull fetches from remote and merges.
func (g *GitBackend) Pull(ctx context.Context, brainPath string) error {
	if g.remote == "" {
		fmt.Println("  No remote configured, skipping pull.")
		return nil
	}

	if _, err := g.git(ctx, "pull", "origin", g.branch); err != nil {
		return fmt.Errorf("git pull: %w", err)
	}
	return nil
}

// Status shows uncommitted changes and unpushed commits.
func (g *GitBackend) Status(ctx context.Context, brainPath string) (*Status, error) {
	gitDir := filepath.Join(g.brainPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("brain git repo not initialized — run 'kai brain sync init'")
	}

	s := &Status{}

	// Get working tree changes.
	out, err := g.git(ctx, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		code := line[:2]
		file := strings.TrimSpace(line[2:])
		switch {
		case strings.Contains(code, "?"):
			s.LocalOnly = append(s.LocalOnly, file)
		case strings.Contains(code, "D"):
			s.RemoteOnly = append(s.RemoteOnly, file)
		default:
			s.Modified = append(s.Modified, file)
		}
	}

	return s, nil
}

// git runs a git command in the brain directory and returns stdout.
func (g *GitBackend) git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.brainPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
