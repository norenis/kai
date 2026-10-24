package brain

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Compact wire format for the full index. Reduces JSON size by:
// - Path table: numeric IDs instead of repeating full path strings
// - Array postings: [pathIdx, freq, fieldCode, chunkID] instead of objects
// - Short JSON keys throughout
// - Gzip compression on disk

// compactWire is the on-disk representation of FullIndex.
type compactWire struct {
	P   []string              `json:"p"`   // path table: index → path
	D   []compactDocW         `json:"d"`   // documents (ordered by path table index)
	C   []compactChunkW       `json:"c"`   // chunks (flat list)
	I   map[string][][4]int   `json:"i"`   // inverted index: term → [[pathIdx, freq, field, chunkID], ...]
	TD  int                   `json:"td"`  // total docs
	TC  int                   `json:"tc"`  // total chunks
	ADL float64               `json:"adl"` // avg doc length
	ACL float64               `json:"acl"` // avg chunk length
}

// Field codes for compact postings.
const (
	fieldContent = 0
	fieldTag     = 1
	fieldPath    = 2
)

type compactDocW struct {
	W  int      `json:"w"`            // word count
	T  []string `json:"t,omitempty"`  // tags
	U  string   `json:"u"`            // updated
	A  int      `json:"a,omitempty"`  // access count
	CC int      `json:"cc,omitempty"` // chunk count
	Cf string   `json:"cf,omitempty"` // confidence
	Ti string   `json:"ti,omitempty"` // title
}

type compactChunkW struct {
	P int `json:"p"` // path index
	I int `json:"i"` // chunk ID within document
	W int `json:"w"` // word count
	O int `json:"o"` // character offset
	L int `json:"l"` // character length
}

// toCompact converts a FullIndex to the compact wire format.
func toCompact(idx *FullIndex) *compactWire {
	// Build path table (sorted for deterministic output).
	paths := make([]string, 0, len(idx.Documents))
	for p := range idx.Documents {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	pathIdx := make(map[string]int, len(paths))
	for i, p := range paths {
		pathIdx[p] = i
	}

	// Convert documents (ordered by path table index).
	docs := make([]compactDocW, len(paths))
	for i, p := range paths {
		d := idx.Documents[p]
		docs[i] = compactDocW{
			W:  d.WordCount,
			T:  d.Tags,
			U:  d.Updated,
			A:  d.AccessCount,
			CC: d.ChunkCount,
			Cf: d.Confidence,
			Ti: d.Title,
		}
	}

	// Convert chunks.
	chunks := make([]compactChunkW, 0, len(idx.Chunks))
	for _, ce := range idx.Chunks {
		chunks = append(chunks, compactChunkW{
			P: pathIdx[ce.DocPath],
			I: ce.ChunkID,
			W: ce.WordCount,
			O: ce.Offset,
			L: ce.Length,
		})
	}

	// Convert inverted index.
	inv := make(map[string][][4]int, len(idx.InvertedIndex))
	for term, postings := range idx.InvertedIndex {
		compact := make([][4]int, len(postings))
		for i, p := range postings {
			fc := fieldContent
			switch p.Field {
			case "tag":
				fc = fieldTag
			case "path":
				fc = fieldPath
			}
			chunkID := -1
			if p.ChunkKey != "" {
				// Extract chunk ID from "path#N"
				if idx := strings.LastIndex(p.ChunkKey, "#"); idx >= 0 {
					if id, err := strconv.Atoi(p.ChunkKey[idx+1:]); err == nil {
						chunkID = id
					}
				}
			}
			compact[i] = [4]int{pathIdx[p.Path], p.Frequency, fc, chunkID}
		}
		inv[term] = compact
	}

	return &compactWire{
		P:   paths,
		D:   docs,
		C:   chunks,
		I:   inv,
		TD:  idx.TotalDocs,
		TC:  idx.TotalChunks,
		ADL: idx.AvgDocLength,
		ACL: idx.AvgChunkLength,
	}
}

// fromCompact converts the compact wire format back to a FullIndex.
func fromCompact(cw *compactWire) *FullIndex {
	idx := &FullIndex{
		Documents:      make(map[string]DocEntry, len(cw.D)),
		Chunks:         make(map[string]ChunkEntry, len(cw.C)),
		InvertedIndex:  make(map[string][]Posting, len(cw.I)),
		TotalDocs:      cw.TD,
		TotalChunks:    cw.TC,
		AvgDocLength:   cw.ADL,
		AvgChunkLength: cw.ACL,
	}

	// Restore documents.
	for i, d := range cw.D {
		path := cw.P[i]
		idx.Documents[path] = DocEntry{
			Path:        path,
			WordCount:   d.W,
			Tags:        d.T,
			Updated:     d.U,
			AccessCount: d.A,
			ChunkCount:  d.CC,
			Confidence:  d.Cf,
			Title:       d.Ti,
		}
	}

	// Restore chunks.
	for _, c := range cw.C {
		path := cw.P[c.P]
		key := fmt.Sprintf("%s#%d", path, c.I)
		idx.Chunks[key] = ChunkEntry{
			DocPath:   path,
			ChunkID:   c.I,
			WordCount: c.W,
			Offset:    c.O,
			Length:    c.L,
		}
	}

	// Restore inverted index.
	for term, postings := range cw.I {
		restored := make([]Posting, len(postings))
		for i, p := range postings {
			path := cw.P[p[0]]
			field := "content"
			switch p[2] {
			case fieldTag:
				field = "tag"
			case fieldPath:
				field = "path"
			}
			chunkKey := ""
			if p[3] >= 0 {
				chunkKey = fmt.Sprintf("%s#%d", path, p[3])
			}
			restored[i] = Posting{
				Path:      path,
				Frequency: p[1],
				Field:     field,
				ChunkKey:  chunkKey,
			}
		}
		idx.InvertedIndex[term] = restored
	}

	return idx
}

// saveFullIndexCompact writes the full index as gzip-compressed compact JSON.
func (b *FileBrain) saveFullIndexCompact(idx *FullIndex) error {
	path := filepath.Join(b.root, FullIndexFileName)
	cw := toCompact(idx)

	data, err := json.Marshal(cw)
	if err != nil {
		return fmt.Errorf("marshal compact index: %w", err)
	}

	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create compact index tmp: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewWriterLevel(f, gzip.DefaultCompression)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("create gzip writer: %w", err)
	}

	if _, err := gz.Write(data); err != nil {
		gz.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write compact index: %w", err)
	}

	if err := gz.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close gzip writer: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close compact index file: %w", err)
	}

	return os.Rename(tmpPath, path)
}

// loadFullIndexCompact reads the gzip-compressed compact JSON index.
func (b *FileBrain) loadFullIndexCompact() (*FullIndex, error) {
	path := filepath.Join(b.root, FullIndexFileName)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open compact index: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer gz.Close()

	var cw compactWire
	if err := json.NewDecoder(gz).Decode(&cw); err != nil {
		return nil, fmt.Errorf("decode compact index: %w", err)
	}

	return fromCompact(&cw), nil
}
