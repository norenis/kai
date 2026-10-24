package brain

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/adrg/frontmatter"
	"gopkg.in/yaml.v3"
)

// EmbedProvider specifies which embedding backend to use.
type EmbedProvider string

const (
	EmbedAuto   EmbedProvider = ""       // Try ollama, fall back to LSI.
	EmbedOllama EmbedProvider = "ollama" // Use Ollama embedding model.
	EmbedGemini EmbedProvider = "gemini" // Use Google Gemini embedding API.
	EmbedLSI    EmbedProvider = "lsi"    // Use LSI (corpus-derived, no external deps).
)

// EmbedOptions holds embedding configuration for the brain.
type EmbedOptions struct {
	Provider  EmbedProvider
	OllamaURL   string
	OllamaModel string
	GeminiAPIKey  string
	GeminiModel   string
	GeminiBaseURL string
}

const indexCacheTTL = 5 * time.Second

// FileBrain is the file-based Brain implementation.
type FileBrain struct {
	root      string
	mu        sync.RWMutex
	embed     EmbedOptions
	fileCache map[string]*BrainFile // transient, populated during rebuild only

	// TTL caches — avoid repeated gzip decompression on rapid dashboard reads.
	indexCache   *FullIndex
	indexCacheAt time.Time
	vecCache     *VecIndex
	vecCacheAt   time.Time

	// Constellations, valid for the vector/full index they were derived from.
	clMu       sync.Mutex
	clCache    *ClusterSet
	clCacheFor *VecIndex
	clCacheIdx *FullIndex
}

// New creates a new FileBrain rooted at the given directory.
func New(root string) *FileBrain {
	return &FileBrain{root: root}
}

// SetEmbedOptions configures the embedding backend.
func (b *FileBrain) SetEmbedOptions(opts EmbedOptions) {
	b.embed = opts
}

// safePath validates that relPath stays within the brain root directory.
func (b *FileBrain) safePath(relPath string) (string, error) {
	fullPath := filepath.Join(b.root, relPath)
	absRoot, err := filepath.Abs(b.root)
	if err != nil {
		return "", fmt.Errorf("resolve brain root: %w", err)
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) && absPath != absRoot {
		return "", fmt.Errorf("path %q escapes brain directory", relPath)
	}
	return fullPath, nil
}

// load is the internal unlocked implementation of Load.
func (b *FileBrain) load(relPath string) (*BrainFile, error) {
	fullPath, err := b.safePath(relPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("read brain file %s: %w", relPath, err)
	}

	var bf BrainFile
	rest, err := frontmatter.Parse(bytes.NewReader(data), &bf)
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter %s: %w", relPath, err)
	}

	bf.Path = relPath
	bf.Content = string(rest)
	return &bf, nil
}

// loadCached returns a brain file from the transient rebuild cache if available,
// otherwise falls back to load(). Safe to call when fileCache is nil.
func (b *FileBrain) loadCached(relPath string) (*BrainFile, error) {
	if b.fileCache != nil {
		if bf, ok := b.fileCache[relPath]; ok {
			return bf, nil
		}
		bf, err := b.load(relPath)
		if err != nil {
			return nil, err
		}
		b.fileCache[relPath] = bf
		return bf, nil
	}
	return b.load(relPath)
}

// Load reads and parses a brain file by relative path.
func (b *FileBrain) Load(relPath string) (*BrainFile, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.load(relPath)
}

// save is the internal unlocked implementation of Save.
func (b *FileBrain) save(bf *BrainFile) error {
	fullPath, err := b.safePath(bf.Path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, DirPermissions); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")

	fm := struct {
		Tags         []string `yaml:"tags"`
		Confidence   string   `yaml:"confidence"`
		Source       string   `yaml:"source"`
		Updated      string   `yaml:"updated"`
		AccessCount  int      `yaml:"access_count,omitempty"`
		LastAccessed string   `yaml:"last_accessed,omitempty"`
	}{
		Tags:       bf.Tags,
		Confidence: bf.Confidence,
		Source:     bf.Source,
		Updated:    bf.Updated.Time.Format(DateFormat),
		AccessCount: bf.AccessCount,
	}
	if !bf.LastAccessed.Time.IsZero() {
		fm.LastAccessed = bf.LastAccessed.Time.Format(DateFormat)
	}

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return fmt.Errorf("encode frontmatter: %w", err)
	}
	enc.Close()
	buf.WriteString("---\n")
	buf.WriteString(strings.TrimLeft(bf.Content, "\n"))

	tmpPath := fullPath + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), FilePermissions); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmpPath, fullPath); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, fullPath, err)
	}

	return nil
}

// Save writes a brain file to disk with frontmatter. Creates directories as needed.
func (b *FileBrain) Save(bf *BrainFile) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.indexCache = nil // invalidate — file content changed
	return b.save(bf)
}

// loadFullIndexCached returns the cached full index if fresh, otherwise loads from disk.
// Must be called with at least a read lock held.
func (b *FileBrain) loadFullIndexCached() (*FullIndex, error) {
	if b.indexCache != nil && time.Since(b.indexCacheAt) < indexCacheTTL {
		return b.indexCache, nil
	}
	idx, err := b.loadFullIndex()
	if err != nil {
		return nil, err
	}
	b.indexCache = idx
	b.indexCacheAt = time.Now()
	return idx, nil
}

// LoadFullIndexCached returns the full index, using a short-lived cache to
// avoid repeated gzip decompression across concurrent dashboard handlers.
func (b *FileBrain) LoadFullIndexCached() (*FullIndex, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.loadFullIndexCached()
}

// loadVecIndexCached returns the cached vec index if fresh, otherwise loads from disk.
func (b *FileBrain) loadVecIndexCached() (*VecIndex, error) {
	if b.vecCache != nil && time.Since(b.vecCacheAt) < indexCacheTTL {
		return b.vecCache, nil
	}
	vi, err := b.loadVecIndex()
	if err != nil {
		return nil, err
	}
	b.vecCache = vi
	b.vecCacheAt = time.Now()
	return vi, nil
}

// LoadVecIndexCached returns the vec index, using a short-lived cache.
func (b *FileBrain) LoadVecIndexCached() (*VecIndex, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.loadVecIndexCached()
}

// listAll is the internal unlocked implementation of ListAll.
func (b *FileBrain) listAll() ([]string, error) {
	var files []string
	trashPrefix := filepath.Join(b.root, TrashDir) + string(filepath.Separator)
	err := filepath.Walk(b.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if path == b.root {
				return fmt.Errorf("access brain directory %s: %w", b.root, err)
			}
			return nil
		}
		// Skip the trash directory entirely.
		if info.IsDir() && strings.HasPrefix(path+string(filepath.Separator), trashPrefix) {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			rel, err := filepath.Rel(b.root, path)
			if err != nil {
				return nil
			}
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}

// ListAll returns all .md files in the brain directory.
func (b *FileBrain) ListAll() ([]string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.listAll()
}

// ValidCategory reports whether cat is one of the allowed root brain categories.
func ValidCategory(cat string) bool {
	for _, c := range DefaultCategories {
		if c == cat {
			return true
		}
	}
	return false
}

// Learn applies extracted learnings to the brain.
func (b *FileBrain) Learn(learnings []Learning) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, l := range learnings {
		if !ValidCategory(l.Category) {
			slog.Warn("learning skipped: invalid category", "category", l.Category, "topic", l.Topic)
			continue
		}
		relPath := filepath.Join(l.Category, l.Topic+".md")

		switch l.Action {
		case "create":
			bf := &BrainFile{
				Path:       relPath,
				Tags:       l.Tags,
				Confidence: l.Confidence,
				Source:     "conversation",
				Updated:    DateOnly{time.Now()},
				Content:    "\n" + l.Content + "\n",
			}
			if err := b.save(bf); err != nil {
				return fmt.Errorf("create %s: %w", relPath, err)
			}

		case "append":
			existing, err := b.load(relPath)
			if err != nil {
				// File doesn't exist yet, create it instead.
				bf := &BrainFile{
					Path:       relPath,
					Tags:       l.Tags,
					Confidence: l.Confidence,
					Source:     "conversation",
					Updated:    DateOnly{time.Now()},
					Content:    "\n" + l.Content + "\n",
				}
				if err := b.save(bf); err != nil {
					return fmt.Errorf("create %s: %w", relPath, err)
				}
				continue
			}
			// Dedup: skip if leading text of new content already appears verbatim.
			newSnippet := strings.ToLower(strings.TrimSpace(l.Content))
			if len(newSnippet) > 60 {
				newSnippet = newSnippet[:60]
			}
			if newSnippet != "" && strings.Contains(strings.ToLower(existing.Content), newSnippet) {
				continue
			}
			existing.Content = strings.TrimRight(existing.Content, "\n") + "\n\n" + l.Content + "\n"
			existing.Tags = mergeTags(existing.Tags, l.Tags)
			existing.Updated = DateOnly{time.Now()}
			if err := b.save(existing); err != nil {
				return fmt.Errorf("append %s: %w", relPath, err)
			}

		case "replace":
			bf := &BrainFile{
				Path:       relPath,
				Tags:       l.Tags,
				Confidence: l.Confidence,
				Source:     "conversation",
				Updated:    DateOnly{time.Now()},
				Content:    "\n" + l.Content + "\n",
			}
			if err := b.save(bf); err != nil {
				return fmt.Errorf("replace %s: %w", relPath, err)
			}
		}
	}
	return nil
}

// Touch increments the access count and updates the last accessed date for a brain file.
func (b *FileBrain) Touch(relPath string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.touch(relPath)
}

// touch is the internal unlocked implementation of Touch.
func (b *FileBrain) touch(relPath string) error {
	bf, err := b.load(relPath)
	if err != nil {
		return err
	}
	bf.AccessCount++
	bf.LastAccessed = DateOnly{time.Now()}
	return b.save(bf)
}

// Decay lowers confidence on brain files that haven't been updated in a while.
// high → medium after DecayHighToMediumDays; medium → low after DecayMediumToLowDays.
// If dryRun is true, files are not modified.
func (b *FileBrain) Decay(dryRun bool) ([]DecayResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	files, err := b.listAll()
	if err != nil {
		return nil, err
	}

	var results []DecayResult
	now := time.Now()

	for _, relPath := range files {
		// Vault notes are kept verbatim; their confidence does not decay.
		if IsVaultPath(relPath) {
			continue
		}
		bf, err := b.load(relPath)
		if err != nil {
			continue
		}

		daysSince := int(now.Sub(bf.Updated.Time).Hours() / 24)
		newConf := bf.Confidence

		switch bf.Confidence {
		case ConfidenceHigh:
			if daysSince >= DecayHighToMediumDays {
				newConf = ConfidenceMedium
			}
		case ConfidenceMedium:
			if daysSince >= DecayMediumToLowDays {
				newConf = ConfidenceLow
			}
		}

		if newConf == bf.Confidence {
			continue
		}

		results = append(results, DecayResult{
			Path:      relPath,
			OldConf:   bf.Confidence,
			NewConf:   newConf,
			DaysSince: daysSince,
		})

		if !dryRun {
			bf.Confidence = newConf
			if err := b.save(bf); err != nil {
				return results, fmt.Errorf("save %s: %w", relPath, err)
			}
		}
	}

	return results, nil
}

func mergeTags(existing, new []string) []string {
	seen := make(map[string]bool)
	for _, t := range existing {
		seen[t] = true
	}
	merged := append([]string{}, existing...)
	for _, t := range new {
		if !seen[t] {
			merged = append(merged, t)
		}
	}
	return merged
}
