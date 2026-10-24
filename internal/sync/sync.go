package sync

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// FileState describes a file's sync state.
type FileState struct {
	Path    string
	ModTime time.Time
	Size    int64
}

// Status represents the sync comparison between local and remote.
type Status struct {
	LocalOnly  []string // files only on local
	RemoteOnly []string // files only on remote
	Modified   []string // files that differ
	InSync     []string // files that match
}

// Backend is the interface all sync backends implement.
type Backend interface {
	Name() string
	Init(ctx context.Context) error
	Push(ctx context.Context, brainPath string) error
	Pull(ctx context.Context, brainPath string) error
	Status(ctx context.Context, brainPath string) (*Status, error)
}

// Syncer orchestrates sync across git and cloud backends.
type Syncer struct {
	brainPath string
	git       Backend
	cloud     Backend
}

// NewSyncer creates a Syncer. Either backend may be nil (disabled).
func NewSyncer(brainPath string, git, cloud Backend) *Syncer {
	return &Syncer{brainPath: brainPath, git: git, cloud: cloud}
}

// Init initializes all enabled backends.
func (s *Syncer) Init(ctx context.Context) error {
	if s.git != nil {
		if err := s.git.Init(ctx); err != nil {
			return fmt.Errorf("git init: %w", err)
		}
	}
	if s.cloud != nil {
		if err := s.cloud.Init(ctx); err != nil {
			return fmt.Errorf("cloud init: %w", err)
		}
	}
	return nil
}

// Push syncs local brain to all enabled backends.
func (s *Syncer) Push(ctx context.Context, gitOnly, cloudOnly bool) error {
	if s.git != nil && !cloudOnly {
		fmt.Printf("Pushing to %s...\n", s.git.Name())
		if err := s.git.Push(ctx, s.brainPath); err != nil {
			return fmt.Errorf("git push: %w", err)
		}
		fmt.Printf("  %s push complete.\n", s.git.Name())
	}
	if s.cloud != nil && !gitOnly {
		fmt.Printf("Pushing to %s...\n", s.cloud.Name())
		if err := s.cloud.Push(ctx, s.brainPath); err != nil {
			return fmt.Errorf("cloud push: %w", err)
		}
		fmt.Printf("  %s push complete.\n", s.cloud.Name())
	}
	return nil
}

// Pull syncs remote brain to local from enabled backends.
func (s *Syncer) Pull(ctx context.Context, gitOnly, cloudOnly bool) error {
	if s.git != nil && !cloudOnly {
		fmt.Printf("Pulling from %s...\n", s.git.Name())
		if err := s.git.Pull(ctx, s.brainPath); err != nil {
			return fmt.Errorf("git pull: %w", err)
		}
		fmt.Printf("  %s pull complete.\n", s.git.Name())
	}
	if s.cloud != nil && !gitOnly {
		fmt.Printf("Pulling from %s...\n", s.cloud.Name())
		if err := s.cloud.Pull(ctx, s.brainPath); err != nil {
			return fmt.Errorf("cloud pull: %w", err)
		}
		fmt.Printf("  %s pull complete.\n", s.cloud.Name())
	}
	return nil
}

// StatusAll returns status from all enabled backends.
func (s *Syncer) StatusAll(ctx context.Context) (gitStatus, cloudStatus *Status, err error) {
	if s.git != nil {
		gitStatus, err = s.git.Status(ctx, s.brainPath)
		if err != nil {
			return nil, nil, fmt.Errorf("git status: %w", err)
		}
	}
	if s.cloud != nil {
		cloudStatus, err = s.cloud.Status(ctx, s.brainPath)
		if err != nil {
			return gitStatus, nil, fmt.Errorf("cloud status: %w", err)
		}
	}
	return gitStatus, cloudStatus, nil
}

// HasBackends returns true if at least one backend is configured.
func (s *Syncer) HasBackends() bool {
	return s.git != nil || s.cloud != nil
}

// skipPaths are directories/files excluded from cloud sync.
var skipPrefixes = []string{".trash/", ".trash\\"}
var skipFiles = map[string]bool{
	".gitignore":        true,
	".DS_Store":         true,
	"index_full.json.gz": true,
	"vectors.bin.gz":     true,
	"clusters.json":      true,
}

// shouldSkipSync returns true if a relative path should be excluded from sync.
func shouldSkipSync(rel string) bool {
	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return skipFiles[rel]
}
