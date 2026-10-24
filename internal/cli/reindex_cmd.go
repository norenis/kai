package cli

import (
	"fmt"
	"log/slog"

	"github.com/norenis/kai/internal/brain"
	"github.com/spf13/cobra"
)

func newReindexCmd() *cobra.Command {
	var full bool

	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the BM25 and vector search indexes",
		Long:  "Scan brain files and rebuild the full-text BM25 index and vector embeddings (for hybrid search). By default only changed files are re-indexed (incremental). Use --full to force a complete rebuild from scratch.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			brainPath := cfg.Brain.Path
			if brainPath == "" {
				brainPath = getBrainPath()
			}

			b := brain.New(brainPath)

			// Apply embedding config.
			applyEmbedConfig(b, cfg)

			fmt.Printf("Rebuilding indexes for brain at: %s\n", brainPath)
			if full {
				err = b.RebuildIndex()
			} else {
				err = b.RebuildIndexIncremental()
			}
			if err != nil {
				return fmt.Errorf("rebuild index failed: %w", err)
			}
			// An explicit reindex is when constellations are recomputed.
			if err := b.RecomputeClusters(); err != nil {
				return fmt.Errorf("recompute clusters: %w", err)
			}

			fmt.Println("✓ Indexes rebuilt successfully")
			slog.Info("indexes rebuilt", "brain", brainPath)

			// Report index stats and diagnose vector issues
			idx, err := b.LoadFullIndex()
			if err == nil && idx != nil {
				fmt.Printf("  Documents: %d\n", idx.TotalDocs)
				fmt.Printf("  Chunks: %d\n", idx.TotalChunks)
				fmt.Printf("  Inverted index terms: %d\n", len(idx.InvertedIndex))

				vi, err := b.LoadVecIndex()
				if err == nil && vi != nil {
					fmt.Printf("  Vector embeddings: %d chunks\n", len(vi.ChunkVecs))
					fmt.Printf("  Vector provider: %s\n", vi.Provider)
				} else {
					// Diagnose why vectors weren't created
					fmt.Printf("  Vector embeddings: ❌ NOT created\n")

					provider := cfg.Brain.Embeddings.Provider
					if provider == "" {
						provider = "auto (LSI default)"
					}
					fmt.Printf("    Provider: %s\n", provider)

					// Check document count (LSI needs >=5)
					if idx.TotalDocs < 5 {
						fmt.Printf("    ⚠️  Reason: LSI requires ≥5 documents, you have %d\n", idx.TotalDocs)
						fmt.Printf("    → Add more brain files, then run 'kai reindex' again\n")
					} else if provider == "ollama" {
						fmt.Printf("    ⚠️  Reason: Ollama is not available\n")
						ollamaURL := cfg.Brain.Embeddings.Ollama.URL
						if ollamaURL == "" {
							ollamaURL = "http://localhost:11434"
						}
						fmt.Printf("    → Start Ollama at %s, then run 'kai reindex' again\n", ollamaURL)
					} else {
						fmt.Printf("    ⚠️  Reason: Unknown (check logs)\n")
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&full, "full", false, "Force a complete rebuild, ignoring incremental state")
	return cmd
}
