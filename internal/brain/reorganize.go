package brain

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TrashDir is the subdirectory inside the brain root where trashed files are kept.
const TrashDir = ".trash"

// categoryKeywords maps each category to its discriminating tokens.
var categoryKeywords = map[string][]string{
	"identity":  {"name", "background", "role", "personal", "bio", "i", "me", "my", "work", "job", "title", "live", "born", "age", "experience"},
	"opinions":  {"prefer", "think", "believe", "opinion", "like", "dislike", "love", "hate", "feel", "view", "perspective", "rather", "better", "worse", "favor"},
	"style":     {"style", "format", "convention", "pattern", "approach", "structure", "organize", "layout", "naming", "indent", "write", "code"},
	"decisions": {"decision", "chose", "decided", "tradeoff", "why", "reason", "because", "instead", "choose", "pick", "select", "adopt"},
	"knowledge": {"how", "what", "fact", "reference", "documentation", "know", "learn", "understand", "concept", "definition", "explain", "guide"},
}

// kwToCategories is a pre-built reverse map: token → categories it signals.
// Built once at init to avoid O(C×K) per-file work in suggestRecategorization.
var kwToCategories map[string][]string

func init() {
	kwToCategories = make(map[string][]string)
	for cat, keywords := range categoryKeywords {
		for _, kw := range keywords {
			kwToCategories[kw] = append(kwToCategories[kw], cat)
		}
	}
}

// computeDocVectors builds BM25-weighted vectors per document using content and
// tag fields only. Path tokens are excluded: the category directory token (e.g.
// "knowledge") is shared by every file in that category and would inflate
// same-category similarity, producing false-positive duplicates.
func (b *FileBrain) computeDocVectors(idx *FullIndex) map[string]map[string]float64 {
	vectors := make(map[string]map[string]float64, len(idx.Documents))
	for docPath := range idx.Documents {
		vectors[docPath] = make(map[string]float64)
	}

	avgdl := idx.AvgDocLength
	if avgdl == 0 {
		avgdl = 1
	}
	N := float64(idx.TotalDocs)

	for term, postings := range idx.InvertedIndex {
		docSet := make(map[string]bool)
		for _, p := range postings {
			if p.Field != "path" {
				docSet[p.Path] = true
			}
		}
		if len(docSet) == 0 {
			continue
		}
		n := float64(len(docSet))
		idf := math.Log((N-n+0.5)/(n+0.5) + 1.0)

		for _, p := range postings {
			if p.Field == "path" {
				continue
			}
			if _, ok := vectors[p.Path]; !ok {
				continue
			}
			tf := float64(p.Frequency)
			dl := float64(idx.Documents[p.Path].WordCount)
			num := tf * (BM25K1 + 1)
			denom := tf + BM25K1*(1-BM25B+BM25B*dl/avgdl)
			boost := fieldBoost(p.Field)
			vectors[p.Path][term] += idf * (num / denom) * boost
		}
	}

	return vectors
}

// cosineSimilarity returns the cosine similarity between two TF-IDF vectors.
func cosineSimilarity(a, b map[string]float64) float64 {
	var dot, normA, normB float64
	for term, scoreA := range a {
		normA += scoreA * scoreA
		if scoreB, ok := b[term]; ok {
			dot += scoreA * scoreB
		}
	}
	for _, scoreB := range b {
		normB += scoreB * scoreB
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// computeSimilarityMatrix returns pairwise cosine similarities above minThreshold.
func (b *FileBrain) computeSimilarityMatrix(idx *FullIndex, minThreshold float64) map[[2]string]float64 {
	vectors := b.computeDocVectors(idx)

	docs := make([]string, 0, len(vectors))
	for d := range vectors {
		docs = append(docs, d)
	}
	sort.Strings(docs)

	result := make(map[[2]string]float64)
	for i := 0; i < len(docs); i++ {
		for j := i + 1; j < len(docs); j++ {
			sim := cosineSimilarity(vectors[docs[i]], vectors[docs[j]])
			if sim >= minThreshold {
				result[[2]string{docs[i], docs[j]}] = sim
			}
		}
	}
	return result
}

// unionFind groups transitive duplicates.
type unionFind struct {
	parent map[string]string
}

func newUnionFind() *unionFind {
	return &unionFind{parent: make(map[string]string)}
}

func (uf *unionFind) find(x string) string {
	if _, ok := uf.parent[x]; !ok {
		uf.parent[x] = x
	}
	if uf.parent[x] != x {
		uf.parent[x] = uf.find(uf.parent[x])
	}
	return uf.parent[x]
}

func (uf *unionFind) union(x, y string) {
	uf.parent[uf.find(x)] = uf.find(y)
}

func (uf *unionFind) groups() map[string][]string {
	groups := make(map[string][]string)
	for k := range uf.parent {
		root := uf.find(k)
		groups[root] = append(groups[root], k)
	}
	return groups
}

// bestTarget picks the merge target: highest access count → newest → longest.
func bestTarget(files []*BrainFile) *BrainFile {
	best := files[0]
	for _, f := range files[1:] {
		if f.AccessCount > best.AccessCount {
			best = f
		} else if f.AccessCount == best.AccessCount {
			if f.Updated.Time.After(best.Updated.Time) {
				best = f
			} else if f.Updated.Time.Equal(best.Updated.Time) && len(f.Content) > len(best.Content) {
				best = f
			}
		}
	}
	return best
}

// ensureFullIndex loads the full index, rebuilding if missing, corrupted, or
// from a pre-chunking format (TotalChunks == 0). Must only be called while
// holding b.mu (write lock). Uses internal unlocked helpers only.
func (b *FileBrain) ensureFullIndex() (*FullIndex, error) {
	idx, err := b.loadFullIndex()
	if err != nil || idx.TotalChunks == 0 {
		if rebuildErr := b.rebuildFullIndex(); rebuildErr != nil {
			return nil, fmt.Errorf("rebuild full index: %w", rebuildErr)
		}
		idx, err = b.loadFullIndex()
		if err != nil {
			return nil, fmt.Errorf("load full index after rebuild: %w", err)
		}
	}
	return idx, nil
}

// FindDuplicates detects brain files with high content similarity and proposes merges.
func (b *FileBrain) FindDuplicates(opts ReorgOptions) (*ReorgPlan, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.findDuplicates(opts)
}

func (b *FileBrain) findDuplicates(opts ReorgOptions) (*ReorgPlan, error) {
	idx, err := b.ensureFullIndex()
	if err != nil {
		return nil, err
	}
	if idx.TotalDocs < 2 {
		return &ReorgPlan{}, nil
	}

	threshold := opts.SimilarityThreshold
	if threshold <= 0 {
		threshold = 0.7
	}

	matrix := b.computeSimilarityMatrix(idx, threshold)

	// Only union paths that actually appear in above-threshold pairs; isolated
	// docs are never duplicates and don't need to occupy the union-find.
	uf := newUnionFind()
	for pair := range matrix {
		uf.union(pair[0], pair[1])
	}

	groups := uf.groups()
	plan := &ReorgPlan{}
	for _, members := range groups {
		if len(members) < 2 {
			continue
		}

		var files []*BrainFile
		for _, m := range members {
			bf, err := b.load(m)
			if err != nil {
				continue
			}
			files = append(files, bf)
		}
		if len(files) < 2 {
			continue
		}

		target := bestTarget(files)

		var simSum float64
		var simCount int
		for i := 0; i < len(members); i++ {
			for j := i + 1; j < len(members); j++ {
				key := [2]string{members[i], members[j]}
				if s, ok := matrix[key]; ok {
					simSum += s
					simCount++
				} else if s, ok := matrix[[2]string{members[j], members[i]}]; ok {
					simSum += s
					simCount++
				}
			}
		}
		avgSim := 0.0
		if simCount > 0 {
			avgSim = simSum / float64(simCount)
		}

		sources := make([]string, 0, len(members)-1)
		for _, m := range members {
			if m != target.Path {
				sources = append(sources, m)
			}
		}
		sort.Strings(sources)

		plan.Actions = append(plan.Actions, ReorgAction{
			Type:        ActionMerge,
			SourcePaths: sources,
			TargetPath:  target.Path,
			Reason:      fmt.Sprintf("%.0f%% content similarity", avgSim*100),
			Similarity:  avgSim,
		})
	}

	return plan, nil
}

// SuggestRecategorization finds brain files that appear to belong in a different category.
func (b *FileBrain) SuggestRecategorization(opts ReorgOptions) (*ReorgPlan, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.suggestRecategorization()
}

func (b *FileBrain) suggestRecategorization() (*ReorgPlan, error) {
	files, err := b.listAll()
	if err != nil {
		return nil, fmt.Errorf("list brain files: %w", err)
	}

	plan := &ReorgPlan{}
	for _, relPath := range files {
		// The vault is a reserved category, not a misplaced one.
		if IsVaultPath(relPath) {
			continue
		}
		parts := strings.SplitN(relPath, string(filepath.Separator), 2)
		if len(parts) != 2 {
			continue
		}
		currentCategory := parts[0]
		filename := parts[1]

		if _, ok := categoryKeywords[currentCategory]; !ok {
			continue
		}

		bf, err := b.load(relPath)
		if err != nil {
			continue
		}

		text := strings.ToLower(bf.Content + " " + strings.Join(bf.Tags, " "))
		tokens := tokenize(text)

		// Score using pre-built reverse map: one pass over tokens, O(T) per file.
		scores := make(map[string]float64)
		for _, tok := range tokens {
			for _, cat := range kwToCategories[tok] {
				scores[cat]++
			}
		}

		bestCat := currentCategory
		bestScore := scores[currentCategory]
		for cat, score := range scores {
			if score > bestScore {
				bestScore = score
				bestCat = cat
			}
		}

		if bestCat == currentCategory {
			continue
		}
		// Require ≥30% improvement over the current category score to avoid noise.
		if bestScore < scores[currentCategory]*1.3+2 {
			continue
		}

		plan.Actions = append(plan.Actions, ReorgAction{
			Type:        ActionMove,
			SourcePaths: []string{relPath},
			TargetPath:  filepath.Join(bestCat, filename),
			Reason:      fmt.Sprintf("content scores higher in '%s' (%.0f vs %.0f)", bestCat, bestScore, scores[currentCategory]),
		})
	}

	return plan, nil
}

// FindConsolidationCandidates finds small, related files that should be merged.
func (b *FileBrain) FindConsolidationCandidates(opts ReorgOptions) (*ReorgPlan, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.findConsolidationCandidates(opts)
}

func (b *FileBrain) findConsolidationCandidates(opts ReorgOptions) (*ReorgPlan, error) {
	threshold := opts.SmallFileThreshold
	if threshold <= 0 {
		threshold = 50
	}

	idx, err := b.ensureFullIndex()
	if err != nil {
		return nil, err
	}

	categorySmall := make(map[string][]string)
	for path, entry := range idx.Documents {
		if entry.WordCount < threshold {
			cat := strings.SplitN(path, string(filepath.Separator), 2)[0]
			categorySmall[cat] = append(categorySmall[cat], path)
		}
	}

	plan := &ReorgPlan{}
	const consolidateSimilarityThreshold = 0.35

	vectors := b.computeDocVectors(idx)

	for cat, smallFiles := range categorySmall {
		if len(smallFiles) < 2 {
			continue
		}
		sort.Strings(smallFiles)

		uf := newUnionFind()
		for i := 0; i < len(smallFiles); i++ {
			for j := i + 1; j < len(smallFiles); j++ {
				if cosineSimilarity(vectors[smallFiles[i]], vectors[smallFiles[j]]) >= consolidateSimilarityThreshold {
					uf.union(smallFiles[i], smallFiles[j])
				}
			}
		}

		for _, members := range uf.groups() {
			if len(members) < 2 {
				continue
			}
			sort.Strings(members)

			var files []*BrainFile
			for _, m := range members {
				bf, err := b.load(m)
				if err != nil {
					continue
				}
				files = append(files, bf)
			}
			if len(files) < 2 {
				continue
			}

			target := bestTarget(files)
			sources := make([]string, 0, len(members)-1)
			for _, m := range members {
				if m != target.Path {
					sources = append(sources, m)
				}
			}

			plan.Actions = append(plan.Actions, ReorgAction{
				Type:        ActionConsolidate,
				SourcePaths: sources,
				TargetPath:  target.Path,
				Reason:      fmt.Sprintf("small related files in '%s' category (%d files)", cat, len(members)),
			})
		}
	}

	return plan, nil
}

// trashDest returns the destination path inside the trash and ensures the
// directory exists. Shared by trashFile and copyToTrash.
func (b *FileBrain) trashDest(relPath, trashSession string) (string, error) {
	dst := filepath.Join(b.root, TrashDir, trashSession, relPath)
	if err := os.MkdirAll(filepath.Dir(dst), DirPermissions); err != nil {
		return "", fmt.Errorf("create trash dir: %w", err)
	}
	return dst, nil
}

// trashFile moves a brain file to .trash/<session>/<relPath>.
func (b *FileBrain) trashFile(relPath, trashSession string) error {
	src, err := b.safePath(relPath)
	if err != nil {
		return err
	}
	dst, err := b.trashDest(relPath, trashSession)
	if err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// copyToTrash copies a brain file to .trash/<session>/<relPath> without
// removing the original. Used to back up targets before in-place modification.
func (b *FileBrain) copyToTrash(relPath, trashSession string) error {
	src, err := b.safePath(relPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read for backup: %w", err)
	}
	dst, err := b.trashDest(relPath, trashSession)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, FilePermissions)
}

// ApplyReorgPlan executes a reorganization plan, moving removed files to
// .trash/ so nothing is permanently lost.
func (b *FileBrain) ApplyReorgPlan(plan *ReorgPlan) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.applyReorgPlan(plan)
}

func (b *FileBrain) applyReorgPlan(plan *ReorgPlan) error {
	session := time.Now().Format("20060102-150405")

	for _, action := range plan.Actions {
		if touchesVault(action) {
			continue // never move, merge or consolidate vault notes
		}
		var err error
		switch action.Type {
		case ActionMerge:
			err = b.applyMergeOrConsolidate(action, session, false)
		case ActionMove:
			err = b.applyMove(action, session)
		case ActionConsolidate:
			err = b.applyMergeOrConsolidate(action, session, true)
		default:
			return fmt.Errorf("unknown action type: %s", action.Type)
		}
		if err != nil {
			return fmt.Errorf("%s %v -> %s: %w", action.Type, action.SourcePaths, action.TargetPath, err)
		}
	}
	return b.rebuildIndex()
}

// applyMergeOrConsolidate handles both merge and consolidate actions.
// When withHeaders is true, each source gets a "## Topic" section header
// (consolidate); otherwise a "---" separator is used (merge).
func (b *FileBrain) applyMergeOrConsolidate(action ReorgAction, trashSession string, withHeaders bool) error {
	target, err := b.load(action.TargetPath)
	if err != nil {
		return fmt.Errorf("load target: %w", err)
	}

	// Back up the original target (copy, not move — original stays in place).
	if err := b.copyToTrash(action.TargetPath, trashSession+"-originals"); err != nil {
		return fmt.Errorf("backup target: %w", err)
	}

	var merged []string
	for _, srcPath := range action.SourcePaths {
		src, err := b.load(srcPath)
		if err != nil {
			continue
		}

		var separator string
		if withHeaders {
			topicName := titleCase(strings.ReplaceAll(strings.TrimSuffix(filepath.Base(srcPath), ".md"), "-", " "))
			separator = "\n\n## " + topicName + "\n\n"
		} else {
			separator = "\n\n---\n\n"
		}

		target.Content = strings.TrimRight(target.Content, "\n") + separator + strings.TrimSpace(src.Content) + "\n"
		target.Tags = mergeTags(target.Tags, src.Tags)
		if confidenceRank(src.Confidence) > confidenceRank(target.Confidence) {
			target.Confidence = src.Confidence
		}
		merged = append(merged, srcPath)
	}
	target.Updated = DateOnly{time.Now()}

	if err := b.save(target); err != nil {
		return err
	}

	for _, srcPath := range merged {
		_ = b.trashFile(srcPath, trashSession)
	}
	return nil
}

func (b *FileBrain) applyMove(action ReorgAction, trashSession string) error {
	if len(action.SourcePaths) == 0 {
		return fmt.Errorf("no source paths")
	}
	srcPath := action.SourcePaths[0]

	targetFull, err := b.safePath(action.TargetPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(targetFull); err == nil {
		return fmt.Errorf("target %q already exists; will not overwrite", action.TargetPath)
	}

	bf, err := b.load(srcPath)
	if err != nil {
		return err
	}
	bf.Path = action.TargetPath
	bf.Updated = DateOnly{time.Now()}

	if err := b.save(bf); err != nil {
		return err
	}

	return b.trashFile(srcPath, trashSession)
}

// Reorganize runs reorganization analyses according to opts.Mode and optionally
// applies the result — all under a single lock, preventing TOCTOU races.
//
// opts.Mode selects which analyses to run:
//
//	ModeAll (default) — dedup + recategorize + consolidate
//	ModeDedup         — duplicate detection only
//	ModeRecategorize  — miscategorization detection only
//	ModeConsolidate   — small-file consolidation only
func (b *FileBrain) Reorganize(opts ReorgOptions) (*ReorgPlan, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var dupPlan, recatPlan, consolPlan *ReorgPlan
	var err error

	runDedup := opts.Mode == ModeAll || opts.Mode == ModeDedup || opts.Mode == ""
	runRecat := opts.Mode == ModeAll || opts.Mode == ModeRecategorize || opts.Mode == ""
	runConsol := opts.Mode == ModeAll || opts.Mode == ModeConsolidate || opts.Mode == ""

	if runDedup {
		if dupPlan, err = b.findDuplicates(opts); err != nil {
			return nil, fmt.Errorf("find duplicates: %w", err)
		}
	}
	if runRecat {
		if recatPlan, err = b.suggestRecategorization(); err != nil {
			return nil, fmt.Errorf("suggest recategorization: %w", err)
		}
	}
	if runConsol {
		if consolPlan, err = b.findConsolidationCandidates(opts); err != nil {
			return nil, fmt.Errorf("find consolidation candidates: %w", err)
		}
	}

	// Merge plans: a file may only be consumed (removed/moved) once.
	// Priority: merge > consolidate > recategorize.
	consumed := make(map[string]bool)
	combined := &ReorgPlan{}

	addIfNew := func(actions []ReorgAction) {
		for _, a := range actions {
			if touchesVault(a) {
				continue
			}
			conflict := consumed[a.TargetPath]
			for _, src := range a.SourcePaths {
				if consumed[src] {
					conflict = true
					break
				}
			}
			if conflict {
				continue
			}
			for _, src := range a.SourcePaths {
				consumed[src] = true
			}
			combined.Actions = append(combined.Actions, a)
		}
	}

	if dupPlan != nil {
		addIfNew(dupPlan.Actions)
	}
	if consolPlan != nil {
		addIfNew(consolPlan.Actions)
	}
	if recatPlan != nil {
		addIfNew(recatPlan.Actions)
	}

	if !opts.DryRun {
		if err := b.applyReorgPlan(combined); err != nil {
			return combined, fmt.Errorf("apply plan: %w", err)
		}
	}

	return combined, nil
}

// Migrate finds brain files whose root directory is not a valid category and
// proposes moving them to the best-matching valid category.
//
// Scoring uses content, tags, AND filename tokens so sparse files get a better
// signal. Normalized confidence (0–1) is stored in ReorgAction.Similarity.
// When the target path already exists the action type is set to ActionConsolidate
// so the source is merged into the existing file rather than overwriting it.
func (b *FileBrain) Migrate(opts MigrateOptions) (*ReorgPlan, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	files, err := b.listAll()
	if err != nil {
		return nil, fmt.Errorf("list brain files: %w", err)
	}

	plan := &ReorgPlan{}
	for _, relPath := range files {
		// The vault is a reserved category, not a misplaced one.
		if IsVaultPath(relPath) {
			continue
		}
		parts := strings.SplitN(relPath, string(filepath.Separator), 2)

		var currentCategory, filename string
		if len(parts) == 1 {
			currentCategory = ""
			filename = parts[0]
		} else {
			currentCategory = parts[0]
			filename = parts[1]
		}

		if ValidCategory(currentCategory) {
			continue
		}

		bf, err := b.load(relPath)
		if err != nil {
			continue
		}

		// Score content + tags + filename tokens (slug split on - and _).
		slug := strings.TrimSuffix(filepath.Base(filename), ".md")
		slugText := strings.ReplaceAll(strings.ReplaceAll(slug, "-", " "), "_", " ")
		text := strings.ToLower(bf.Content + " " + strings.Join(bf.Tags, " ") + " " + slugText)
		tokens := tokenize(text)

		scores := make(map[string]float64)
		for _, tok := range tokens {
			for _, cat := range kwToCategories[tok] {
				scores[cat]++
			}
		}

		// Normalized confidence: best / total across all categories.
		var totalScore float64
		for _, s := range scores {
			totalScore += s
		}

		bestCat := "knowledge"
		var bestScore float64
		for _, cat := range DefaultCategories {
			if scores[cat] > bestScore {
				bestScore = scores[cat]
				bestCat = cat
			}
		}

		confidence := 0.0
		if totalScore > 0 {
			confidence = bestScore / totalScore
		}

		targetPath := filepath.Join(bestCat, filename)

		// Build reason string.
		src := fmt.Sprintf("%q", currentCategory)
		if currentCategory == "" {
			src = "brain root (no category)"
		}
		reason := fmt.Sprintf("root %s → %q (confidence %.0f%%)", src, bestCat, confidence*100)

		// Detect target conflict: switch to consolidate so we merge, not overwrite.
		actionType := ActionMove
		targetFull, pathErr := b.safePath(targetPath)
		if pathErr == nil {
			if _, statErr := os.Stat(targetFull); statErr == nil {
				actionType = ActionConsolidate
				reason += " [target exists — will merge]"
			}
		}

		plan.Actions = append(plan.Actions, ReorgAction{
			Type:        actionType,
			SourcePaths: []string{relPath},
			TargetPath:  targetPath,
			Reason:      reason,
			Similarity:  confidence,
		})
	}

	if !opts.DryRun && len(plan.Actions) > 0 {
		trashSession := time.Now().Format("20060102-150405")
		for _, action := range plan.Actions {
			var err error
			if action.Type == ActionConsolidate {
				err = b.applyMergeOrConsolidate(action, trashSession, true)
			} else {
				err = b.applyMove(action, trashSession)
			}
			if err != nil {
				return plan, fmt.Errorf("migrate %v: %w", action.SourcePaths, err)
			}
		}
		if err := b.rebuildIndex(); err != nil {
			return plan, fmt.Errorf("rebuild index: %w", err)
		}
	}

	return plan, nil
}

// confidenceRank returns a numeric rank for confidence levels (0–3).
func confidenceRank(c string) int {
	switch c {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

// titleCase capitalizes the first letter of each space-separated word.
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// touchesVault reports whether a reorganization action reads or writes a
// vault note. Such actions are never proposed or applied.
func touchesVault(a ReorgAction) bool {
	if IsVaultPath(a.TargetPath) {
		return true
	}
	for _, p := range a.SourcePaths {
		if IsVaultPath(p) {
			return true
		}
	}
	return false
}
