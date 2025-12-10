package crossseed

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/stringutils"
)

type fileRenameInstruction struct {
	oldPath string
	newPath string
}

// alignCrossSeedContentPaths renames the incoming cross-seed torrent (display name, folders, files)
// so that it matches the layout of the already-seeded torrent we're borrowing data from.
// Returns true if alignment succeeded (or wasn't needed), false if alignment failed.
func (s *Service) alignCrossSeedContentPaths(
	ctx context.Context,
	instanceID int,
	torrentHash string,
	sourceTorrentName string,
	matchedTorrent *qbt.Torrent,
	expectedSourceFiles qbt.TorrentFiles,
	candidateFiles qbt.TorrentFiles,
) bool {
	if matchedTorrent == nil {
		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", torrentHash).
			Msg("alignCrossSeedContentPaths called with nil matchedTorrent")
		return false
	}

	sourceRelease := s.releaseCache.Parse(sourceTorrentName)
	matchedRelease := s.releaseCache.Parse(matchedTorrent.Name)

	if len(expectedSourceFiles) == 0 || len(candidateFiles) == 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", torrentHash).
			Int("expectedSourceFiles", len(expectedSourceFiles)).
			Int("candidateFiles", len(candidateFiles)).
			Msg("Empty file list provided to alignment, skipping")
		return false
	}

	if !s.waitForTorrentAvailability(ctx, instanceID, torrentHash, crossSeedRenameWaitTimeout) {
		log.Warn().
			Int("instanceID", instanceID).
			Str("torrentHash", torrentHash).
			Msg("Cross-seed torrent not visible yet, skipping rename alignment")
		return false
	}

	canonicalHash := normalizeHash(torrentHash)

	trimmedSourceName := strings.TrimSpace(sourceTorrentName)
	trimmedMatchedName := strings.TrimSpace(matchedTorrent.Name)

	// Detect single-file → folder case (using expected files, before any qBittorrent updates)
	expectedSourceRoot := detectCommonRoot(expectedSourceFiles)
	expectedCandidateRoot := detectCommonRoot(candidateFiles)
	isSingleFileToFolder := expectedSourceRoot == "" && expectedCandidateRoot != ""

	// Determine if we should rename the torrent display name.
	// For single-file → folder cases with contentLayout=Subfolder, qBittorrent automatically
	// strips the file extension when creating the subfolder (e.g., "Movie.mkv" → "Movie/").
	// Don't rename in this case as qBittorrent handles it, and renaming would trigger recheck.
	shouldRename := shouldRenameTorrentDisplay(sourceRelease, matchedRelease) &&
		trimmedMatchedName != "" &&
		trimmedSourceName != trimmedMatchedName &&
		!(isSingleFileToFolder && namesMatchIgnoringExtension(trimmedSourceName, trimmedMatchedName))

	// Display name rename is best-effort - failure only affects UI label, not seeding functionality.
	// Unlike folder/file renames which are critical for data location, we continue on failure here.
	if shouldRename {
		if err := s.syncManager.RenameTorrent(ctx, instanceID, torrentHash, trimmedMatchedName); err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Msg("Failed to rename cross-seed torrent display name (cosmetic, continuing)")
		} else {
			log.Debug().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Str("newName", trimmedMatchedName).
				Msg("Renamed cross-seed torrent to match existing torrent name")
		}
	}

	if !shouldAlignFilesWithCandidate(sourceRelease, matchedRelease) {
		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", torrentHash).
			Str("sourceName", sourceTorrentName).
			Str("matchedName", matchedTorrent.Name).
			Msg("Skipping file alignment for episode matched to season pack")
		return true // Episode-in-pack uses season pack path directly, no alignment needed
	}

	sourceFiles := expectedSourceFiles
	refreshCtx := qbittorrent.WithForceFilesRefresh(ctx)
	filesMap, err := s.syncManager.GetTorrentFilesBatch(refreshCtx, instanceID, []string{torrentHash})
	if err != nil {
		log.Debug().
			Err(err).
			Int("instanceID", instanceID).
			Str("torrentHash", torrentHash).
			Msg("Failed to refresh torrent files, using expected source files")
	} else if currentFiles, ok := filesMap[canonicalHash]; ok && len(currentFiles) > 0 {
		sourceFiles = currentFiles
	} else {
		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", torrentHash).
			Str("canonicalHash", canonicalHash).
			Msg("Torrent hash not found in file map or files empty, using expected source files")
	}

	sourceRoot := detectCommonRoot(sourceFiles)
	targetRoot := detectCommonRoot(candidateFiles)

	// Rename folder FIRST if different - this must happen before file renames
	// because qBittorrent needs to know where to look for files on disk
	rootRenamed := false
	if sourceRoot != "" && targetRoot != "" && sourceRoot != targetRoot {
		if err := s.syncManager.RenameTorrentFolder(ctx, instanceID, torrentHash, sourceRoot, targetRoot); err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Str("from", sourceRoot).
				Str("to", targetRoot).
				Msg("Failed to rename cross-seed root folder, skipping file alignment")
			return false // Don't attempt file renames with wrong folder paths
		}
		rootRenamed = true
		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", torrentHash).
			Str("from", sourceRoot).
			Str("to", targetRoot).
			Msg("Renamed cross-seed root folder to match existing torrent")

		// Update sourceFiles paths to reflect the folder rename for file matching
		for i := range sourceFiles {
			sourceFiles[i].Name = adjustPathForRootRename(sourceFiles[i].Name, sourceRoot, targetRoot)
		}
	}

	plan, unmatched := buildFileRenamePlan(sourceFiles, candidateFiles)

	if len(plan) == 0 {
		if len(unmatched) > 0 {
			// Some files couldn't be mapped - this is OK, the hasExtraSourceFiles check
			// will detect this and trigger a recheck with threshold-based resume.
			log.Debug().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Int("unmatchedFiles", len(unmatched)).
				Strs("unmatchedPaths", unmatched).
				Msg("Some cross-seed files could not be mapped, recheck will handle verification")
		}
		return true // No renames needed - paths already match or recheck will verify
	}

	renamed := 0
	for _, instr := range plan {
		if instr.oldPath == instr.newPath || instr.oldPath == "" || instr.newPath == "" {
			continue
		}

		if err := s.syncManager.RenameTorrentFile(ctx, instanceID, torrentHash, instr.oldPath, instr.newPath); err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Str("from", instr.oldPath).
				Str("to", instr.newPath).
				Msg("Failed to rename cross-seed file, aborting alignment")
			return false
		}
		renamed++
	}

	if renamed == 0 && !rootRenamed {
		return true // No renames performed (paths already match or renames not required)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Str("torrentHash", torrentHash).
		Int("fileRenames", renamed).
		Bool("folderRenamed", rootRenamed).
		Msg("Aligned cross-seed torrent naming with existing torrent")

	if len(unmatched) > 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", torrentHash).
			Int("unmatchedFiles", len(unmatched)).
			Msg("Some cross-seed files could not be mapped to existing files and will keep their original names")
	}

	return true
}

func (s *Service) waitForTorrentAvailability(ctx context.Context, instanceID int, hash string, timeout time.Duration) bool {
	if strings.TrimSpace(hash) == "" {
		return false
	}

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return false
		}

		if qbtSyncManager, err := s.syncManager.GetQBittorrentSyncManager(ctx, instanceID); err == nil && qbtSyncManager != nil {
			if err := qbtSyncManager.Sync(ctx); err != nil {
				log.Debug().
					Err(err).
					Int("instanceID", instanceID).
					Msg("Failed to sync while waiting for cross-seed torrent availability, retrying")
			}
		}

		torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{Hashes: []string{hash}})
		if err == nil && len(torrents) > 0 {
			return true
		} else if err != nil {
			log.Debug().
				Err(err).
				Int("instanceID", instanceID).
				Msg("Failed to get torrents while waiting for cross-seed torrent availability, retrying")
		}

		time.Sleep(crossSeedRenamePollInterval)
	}

	return false
}

func buildFileRenamePlan(sourceFiles, candidateFiles qbt.TorrentFiles) ([]fileRenameInstruction, []string) {
	type candidateEntry struct {
		path       string
		size       int64
		base       string
		normalized string
		used       bool
	}

	candidateBuckets := make(map[int64][]*candidateEntry)
	for _, cf := range candidateFiles {
		entry := &candidateEntry{
			path:       cf.Name,
			size:       cf.Size,
			base:       strings.ToLower(fileBaseName(cf.Name)),
			normalized: normalizeFileKey(cf.Name),
		}
		candidateBuckets[cf.Size] = append(candidateBuckets[cf.Size], entry)
	}

	plan := make([]fileRenameInstruction, 0)
	unmatched := make([]string, 0)

	for _, sf := range sourceFiles {
		bucket := candidateBuckets[sf.Size]
		if len(bucket) == 0 {
			unmatched = append(unmatched, sf.Name)
			continue
		}

		sourceBase := strings.ToLower(fileBaseName(sf.Name))
		sourceNorm := normalizeFileKey(sf.Name)

		var available []*candidateEntry
		for _, entry := range bucket {
			if !entry.used {
				available = append(available, entry)
			}
		}

		if len(available) == 0 {
			unmatched = append(unmatched, sf.Name)
			continue
		}

		var match *candidateEntry

		// Exact path match.
		for _, cand := range available {
			if cand.path == sf.Name {
				match = cand
				break
			}
		}

		// Prefer identical base names.
		if match == nil {
			var candidates []*candidateEntry
			for _, cand := range available {
				if cand.base == sourceBase {
					candidates = append(candidates, cand)
				}
			}
			if len(candidates) == 1 {
				match = candidates[0]
			}
		}

		// Fallback to normalized key comparison (ignores punctuation).
		if match == nil {
			var candidates []*candidateEntry
			for _, cand := range available {
				if cand.normalized == sourceNorm {
					candidates = append(candidates, cand)
				}
			}
			if len(candidates) == 1 {
				match = candidates[0]
			}
		}

		// If only one candidate remains for this size, use it.
		if match == nil && len(available) == 1 {
			match = available[0]
		}

		if match == nil {
			unmatched = append(unmatched, sf.Name)
			continue
		}

		match.used = true
		if sf.Name == match.path {
			continue
		}

		plan = append(plan, fileRenameInstruction{
			oldPath: sf.Name,
			newPath: match.path,
		})
	}

	sort.Slice(plan, func(i, j int) bool {
		if plan[i].oldPath == plan[j].oldPath {
			return plan[i].newPath < plan[j].newPath
		}
		return plan[i].oldPath < plan[j].oldPath
	})

	return plan, unmatched
}

func normalizeFileKey(path string) string {
	base := fileBaseName(path)
	if base == "" {
		return ""
	}

	ext := ""
	if dot := strings.LastIndex(base, "."); dot >= 0 && dot < len(base)-1 {
		ext = strings.ToLower(base[dot+1:])
		base = base[:dot]
	}

	// For sidecar files like .nfo/.srt/.sub/.idx/.sfv/.txt, ignore an
	// intermediate video extension (e.g. ".mkv" in "name.mkv.nfo") so that
	// "Name.mkv.nfo" and "Name.nfo" normalize to the same key.
	if ext == "nfo" || ext == "srt" || ext == "sub" || ext == "idx" || ext == "sfv" || ext == "txt" {
		if dot := strings.LastIndex(base, "."); dot >= 0 && dot < len(base)-1 {
			videoExt := strings.ToLower(base[dot+1:])
			switch videoExt {
			case "mkv", "mp4", "avi", "ts", "m2ts", "mov", "mpg", "mpeg":
				base = base[:dot]
			}
		}
	}

	// Normalize Unicode characters (Shōgun → Shogun, etc.)
	base = stringutils.NormalizeUnicode(base)

	var b strings.Builder
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}

	if ext != "" {
		b.WriteString(".")
		b.WriteString(ext)
	}

	return b.String()
}

func fileBaseName(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 && idx < len(path)-1 {
		return path[idx+1:]
	}
	return path
}

func detectCommonRoot(files qbt.TorrentFiles) string {
	root := ""
	for _, f := range files {
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 {
			return ""
		}
		first := parts[0]
		if first == "" {
			return ""
		}
		if root == "" {
			root = first
			continue
		}
		if first != root {
			return ""
		}
	}
	return root
}

func adjustPathForRootRename(path, oldRoot, newRoot string) string {
	if oldRoot == "" || newRoot == "" || path == "" {
		return path
	}
	if path == oldRoot {
		return newRoot
	}
	if suffix, found := strings.CutPrefix(path, oldRoot+"/"); found {
		return newRoot + "/" + suffix
	}
	return path
}

func shouldRenameTorrentDisplay(newRelease, matchedRelease *rls.Release) bool {
	// Keep episode torrents named after the episode even when pointing at season pack files
	if newRelease.Series > 0 && newRelease.Episode > 0 &&
		matchedRelease.Series > 0 && matchedRelease.Episode == 0 {
		return false
	}
	return true
}

func shouldAlignFilesWithCandidate(newRelease, matchedRelease *rls.Release) bool {
	if newRelease.Series > 0 && newRelease.Episode > 0 &&
		matchedRelease.Series > 0 && matchedRelease.Episode == 0 {
		return false
	}
	return true
}

// namesMatchIgnoringExtension returns true if two names match after stripping common video file extensions.
// Used for single-file → folder cases where qBittorrent strips the extension when creating subfolders
// with contentLayout=Subfolder (e.g., "Movie.mkv" becomes folder "Movie/").
func namesMatchIgnoringExtension(name1, name2 string) bool {
	extensions := []string{".mkv", ".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm", ".m4v", ".ts", ".m2ts"}

	stripped1 := name1
	stripped2 := name2

	for _, ext := range extensions {
		if strings.HasSuffix(strings.ToLower(name1), ext) {
			stripped1 = name1[:len(name1)-len(ext)]
			break
		}
	}
	for _, ext := range extensions {
		if strings.HasSuffix(strings.ToLower(name2), ext) {
			stripped2 = name2[:len(name2)-len(ext)]
			break
		}
	}

	return stripped1 == stripped2
}

// hasExtraSourceFiles checks if source torrent has files that don't exist in the candidate.
// This happens when source has extra sidecar files (NFO, SRT, etc.) that weren't filtered
// by ignorePatterns. Returns true if source has files with sizes not present in candidate.
// This includes cases where source and candidate have the same file count but different files
// (e.g., source has mkv+srt, candidate has mkv+nfo - the srt won't exist on disk).
func hasExtraSourceFiles(sourceFiles, candidateFiles qbt.TorrentFiles) bool {
	// Build size buckets for candidate files
	candidateSizes := make(map[int64]int)
	for _, cf := range candidateFiles {
		candidateSizes[cf.Size]++
	}

	// Count how many source files can be matched by size
	matched := 0
	for _, sf := range sourceFiles {
		if count := candidateSizes[sf.Size]; count > 0 {
			candidateSizes[sf.Size]--
			matched++
		}
	}

	// If we couldn't match all source files by size, there are extras/mismatches
	return matched < len(sourceFiles)
}

// needsRenameAlignment checks if rename alignment will be required for a cross-seed add.
// Returns true if torrent name, root folder, or file names differ between source and candidate.
// For layout-change cases (folder→bare or bare→folder), also checks if file names inside differ.
func needsRenameAlignment(torrentName string, matchedTorrentName string, sourceFiles, candidateFiles qbt.TorrentFiles) bool {
	sourceRoot := detectCommonRoot(sourceFiles)
	candidateRoot := detectCommonRoot(candidateFiles)

	// Single file → folder: layout handled by contentLayout=Subfolder,
	// but file renames may still be needed if names differ
	if sourceRoot == "" && candidateRoot != "" {
		return filesNeedRenaming(sourceFiles, candidateFiles)
	}

	// Folder → single file: layout handled by contentLayout=NoSubfolder,
	// but file renames may still be needed if names differ
	if sourceRoot != "" && candidateRoot == "" {
		return filesNeedRenaming(sourceFiles, candidateFiles)
	}

	// Check display name (both have folders or both are single files)
	trimmedSourceName := strings.TrimSpace(torrentName)
	trimmedMatchedName := strings.TrimSpace(matchedTorrentName)
	if trimmedSourceName != trimmedMatchedName {
		return true
	}

	// Check root folder (both have folders)
	if sourceRoot != "" && candidateRoot != "" && sourceRoot != candidateRoot {
		return true
	}

	return false
}

// filesNeedRenaming checks if any files would need renaming after a layout change.
// Compares file names (ignoring folder structure) using normalized keys to detect
// punctuation differences like spaces vs periods.
func filesNeedRenaming(sourceFiles, candidateFiles qbt.TorrentFiles) bool {
	if len(sourceFiles) == 0 || len(candidateFiles) == 0 {
		return false
	}

	// Build a set of normalized candidate file keys by size
	type fileKey struct {
		normalized string
		size       int64
	}
	candidateKeys := make(map[fileKey]bool)
	for _, cf := range candidateFiles {
		candidateKeys[fileKey{normalized: normalizeFileKey(cf.Name), size: cf.Size}] = true
	}

	// Check if any source file doesn't have a matching candidate
	for _, sf := range sourceFiles {
		key := fileKey{normalized: normalizeFileKey(sf.Name), size: sf.Size}
		if !candidateKeys[key] {
			// No match by normalized key+size, need rename alignment
			return true
		}
	}

	// All source files have normalized matches - but do actual paths differ?
	// Build size buckets for detailed comparison
	candidateBuckets := make(map[int64][]string)
	for _, cf := range candidateFiles {
		base := fileBaseName(cf.Name)
		candidateBuckets[cf.Size] = append(candidateBuckets[cf.Size], base)
	}

	for _, sf := range sourceFiles {
		sourceBase := fileBaseName(sf.Name)
		bucket := candidateBuckets[sf.Size]

		// Check if exact base name exists in bucket
		found := false
		for _, candBase := range bucket {
			if sourceBase == candBase {
				found = true
				break
			}
		}
		if !found {
			// Base names differ (even if normalized matches), need rename
			return true
		}
	}

	return false
}
