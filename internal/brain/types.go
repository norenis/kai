package brain

import (
	"time"
)

// DateFormat is the standard date format used across brain files.
const DateFormat = "2006-01-02"

// Confidence level labels used in brain file frontmatter.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// File system permissions for brain data.
const (
	DirPermissions  = 0755
	FilePermissions = 0644
)

// DefaultCategories are the standard brain knowledge categories.
var DefaultCategories = []string{"identity", "opinions", "style", "decisions", "knowledge", "tasks"}

// DateOnly is a time.Time that marshals/unmarshals as "2006-01-02".
type DateOnly struct {
	time.Time
}

func (d *DateOnly) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	t, err := time.Parse(DateFormat, s)
	if err != nil {
		// Fall back to full RFC3339
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return err
		}
	}
	d.Time = t
	return nil
}

func (d DateOnly) MarshalYAML() (interface{}, error) {
	return d.Time.Format(DateFormat), nil
}

// BrainFile represents a loaded brain file with parsed frontmatter.
type BrainFile struct {
	Path         string   `yaml:"-"`
	Tags         []string `yaml:"tags"`
	Confidence   string   `yaml:"confidence"`
	Source       string   `yaml:"source"`
	Updated      DateOnly `yaml:"updated"`
	AccessCount  int      `yaml:"access_count,omitempty"`
	LastAccessed DateOnly `yaml:"last_accessed,omitempty"`
	Content      string   `yaml:"-"`
}

// Learning represents a piece of knowledge extracted from conversation.
type Learning struct {
	Category   string   `json:"category"`
	Topic      string   `json:"topic"`
	Tags       []string `json:"tags"`
	Content    string   `json:"content"`
	Confidence string   `json:"confidence"`
	Action     string   `json:"action"`
	Source     string   `json:"source,omitempty"`
}

// Index maps tags to file paths for retrieval.
type Index struct {
	Tags map[string][]string `json:"tags"`
}

// FullIndex stores an inverted index for BM25 full-text search.
type FullIndex struct {
	Documents      map[string]DocEntry   `json:"documents"`
	Chunks         map[string]ChunkEntry `json:"chunks,omitempty"`
	InvertedIndex  map[string][]Posting  `json:"inverted_index"`
	TotalDocs      int                   `json:"total_docs"`
	TotalChunks    int                   `json:"total_chunks,omitempty"`
	AvgDocLength   float64               `json:"avg_doc_length"`
	AvgChunkLength float64               `json:"avg_chunk_length,omitempty"`
}

// Posting records a term occurrence in a document field.
type Posting struct {
	Path      string `json:"path"`
	Frequency int    `json:"frequency"`
	Field     string `json:"field"`                // "tag", "content", or "path"
	ChunkKey  string `json:"chunk_key,omitempty"`   // "path#N" for content postings; empty for tag/path
}

// DocEntry stores per-document metadata for the full index.
type DocEntry struct {
	Path        string   `json:"path"`
	Title       string   `json:"title,omitempty"`
	WordCount   int      `json:"word_count"`
	Tags        []string `json:"tags"`
	Updated     string   `json:"updated"`
	AccessCount int      `json:"access_count"`
	ChunkCount  int      `json:"chunk_count,omitempty"`
	Confidence  string   `json:"confidence,omitempty"`
}

// ChunkEntry stores metadata for a single chunk within a document.
type ChunkEntry struct {
	DocPath   string `json:"doc_path"`
	ChunkID   int    `json:"chunk_id"`
	WordCount int    `json:"word_count"`
	Offset    int    `json:"offset"` // character offset into Content
	Length    int    `json:"length"` // character length of chunk
}

// ChunkResult holds a chunk reference and its BM25 score.
type ChunkResult struct {
	DocPath string
	ChunkID int
	Score   float64
	Offset  int
	Length  int
}

// Reorganization action type constants.
const (
	ActionMerge       = "merge"
	ActionMove        = "move"
	ActionConsolidate = "consolidate"
)

// Reorganization mode constants.
const (
	ModeAll          = "all"
	ModeDedup        = "dedup"
	ModeRecategorize = "recategorize"
	ModeConsolidate  = "consolidate"
)

// ReorgAction describes a single proposed reorganization operation.
type ReorgAction struct {
	Type        string   `json:"type"`
	SourcePaths []string `json:"source_paths"`
	TargetPath  string   `json:"target_path"`
	Reason      string   `json:"reason"`
	Similarity  float64  `json:"similarity,omitempty"`
}

// ReorgPlan is a set of proposed reorganization actions.
type ReorgPlan struct {
	Actions []ReorgAction `json:"actions"`
}

// LowConfidenceThresholdDefault is the default confidence below which a
// category suggestion is considered uncertain and flagged for review.
const LowConfidenceThresholdDefault = 0.4

// MigrateOptions controls the behaviour of Migrate.
type MigrateOptions struct {
	DryRun                 bool
	LowConfidenceThreshold float64 // 0–1; suggestions below this are flagged
}

// DefaultMigrateOptions returns sensible defaults (dry-run, threshold 0.4).
func DefaultMigrateOptions() MigrateOptions {
	return MigrateOptions{DryRun: true, LowConfidenceThreshold: LowConfidenceThresholdDefault}
}

// Decay thresholds: days since last update before confidence is downgraded.
const (
	DecayHighToMediumDays = 180
	DecayMediumToLowDays  = 365
)

// DecayResult describes a confidence change applied (or proposed) by Decay.
type DecayResult struct {
	Path      string
	OldConf   string
	NewConf   string
	DaysSince int
}

// ReorgOptions controls reorganization behavior.
type ReorgOptions struct {
	Mode                string  // ModeAll, ModeDedup, ModeRecategorize, ModeConsolidate
	SimilarityThreshold float64 // 0.0-1.0, default 0.7 for dedup
	SmallFileThreshold  int     // word count below which a file is "small", default 50
	DryRun              bool    // if true, return plan without executing
}

// DefaultReorgOptions returns sensible defaults for reorganization.
func DefaultReorgOptions() ReorgOptions {
	return ReorgOptions{
		Mode:                ModeAll,
		SimilarityThreshold: 0.7,
		SmallFileThreshold:  50,
		DryRun:              true,
	}
}
