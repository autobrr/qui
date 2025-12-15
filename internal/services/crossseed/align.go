package crossseed

import (
	"context"
	"slices"
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

	// Try to get current files from qBittorrent with a few retries for slow clients.
	// On slow clients, the torrent may be visible but files not yet populated.
	var sourceFiles qbt.TorrentFiles
	refreshCtx := qbittorrent.WithForceFilesRefresh(ctx)
	for attempt := range 3 {
		if ctx.Err() != nil {
			break
		}
		filesMap, err := s.syncManager.GetTorrentFilesBatch(refreshCtx, instanceID, []string{torrentHash})
		if err != nil {
			log.Debug().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Int("attempt", attempt+1).
				Msg("Failed to get torrent files, retrying")
		} else if currentFiles, ok := filesMap[canonicalHash]; ok && len(currentFiles) > 0 {
			sourceFiles = currentFiles
			log.Debug().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Int("fileCount", len(currentFiles)).
				Msg("Got torrent files from qBittorrent")
			break
		} else {
			log.Trace().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Int("attempt", attempt+1).
				Msg("Torrent files not yet available, retrying")
		}
		if attempt < 2 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Fallback to expected files if we couldn't get them from qBittorrent
	if len(sourceFiles) == 0 {
		if ctx.Err() != nil {
			log.Trace().
				Err(ctx.Err()).
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Msg("Context cancelled while getting torrent files, using expected source files")
		} else {
			log.Debug().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Msg("Could not get torrent files after retries, using expected source files")
		}
		sourceFiles = expectedSourceFiles
	}

	sourceRoot := detectCommonRoot(sourceFiles)
	targetRoot := detectCommonRoot(candidateFiles)

	// Build file rename plan using original source paths (before any folder rename).
	// The plan maps source files to candidate files based on size matching.
	plan, unmatched := buildFileRenamePlan(sourceFiles, candidateFiles)

	// Rename files FIRST (while still in original folder structure).
	// This must happen before folder rename to avoid disk conflicts: if we rename the
	// folder first, then try to rename a file to a name that already exists on disk
	// (from the matched torrent), qBittorrent will fail with "newPath already in use".
	// By renaming files first within the original folder, we're just updating metadata
	// (the original folder doesn't exist on disk anyway for a fresh cross-seed add).
	//
	// IMPORTANT: qBittorrent's file rename API is async - it returns 200 OK immediately
	// but the actual rename happens later via libtorrent. We must verify renames worked
	// and retry if needed, as the API can silently fail.
	renamed := 0
	for _, instr := range plan {
		if instr.oldPath == instr.newPath || instr.oldPath == "" || instr.newPath == "" {
			continue
		}

		// Compute paths for file rename while keeping the original folder structure.
		// e.g., plan says: "FolderA/file.mkv" -> "FolderB/file2.mkv"
		// We rename: "FolderA/file.mkv" -> "FolderA/file2.mkv" (keep source folder)
		// Then folder rename "FolderA" -> "FolderB" will produce "FolderB/file2.mkv"
		actualOldPath := instr.oldPath
		actualNewPath := instr.newPath
		if sourceRoot != "" && targetRoot != "" && sourceRoot != targetRoot {
			// Adjust newPath to stay in source folder (file rename only changes the filename)
			actualNewPath = adjustPathForRootRename(instr.newPath, targetRoot, sourceRoot)
		}

		if actualOldPath == actualNewPath {
			continue
		}

		// Attempt rename with verification and retry (qBittorrent rename is async and can silently fail)
		if !s.renameFileWithVerification(ctx, instanceID, torrentHash, actualOldPath, actualNewPath) {
			log.Warn().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Str("from", actualOldPath).
				Str("to", actualNewPath).
				Msg("Failed to rename cross-seed file after retries, aborting alignment")
			return false
		}
		renamed++
	}

	// Rename folder AFTER file renames are complete.
	// Now that files have their target names within the source folder, renaming the
	// folder will update all paths to match the candidate torrent's layout, pointing
	// to the existing files on disk (from the matched torrent).
	rootRenamed := false
	if sourceRoot != "" && targetRoot != "" && sourceRoot != targetRoot {
		if err := s.syncManager.RenameTorrentFolder(ctx, instanceID, torrentHash, sourceRoot, targetRoot); err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Str("from", sourceRoot).
				Str("to", targetRoot).
				Msg("Failed to rename cross-seed root folder")
			return false
		}
		rootRenamed = true
		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", torrentHash).
			Str("from", sourceRoot).
			Str("to", targetRoot).
			Msg("Renamed cross-seed root folder to match existing torrent")
	}

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
		found := slices.Contains(bucket, sourceBase)
		if !found {
			// Base names differ (even if normalized matches), need rename
			return true
		}
	}

	return false
}

// renameFileWithVerification attempts to rename a file and verifies the rename actually worked.
// qBittorrent's rename API is async (libtorrent processes it in the background) and can silently
// fail even when returning 200 OK. This function retries with verification to handle such cases.
//
// Timing constants may need adjustment for systems with slow storage or high qBittorrent load.
func (s *Service) renameFileWithVerification(ctx context.Context, instanceID int, hash, oldPath, newPath string) bool {
	const maxAttempts = 3
	const verifyDelay = 150 * time.Millisecond // Wait for libtorrent async rename
	const retryDelay = 300 * time.Millisecond  // Delay between retry attempts

	canonicalHash := normalizeHash(hash)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if ctx.Err() != nil {
			log.Debug().
				Err(ctx.Err()).
				Int("instanceID", instanceID).
				Str("torrentHash", hash).
				Msg("Context cancelled before file rename attempt")
			return false
		}

		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", hash).
			Str("from", oldPath).
			Str("to", newPath).
			Int("attempt", attempt).
			Msg("Renaming cross-seed file")

		if err := s.syncManager.RenameTorrentFile(ctx, instanceID, hash, oldPath, newPath); err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", hash).
				Str("from", oldPath).
				Str("to", newPath).
				Int("attempt", attempt).
				Msg("RenameTorrentFile API call failed")

			if attempt < maxAttempts {
				time.Sleep(retryDelay)
				if ctx.Err() != nil {
					log.Debug().
						Err(ctx.Err()).
						Int("instanceID", instanceID).
						Str("torrentHash", hash).
						Msg("Context cancelled during rename retry delay")
					return false
				}
				continue
			}
			return false
		}

		// Wait for the async rename to complete in qBittorrent/libtorrent
		time.Sleep(verifyDelay)
		if ctx.Err() != nil {
			log.Debug().
				Err(ctx.Err()).
				Int("instanceID", instanceID).
				Str("torrentHash", hash).
				Msg("Context cancelled during rename verification delay")
			return false
		}

		// Verify the rename actually worked by fetching fresh file list
		refreshCtx := qbittorrent.WithForceFilesRefresh(ctx)
		filesMap, err := s.syncManager.GetTorrentFilesBatch(refreshCtx, instanceID, []string{hash})
		if err != nil {
			// Can't verify - this is the same state as the old code (no verification).
			// Assume success since failing here would leave torrent in worse half-aligned state.
			log.Debug().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", hash).
				Int("attempt", attempt).
				Msg("Failed to get files for rename verification, proceeding without verification")
			return true
		}

		currentFiles, ok := filesMap[canonicalHash]
		if !ok || len(currentFiles) == 0 {
			// No files returned - unusual but can't verify. Same reasoning as above.
			log.Debug().
				Int("instanceID", instanceID).
				Str("torrentHash", hash).
				Int("attempt", attempt).
				Msg("No files returned for rename verification, proceeding without verification")
			return true
		}

		// Check if newPath exists in current files (rename succeeded)
		// or if oldPath still exists (rename failed silently)
		oldPathExists := false
		newPathExists := false
		for _, f := range currentFiles {
			if f.Name == oldPath {
				oldPathExists = true
			}
			if f.Name == newPath {
				newPathExists = true
			}
		}

		if newPathExists {
			if attempt > 1 {
				log.Debug().
					Int("instanceID", instanceID).
					Str("torrentHash", hash).
					Str("newPath", newPath).
					Int("attempt", attempt).
					Msg("File rename verified successful after retry")
			}
			return true
		}

		if oldPathExists {
			log.Debug().
				Int("instanceID", instanceID).
				Str("torrentHash", hash).
				Str("oldPath", oldPath).
				Str("newPath", newPath).
				Int("attempt", attempt).
				Msg("File rename silently failed (old path still exists), retrying")

			if attempt < maxAttempts {
				time.Sleep(retryDelay)
				if ctx.Err() != nil {
					log.Debug().
						Err(ctx.Err()).
						Int("instanceID", instanceID).
						Str("torrentHash", hash).
						Msg("Context cancelled during rename retry delay")
					return false
				}
				continue
			}

			// All retries exhausted with old path still present
			log.Warn().
				Int("instanceID", instanceID).
				Str("torrentHash", hash).
				Str("oldPath", oldPath).
				Str("newPath", newPath).
				Int("attempts", maxAttempts).
				Msg("File rename failed after all retry attempts (old path still exists)")
			return false
		}

		// Neither path found - unexpected state. Could be path normalization differences,
		// folder structure changes, or qBittorrent internal state issues. Log at Warn level
		// for visibility but proceed since we can't determine actual state and failing
		// would leave torrent in a worse half-aligned state.
		log.Warn().
			Int("instanceID", instanceID).
			Str("torrentHash", hash).
			Str("oldPath", oldPath).
			Str("newPath", newPath).
			Int("attempt", attempt).
			Msg("Neither old nor new path found after rename - unexpected state, proceeding")
		return true
	}

	return false
}
