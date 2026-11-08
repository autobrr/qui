// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"path"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/pkg/stringutils"
)

type fileRenameInstruction struct {
	oldPath string
	newPath string
}

type fileMatchInstruction struct {
	sourceIndex   int
	sourcePath    string
	candidatePath string
}

// alignCrossSeed renames the incoming cross-seed torrent (display name, folders, files)
// so that it matches the layout of the already-seeded torrent we're borrowing data from.
// Returns true if alignment succeeded (or wasn't needed), false if alignment failed,
// along with the active hash that qBittorrent exposes for the torrent.
func (s *Service) alignCrossSeed(
	ctx context.Context,
	instanceID int,
	torrentHash string,
	torrentHashV2 string,
	sourceTorrentName string,
	addPlan *crossSeedAddPlan,
) (bool, string) {
	hashes := dedupeHashes(torrentHash, torrentHashV2)
	hashLabel := ""
	if len(hashes) > 0 {
		hashLabel = hashes[0]
	}

	if addPlan == nil {
		log.Debug().
			Int("instanceID", instanceID).
			Str("torrentHash", hashLabel).
			Msg("alignCrossSeed called with nil add plan")
		return false, ""
	}

	matchedTorrent := &addPlan.torrent
	expectedSourceFiles := addPlan.sourceFiles
	candidateFiles := addPlan.files
	sourceRelease := addPlan.sourceRelease
	matchedRelease := addPlan.candidateRelease

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

	// Wait for the torrent and read the file names that qBittorrent stored: the
	// content layout can change them from the names in the torrent file.
	activeHash, sourceFiles, visible := s.aligner.WaitForFiles(ctx, instanceID, hashes)
	if !visible {
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
		(!isSingleFileToFolder || !namesMatchIgnoringExtension(trimmedSourceName, trimmedMatchedName))

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

	sourceRoot := detectCommonRoot(sourceFiles)
	targetRoot := detectCommonRoot(candidateFiles)
	rootDiffers := sourceRoot != "" && targetRoot != "" && sourceRoot != targetRoot

	// Build file rename plan using original source paths (before any folder rename).
	// The plan maps source files to candidate files based on size matching.
	plan, unmatched := buildFileRenamePlan(sourceFiles, candidateFiles)

	// Rename files FIRST (while still in original folder structure).
	// This must happen before folder rename to avoid disk conflicts: if we rename the
	// folder first, then try to rename a file to a name that already exists on disk
	// (from the matched torrent), qBittorrent will fail with "newPath already in use".
	// By renaming files first within the original folder, we're just updating metadata
	// (the original folder doesn't exist on disk anyway for a fresh cross-seed add).
	var alignPlan AlignmentPlan
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
		if rootDiffers {
			// Adjust newPath to stay in source folder (file rename only changes the filename)
			actualNewPath = adjustPathForRootRename(instr.newPath, targetRoot, sourceRoot)
		} else if sourceRoot == "" && targetRoot != "" {
			// The torrent was added directly into the matched content folder, so qBittorrent
			// file paths are rootless even though the matched torrent includes a root folder.
			actualNewPath = fileBaseName(instr.newPath)
		}

		if actualOldPath == actualNewPath {
			continue
		}
		alignPlan.Files = append(alignPlan.Files, FileRename{OldPath: actualOldPath, NewPath: actualNewPath})
	}

	// Rename folder AFTER file renames are complete.
	// Now that files have their target names within the source folder, renaming the
	// folder will update all paths to match the candidate torrent's layout, pointing
	// to the existing files on disk (from the matched torrent).
	if rootDiffers {
		alignPlan.FolderFrom, alignPlan.FolderTo = sourceRoot, targetRoot
	}

	if !s.aligner.Align(ctx, instanceID, activeHash, alignPlan) {
		log.Warn().
			Int("instanceID", instanceID).
			Str("torrentHash", activeHash).
			Msg("Failed to align cross-seed files, aborting alignment")
		return false, activeHash
	}
	renamed := len(alignPlan.Files)
	rootRenamed := alignPlan.FolderFrom != ""

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

func matchSourceFilesToCandidates(sourceFiles, candidateFiles qbt.TorrentFiles) ([]fileMatchInstruction, []string) {
	return matchSourceFilesToCandidatesWithPolicy(sourceFiles, candidateFiles, func(_, _ string) bool {
		return true
	})
}

func matchMaterializedSourceFilesToCandidates(sourceFiles, candidateFiles qbt.TorrentFiles) ([]fileMatchInstruction, []string) {
	return matchSourceFilesToCandidatesWithPolicy(sourceFiles, candidateFiles, allowMaterializedSizeOnlyMatch)
}

// exactUsableFilePairing requires every non-ignored file to have one exact-size partner.
func exactUsableFilePairing(sourceFiles, candidateFiles qbt.TorrentFiles, normalizer *stringutils.Normalizer[string, string]) bool {
	filteredSource := make(qbt.TorrentFiles, 0, len(sourceFiles))
	for _, file := range sourceFiles {
		if !shouldIgnoreFile(file.Name, normalizer) {
			filteredSource = append(filteredSource, file)
		}
	}
	filteredCandidate := make(qbt.TorrentFiles, 0, len(candidateFiles))
	for _, file := range candidateFiles {
		if !shouldIgnoreFile(file.Name, normalizer) {
			filteredCandidate = append(filteredCandidate, file)
		}
	}
	if len(filteredSource) == 0 || len(filteredSource) != len(filteredCandidate) {
		return false
	}
	matches, unmatched := matchSourceFilesToCandidates(filteredSource, filteredCandidate)
	return len(unmatched) == 0 && len(matches) == len(filteredSource)
}

func matchSourceFilesToCandidatesWithPolicy(
	sourceFiles,
	candidateFiles qbt.TorrentFiles,
	allowSoleCandidateMatch func(sourcePath, candidatePath string) bool,
) ([]fileMatchInstruction, []string) {
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

	matches := make([]fileMatchInstruction, 0, len(sourceFiles))
	unmatched := make([]string, 0)

	for sourceIndex, sf := range sourceFiles {
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

		// If only one candidate remains for this size, use it when the caller
		// accepts size-only matching for this pair.
		if match == nil && len(available) == 1 && allowSoleCandidateMatch(sf.Name, available[0].path) {
			match = available[0]
		}

		if match == nil {
			unmatched = append(unmatched, sf.Name)
			continue
		}

		match.used = true
		matches = append(matches, fileMatchInstruction{
			sourceIndex:   sourceIndex,
			sourcePath:    sf.Name,
			candidatePath: match.path,
		})
	}

	return matches, unmatched
}

func allowMaterializedSizeOnlyMatch(sourcePath, candidatePath string) bool {
	return !shouldIgnoreFile(sourcePath, stringutils.DefaultNormalizer) && !shouldIgnoreFile(candidatePath, stringutils.DefaultNormalizer)
}

func buildFileRenamePlan(sourceFiles, candidateFiles qbt.TorrentFiles) ([]fileRenameInstruction, []string) {
	matches, unmatched := matchSourceFilesToCandidates(sourceFiles, candidateFiles)
	plan := make([]fileRenameInstruction, 0, len(matches))

	for _, match := range matches {
		if match.sourcePath == match.candidatePath {
			continue
		}

		plan = append(plan, fileRenameInstruction{
			oldPath: match.sourcePath,
			newPath: match.candidatePath,
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
	base = strings.ToLower(base)

	// Dolby Digital Plus commonly appears as either DDP or DD+ depending on indexer naming.
	// Normalize to the same token so equivalent files match by key.
	base = strings.ReplaceAll(base, "dd+", "ddp")

	var b strings.Builder
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
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
	if reject, _ := rejectSeasonPackFromEpisode(newRelease, matchedRelease); reject {
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
type contentFileSizeMismatch struct {
	SourceFile    string `json:"sourceFile"`
	SourceSize    int64  `json:"sourceSize"`
	CandidateFile string `json:"candidateFile"`
	CandidateSize int64  `json:"candidateSize"`
}

// The function also returns mismatch details for logging purposes.
func hasContentFileSizeMismatch(sourceFiles, candidateFiles qbt.TorrentFiles, normalizer *stringutils.Normalizer[string, string]) (bool, []contentFileSizeMismatch) {
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
	type candidateFileInfo struct {
		name string
		size int64
	}
	type candidateInfo struct {
		files    []candidateFileInfo
		allFiles []candidateFileInfo
	}
	candidateByKey := make(map[string]*candidateInfo)
	for _, cf := range filteredCandidate {
		key := normalizeFileKey(cf.Name)
		if key == "" {
			continue
		}
		fileInfo := candidateFileInfo{name: cf.Name, size: cf.Size}
		if info := candidateByKey[key]; info != nil {
			info.files = append(info.files, fileInfo)
			info.allFiles = append(info.allFiles, fileInfo)
		} else {
			candidateByKey[key] = &candidateInfo{
				files:    []candidateFileInfo{fileInfo},
				allFiles: []candidateFileInfo{fileInfo},
			}
		}
	}

	// Check each source file: only flag mismatch if the same normalized key exists
	// in candidate but with a different size.
	// Track: largest matched file, and largest source file that has a key in candidate.
	var mismatches []contentFileSizeMismatch
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
		for i, candidateFile := range info.files {
			if candidateFile.size == sf.Size {
				// Remove this size from available (for multiset correctness)
				info.files = slices.Delete(info.files, i, i+1)
				sizeMatched = true
				if sf.Size > largestMatched {
					largestMatched = sf.Size
				}
				break
			}
		}

		if !sizeMatched {
			// Same file key but different size - true mismatch
			candidateFile := info.allFiles[0]
			if len(info.files) > 0 {
				candidateFile = info.files[0]
			}
			mismatches = append(mismatches, contentFileSizeMismatch{
				SourceFile:    sf.Name,
				SourceSize:    sf.Size,
				CandidateFile: candidateFile.name,
				CandidateSize: candidateFile.size,
			})
		}
	}

	// If we have explicit mismatches (same key, different size), report them
	if len(mismatches) > 0 {
		return true, mismatches
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
	var largestCandidateName string
	for _, cf := range filteredCandidate {
		if cf.Size > largestCandidate {
			largestCandidate = cf.Size
			largestCandidateName = cf.Name
		}
	}

	// If largest files differ, treat as mismatch
	if largestSource > 0 && largestCandidate > 0 && largestSource != largestCandidate {
		return true, []contentFileSizeMismatch{{
			SourceFile:    largestSourceName,
			SourceSize:    largestSource,
			CandidateFile: largestCandidateName,
			CandidateSize: largestCandidate,
		}}
	}

	return false, nil
}

func contentFileSizeMismatchSourceFiles(mismatches []contentFileSizeMismatch) []string {
	files := make([]string, 0, len(mismatches))
	for _, mismatch := range mismatches {
		files = append(files, mismatch.SourceFile)
	}

	return files
}

// hasExtraSourceFiles checks if source torrent has files that don't exist in the candidate.
// This happens when source has extra sidecar files (NFO, SRT, etc.) that aren't in the candidate.
// It matches with the same policy the rename plan and link modes use: normalized name +
// size first, then a sole non-ignored candidate of identical size (a rename-only pair,
// mirroring hardlink strategy 4). Sidecars never match on size alone, so same-size
// english.srt vs spanish.srt still counts as an extra; ambiguous same-size buckets
// (RAR sets) stay unmatched.
func hasExtraSourceFiles(sourceFiles, candidateFiles qbt.TorrentFiles) bool {
	_, unmatched := matchMaterializedSourceFilesToCandidates(sourceFiles, candidateFiles)
	return len(unmatched) > 0
}

// autoGeneratedSubfolderName returns the folder qBittorrent generates when adding a rootless
// single-file torrent with contentLayout=Subfolder: the file base name with its extension
// stripped (e.g. "Movie.2015.mkv" -> "Movie.2015").
func autoGeneratedSubfolderName(singleFileName string) string {
	base := fileBaseName(singleFileName)
	return strings.TrimSuffix(base, path.Ext(base))
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
			if autoGeneratedSubfolderName(sourceFiles[0].Name) != candidateRoot {
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
