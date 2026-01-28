// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"path/filepath"
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
// Returns true if alignment succeeded (or wasn't needed), false if alignment failed,
// along with the active hash that qBittorrent exposes for the torrent.
func (s *Service) alignCrossSeedContentPaths(
	ctx context.Context,
	instanceID int,
	torrentHash string,
	torrentHashV2 string,
	sourceTorrentName string,
	matchedTorrent *qbt.Torrent,
	expectedSourceFiles qbt.TorrentFiles,
	candidateFiles qbt.TorrentFiles,
) (bool, string) {
	hashes := dedupeHashes(torrentHash, torrentHashV2)
	hashLabel := ""
	if len(hashes) > 0 {
		hashLabel = hashes[0]
	}

	if matchedTorrent == nil {
		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", hashLabel).
			Msg("alignCrossSeedContentPaths called with nil matchedTorrent")
		return false, ""
	}

	sourceRelease := s.releaseCache.Parse(sourceTorrentName)
	matchedRelease := s.releaseCache.Parse(matchedTorrent.Name)

	// Safety check: reject forbidden pairing (season pack from episode) at alignment stage.
	// This should have been caught earlier, but serves as a defense-in-depth guard.
	// Returns false so callers know alignment did not succeed (prevents recheck/resume logic).
	if reject, _ := rejectSeasonPackFromEpisode(sourceRelease, matchedRelease, true); reject {
		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", torrentHash).
			Str("sourceName", sourceTorrentName).
			Str("matchedName", matchedTorrent.Name).
			Msg("Skipping alignment: season pack cannot use single-episode files")
		return false, ""
	}

	if len(expectedSourceFiles) == 0 || len(candidateFiles) == 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", torrentHash).
			Int("expectedSourceFiles", len(expectedSourceFiles)).
			Int("candidateFiles", len(candidateFiles)).
			Msg("Empty file list provided to alignment, skipping")
		return false, ""
	}

	expectedSourceRoot := detectCommonRoot(expectedSourceFiles)
	expectedCandidateRoot := detectCommonRoot(candidateFiles)
	planPreview, _ := buildFileRenamePlan(expectedSourceFiles, candidateFiles)
	needsRootRename := expectedSourceRoot != "" && expectedCandidateRoot != "" && expectedSourceRoot != expectedCandidateRoot
	needsFileRename := len(planPreview) > 0
	alignmentNeeded := shouldAlignFilesWithCandidate(sourceRelease, matchedRelease) && (needsRootRename || needsFileRename)

	// Wait for torrent to become visible in qBittorrent before attempting renames
	activeHash := s.waitForTorrentAvailability(ctx, instanceID, hashes, crossSeedRenameWaitTimeout)
	if activeHash == "" {
		if !alignmentNeeded {
			log.Debug().
				Int("instanceID", instanceID).
				Str("torrentHash", hashLabel).
				Msg("Cross-seed torrent not visible yet, no alignment needed")
			return true, ""
		}
		log.Warn().
			Int("instanceID", instanceID).
			Str("torrentHash", hashLabel).
			Msg("Cross-seed torrent not visible yet, skipping rename alignment")
		return false, ""
	}

	canonicalHash := normalizeHash(activeHash)

	trimmedSourceName := strings.TrimSpace(sourceTorrentName)
	trimmedMatchedName := strings.TrimSpace(matchedTorrent.Name)

	// Detect single-file → folder case (using expected files, before any qBittorrent updates)
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
		if err := s.syncManager.RenameTorrent(ctx, instanceID, activeHash, trimmedMatchedName); err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", activeHash).
				Msg("Failed to rename cross-seed torrent display name (cosmetic, continuing)")
		} else {
			log.Debug().
				Int("instanceID", instanceID).
				Str("torrentHash", activeHash).
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
		return true, activeHash // Episode-in-pack uses season pack path directly, no alignment needed
	}

	// Try to get current files from qBittorrent with a few retries for slow clients.
	// On slow clients, the torrent may be visible but files not yet populated.
	var sourceFiles qbt.TorrentFiles
	refreshCtx := qbittorrent.WithForceFilesRefresh(ctx)
	for attempt := range 3 {
		if ctx.Err() != nil {
			break
		}
		filesMap, err := s.syncManager.GetTorrentFilesBatch(refreshCtx, instanceID, []string{activeHash})
		if err != nil {
			log.Debug().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", activeHash).
				Int("attempt", attempt+1).
				Msg("Failed to get torrent files, retrying")
		} else if currentFiles, ok := filesMap[canonicalHash]; ok && len(currentFiles) > 0 {
			sourceFiles = currentFiles
			log.Debug().
				Int("instanceID", instanceID).
				Str("torrentHash", activeHash).
				Int("fileCount", len(currentFiles)).
				Msg("Got torrent files from qBittorrent")
			break
		} else {
			log.Trace().
				Int("instanceID", instanceID).
				Str("torrentHash", activeHash).
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
				Str("torrentHash", activeHash).
				Msg("Context cancelled while getting torrent files, using expected source files")
		} else {
			log.Debug().
				Int("instanceID", instanceID).
				Str("torrentHash", activeHash).
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
		if !s.renameFileWithVerification(ctx, instanceID, activeHash, actualOldPath, actualNewPath) {
			log.Warn().
				Int("instanceID", instanceID).
				Str("torrentHash", activeHash).
				Str("from", actualOldPath).
				Str("to", actualNewPath).
				Msg("Failed to rename cross-seed file after retries, aborting alignment")
			return false, activeHash
		}
		renamed++
	}

	// Rename folder AFTER file renames are complete.
	// Now that files have their target names within the source folder, renaming the
	// folder will update all paths to match the candidate torrent's layout, pointing
	// to the existing files on disk (from the matched torrent).
	rootRenamed := false
	if sourceRoot != "" && targetRoot != "" && sourceRoot != targetRoot {
		if err := s.syncManager.RenameTorrentFolder(ctx, instanceID, activeHash, sourceRoot, targetRoot); err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", activeHash).
				Str("from", sourceRoot).
				Str("to", targetRoot).
				Msg("Failed to rename cross-seed root folder")
			return false, activeHash
		}
		rootRenamed = true
		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", activeHash).
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
		return true, activeHash // No renames needed - paths already match or recheck will verify
	}

	if renamed == 0 && !rootRenamed {
		return true, activeHash // No renames performed (paths already match or renames not required)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Str("torrentHash", activeHash).
		Int("fileRenames", renamed).
		Bool("folderRenamed", rootRenamed).
		Msg("Aligned cross-seed torrent naming with existing torrent")

	if len(unmatched) > 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", activeHash).
			Int("unmatchedFiles", len(unmatched)).
			Msg("Some cross-seed files could not be mapped to existing files and will keep their original names")
	}

	return true, activeHash
}

func (s *Service) waitForTorrentAvailability(ctx context.Context, instanceID int, hashes []string, timeout time.Duration) string {
	hashes = dedupeHashes(hashes...)
	if len(hashes) == 0 {
		return ""
	}

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ""
		}

		if qbtSyncManager, err := s.syncManager.GetQBittorrentSyncManager(ctx, instanceID); err == nil && qbtSyncManager != nil {
			if err := qbtSyncManager.Sync(ctx); err != nil {
				log.Debug().
					Err(err).
					Int("instanceID", instanceID).
					Msg("Failed to sync while waiting for cross-seed torrent availability, retrying")
			}
		}

		torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{Hashes: hashes})
		if err == nil && len(torrents) > 0 {
			for _, torrent := range torrents {
				active := strings.TrimSpace(torrent.Hash)
				if active != "" {
					return active
				}
			}
			return hashes[0]
		} else if err != nil {
			log.Debug().
				Err(err).
				Int("instanceID", instanceID).
				Msg("Failed to get torrents while waiting for cross-seed torrent availability, retrying")
		}

		time.Sleep(crossSeedRenamePollInterval)
	}

	return ""
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
	// Keep episode torrents named after the episode even when pointing at season pack files.
	if isTVEpisode(newRelease) && isTVSeasonPack(matchedRelease) {
		return false
	}
	// Keep season pack torrents named as season packs; never rename a season pack to an episode.
	// This is the forbidden pairing (season pack from episode) - also reject rename.
	if reject, _ := rejectSeasonPackFromEpisode(newRelease, matchedRelease, true); reject {
		return false
	}
	return true
}

func shouldAlignFilesWithCandidate(newRelease, matchedRelease *rls.Release) bool {
	// Episode pointing at season pack files: don't align files (episode uses subset of pack).
	if isTVEpisode(newRelease) && isTVSeasonPack(matchedRelease) {
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

// hasContentFileSizeMismatch checks if files that exist in both source and candidate (matched
// by normalized filename) have matching sizes. Returns true only if a file exists in both but
// with different sizes, indicating possible corruption or a different release.
//
// This uses matched-files-only logic: extra files in source (files with no matching name in
// candidate) are NOT treated as mismatches. Such extras are handled downstream by the
// piece-boundary safety check and recheck/auto-resume logic.
//
// Significance check: we verify that among source files that HAVE corresponding keys in
// candidate, the largest one was successfully matched AND is at least 1MB. This ignores
// truly "extra" source files (those with no candidate key) when assessing match quality,
// and prevents tiny sidecars from being the sole basis for trusting a match.
//
// Safety fallback: when matches aren't significant (only tiny files matched, or no keys
// matched at all), we compare the largest files in each set to catch obvious mismatches.
//
// The function also returns a list of mismatched files for logging purposes.
func hasContentFileSizeMismatch(sourceFiles, candidateFiles qbt.TorrentFiles, normalizer *stringutils.Normalizer[string, string]) (bool, []string) {
	// Filter files by ignore patterns
	var filteredSource, filteredCandidate qbt.TorrentFiles

	for _, sf := range sourceFiles {
		if !shouldIgnoreFile(sf.Name, normalizer) {
			filteredSource = append(filteredSource, sf)
		}
	}

	for _, cf := range candidateFiles {
		if !shouldIgnoreFile(cf.Name, normalizer) {
			filteredCandidate = append(filteredCandidate, cf)
		}
	}

	// If no content files remain in source after filtering, nothing to check
	if len(filteredSource) == 0 {
		return false, nil
	}

	// Build a map of normalized key -> size for candidate files.
	// For duplicate keys (rare), store all sizes to handle multisets.
	type candidateInfo struct {
		sizes []int64
	}
	candidateByKey := make(map[string]*candidateInfo)
	for _, cf := range filteredCandidate {
		key := normalizeFileKey(cf.Name)
		if key == "" {
			continue
		}
		if info := candidateByKey[key]; info != nil {
			info.sizes = append(info.sizes, cf.Size)
		} else {
			candidateByKey[key] = &candidateInfo{sizes: []int64{cf.Size}}
		}
	}

	// Check each source file: only flag mismatch if the same normalized key exists
	// in candidate but with a different size.
	// Track: largest matched file, and largest source file that has a key in candidate.
	var mismatchedFiles []string
	var largestMatched int64
	var largestSourceWithKey int64

	for _, sf := range filteredSource {
		sourceKey := normalizeFileKey(sf.Name)
		if sourceKey == "" {
			continue
		}

		info := candidateByKey[sourceKey]
		if info == nil {
			// No candidate file with this key - this is an "extra" file in source.
			// Not a mismatch; downstream checks (piece-boundary, recheck) handle this.
			continue
		}

		// Track the largest source file that has a corresponding key in candidate
		if sf.Size > largestSourceWithKey {
			largestSourceWithKey = sf.Size
		}

		// Check if any size matches
		sizeMatched := false
		for i, candSize := range info.sizes {
			if candSize == sf.Size {
				// Remove this size from available (for multiset correctness)
				info.sizes = slices.Delete(info.sizes, i, i+1)
				sizeMatched = true
				if sf.Size > largestMatched {
					largestMatched = sf.Size
				}
				break
			}
		}

		if !sizeMatched {
			// Same file key but different size - true mismatch
			mismatchedFiles = append(mismatchedFiles, sf.Name)
		}
	}

	// If we have explicit mismatches (same key, different size), report them
	if len(mismatchedFiles) > 0 {
		return true, mismatchedFiles
	}

	// Check if we matched the source's main matchable content: the largest matched file
	// should equal the largest source file that has a corresponding key in candidate.
	// This ignores "extra" source files (files with no candidate key) when checking
	// significance, so source torrents with large extras won't trigger false alarms.
	//
	// Additionally, require the matched content to be substantial (≥1MB) to avoid trusting
	// matches based solely on tiny sidecars like .sfv files.
	const minSubstantialSize int64 = 1 << 20 // 1MB
	substantialMatch := largestMatched >= largestSourceWithKey &&
		largestSourceWithKey > 0 &&
		largestMatched >= minSubstantialSize
	if substantialMatch {
		return false, nil // Source's main matchable content was matched, no mismatch
	}

	// Fallback: either no source files had matching keys, or only tiny files (<1MB) matched.
	// Compare largest files to catch obvious mismatches from different naming or releases.
	var largestSource int64
	var largestSourceName string
	for _, sf := range filteredSource {
		if sf.Size > largestSource {
			largestSource = sf.Size
			largestSourceName = sf.Name
		}
	}
	var largestCandidate int64
	for _, cf := range filteredCandidate {
		if cf.Size > largestCandidate {
			largestCandidate = cf.Size
		}
	}

	// If largest files differ, treat as mismatch
	if largestSource > 0 && largestCandidate > 0 && largestSource != largestCandidate {
		return true, []string{largestSourceName}
	}

	return false, nil
}

// fileKeySize is a composite key for matching files by normalized name + size.
// This prevents mismatches when different files happen to have the same size
// (e.g., source has english.srt, candidate has spanish.srt, same size → should NOT match).
type fileKeySize struct {
	key  string
	size int64
}

// hasExtraSourceFiles checks if source torrent has files that don't exist in the candidate.
// This happens when source has extra sidecar files (NFO, SRT, etc.) that aren't in the candidate.
// Returns true if source has files with (normalizedKey, size) not present in candidate.
// This includes cases where source and candidate have the same file count but different files
// (e.g., source has mkv+srt, candidate has mkv+nfo - the srt won't exist on disk).
func hasExtraSourceFiles(sourceFiles, candidateFiles qbt.TorrentFiles) bool {
	// Build (normalizedKey, size) multiset for candidate files
	candidateKeys := make(map[fileKeySize]int)
	for _, cf := range candidateFiles {
		key := fileKeySize{key: normalizeFileKey(cf.Name), size: cf.Size}
		candidateKeys[key]++
	}

	// Count how many source files can be matched by (normalizedKey, size)
	matched := 0
	for _, sf := range sourceFiles {
		key := fileKeySize{key: normalizeFileKey(sf.Name), size: sf.Size}
		if count := candidateKeys[key]; count > 0 {
			candidateKeys[key]--
			matched++
		}
	}

	// If we couldn't match all source files, there are extras/mismatches
	return matched < len(sourceFiles)
}

// needsRenameAlignment checks if rename alignment will be required for a cross-seed add.
// Returns true if torrent name, root folder, or file names differ between source and candidate.
// For layout-change cases (folder→bare or bare→folder), also checks if file names inside differ.
func needsRenameAlignment(torrentName string, matchedTorrentName string, sourceFiles, candidateFiles qbt.TorrentFiles) bool {
	sourceRoot := detectCommonRoot(sourceFiles)
	candidateRoot := detectCommonRoot(candidateFiles)

	// Single file → folder: contentLayout=Subfolder handles adding folder structure,
	// but we need alignment if:
	// 1. The auto-generated folder name differs from candidate's folder
	// 2. File names differ
	if sourceRoot == "" && candidateRoot != "" {
		// qBittorrent auto-generates folder by stripping extension from the single file's name.
		// Only applies to single-file rootless torrents (multi-file rootless is rare/invalid).
		if len(sourceFiles) == 1 {
			sourceFileName := fileBaseName(sourceFiles[0].Name)
			autoGeneratedFolder := strings.TrimSuffix(sourceFileName, filepath.Ext(sourceFileName))
			if autoGeneratedFolder != candidateRoot {
				return true // Folder will need renaming after add
			}
		}
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

	return planRequiresRenames(sourceFiles, candidateFiles)
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

// planRequiresRenames checks if the rename plan contains any actual path changes.
// Uses buildFileRenamePlan which compares full paths (including subfolder structure),
// not just base filenames.
func planRequiresRenames(sourceFiles, candidateFiles qbt.TorrentFiles) bool {
	plan, _ := buildFileRenamePlan(sourceFiles, candidateFiles)
	for _, instr := range plan {
		if instr.oldPath != "" && instr.newPath != "" && instr.oldPath != instr.newPath {
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
