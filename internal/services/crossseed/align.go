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

	"github.com/autobrr/qui/internal/qbittorrent"
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

func (s *Service) alignCrossSeed(
	ctx context.Context,
	instanceID int,
	torrentHash string,
	torrentHashV2 string,
	sourceTorrentName string,
	addPlan *crossSeedAddPlan,
) (bool, string) {
	if addPlan != nil && len(addPlan.filePairs) > 0 {
		return s.alignCatalogFiles(ctx, instanceID, torrentHash, torrentHashV2, addPlan)
	}
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

	isSingleFileToFolder := expectedSourceRoot == "" && expectedCandidateRoot != ""

	shouldRename := shouldRenameTorrentDisplay(sourceRelease, matchedRelease) &&
		trimmedMatchedName != "" &&
		trimmedSourceName != trimmedMatchedName &&
		(!isSingleFileToFolder || !namesMatchIgnoringExtension(trimmedSourceName, trimmedMatchedName))

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
		return true, activeHash
	}

	sourceRoot := detectCommonRoot(sourceFiles)
	targetRoot := detectCommonRoot(candidateFiles)
	rootDiffers := sourceRoot != "" && targetRoot != "" && sourceRoot != targetRoot

	plan, unmatched := buildFileRenamePlan(sourceFiles, candidateFiles)

	var alignPlan AlignmentPlan
	for _, instr := range plan {
		if instr.oldPath == instr.newPath || instr.oldPath == "" || instr.newPath == "" {
			continue
		}

		actualOldPath := instr.oldPath
		actualNewPath := instr.newPath
		if rootDiffers {
			actualNewPath = adjustPathForRootRename(instr.newPath, targetRoot, sourceRoot)
		} else if sourceRoot == "" && targetRoot != "" {
			actualNewPath = fileBaseName(instr.newPath)
		}

		if actualOldPath == actualNewPath {
			continue
		}
		alignPlan.Files = append(alignPlan.Files, FileRename{OldPath: actualOldPath, NewPath: actualNewPath})
	}

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
			log.Debug().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Int("unmatchedFiles", len(unmatched)).
				Strs("unmatchedPaths", unmatched).
				Msg("Some cross-seed files could not be mapped, recheck will handle verification")
		}
		return true, activeHash
	}

	if renamed == 0 && !rootRenamed {
		return true, activeHash
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

		if qbtSyncManager, err := s.syncManager.GetSyncOwner(ctx, instanceID); err == nil && qbtSyncManager != nil {
			if err := qbtSyncManager.Sync(ctx); err != nil {
				log.Debug().
					Err(err).
					Int("instanceID", instanceID).
					Msg("Failed to sync while waiting for cross-seed torrent availability, retrying")
			}
		}

		torrents, err := s.syncManager.GetTorrentRefs(ctx, instanceID, qbittorrent.TorrentRefFilter{Hashes: hashes})
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

		for _, cand := range available {
			if cand.path == sf.Name {
				match = cand
				break
			}
		}

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

	if ext == "nfo" || ext == "srt" || ext == "sub" || ext == "idx" || ext == "sfv" || ext == "txt" {
		if dot := strings.LastIndex(base, "."); dot >= 0 && dot < len(base)-1 {
			videoExt := strings.ToLower(base[dot+1:])
			switch videoExt {
			case "mkv", "mp4", "avi", "ts", "m2ts", "mov", "mpg", "mpeg":
				base = base[:dot]
			}
		}
	}

	base = stringutils.NormalizeUnicode(base)
	base = strings.ToLower(base)

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
	if isTVEpisode(newRelease) && isTVSeasonPack(matchedRelease) {
		return false
	}
	if reject, _ := rejectSeasonPackFromEpisode(newRelease, matchedRelease); reject {
		return false
	}
	return true
}

func shouldAlignFilesWithCandidate(newRelease, matchedRelease *rls.Release) bool {
	if isTVEpisode(newRelease) && isTVSeasonPack(matchedRelease) {
		return false
	}
	return true
}

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

type contentFileSizeMismatch struct {
	SourceFile    string `json:"sourceFile"`
	SourceSize    int64  `json:"sourceSize"`
	CandidateFile string `json:"candidateFile"`
	CandidateSize int64  `json:"candidateSize"`
}

func hasContentFileSizeMismatch(sourceFiles, candidateFiles qbt.TorrentFiles, normalizer *stringutils.Normalizer[string, string]) (bool, []contentFileSizeMismatch) {
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

	if len(filteredSource) == 0 {
		return false, nil
	}

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
			continue
		}

		if sf.Size > largestSourceWithKey {
			largestSourceWithKey = sf.Size
		}

		sizeMatched := false
		for i, candidateFile := range info.files {
			if candidateFile.size == sf.Size {
				info.files = slices.Delete(info.files, i, i+1)
				sizeMatched = true
				if sf.Size > largestMatched {
					largestMatched = sf.Size
				}
				break
			}
		}

		if !sizeMatched {
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

	if len(mismatches) > 0 {
		return true, mismatches
	}

	const minSubstantialSize int64 = 1 << 20
	substantialMatch := largestMatched >= largestSourceWithKey &&
		largestSourceWithKey > 0 &&
		largestMatched >= minSubstantialSize
	if substantialMatch {
		return false, nil
	}

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

func hasExtraSourceFiles(sourceFiles, candidateFiles qbt.TorrentFiles) bool {
	_, unmatched := matchMaterializedSourceFilesToCandidates(sourceFiles, candidateFiles)
	return len(unmatched) > 0
}

func autoGeneratedSubfolderName(singleFileName string) string {
	base := fileBaseName(singleFileName)
	return strings.TrimSuffix(base, path.Ext(base))
}

func needsRenameAlignment(torrentName string, matchedTorrentName string, sourceFiles, candidateFiles qbt.TorrentFiles) bool {
	sourceRoot := detectCommonRoot(sourceFiles)
	candidateRoot := detectCommonRoot(candidateFiles)

	if sourceRoot == "" && candidateRoot != "" {
		if len(sourceFiles) == 1 {
			if autoGeneratedSubfolderName(sourceFiles[0].Name) != candidateRoot {
				return true
			}
		}
		return filesNeedRenaming(sourceFiles, candidateFiles)
	}

	if sourceRoot != "" && candidateRoot == "" {
		return filesNeedRenaming(sourceFiles, candidateFiles)
	}

	trimmedSourceName := strings.TrimSpace(torrentName)
	trimmedMatchedName := strings.TrimSpace(matchedTorrentName)
	if trimmedSourceName != trimmedMatchedName {
		return true
	}

	if sourceRoot != "" && candidateRoot != "" && sourceRoot != candidateRoot {
		return true
	}

	return planRequiresRenames(sourceFiles, candidateFiles)
}

func filesNeedRenaming(sourceFiles, candidateFiles qbt.TorrentFiles) bool {
	if len(sourceFiles) == 0 || len(candidateFiles) == 0 {
		return false
	}

	type fileKey struct {
		normalized string
		size       int64
	}
	candidateKeys := make(map[fileKey]bool)
	for _, cf := range candidateFiles {
		candidateKeys[fileKey{normalized: normalizeFileKey(cf.Name), size: cf.Size}] = true
	}

	for _, sf := range sourceFiles {
		key := fileKey{normalized: normalizeFileKey(sf.Name), size: sf.Size}
		if !candidateKeys[key] {
			return true
		}
	}

	candidateBuckets := make(map[int64][]string)
	for _, cf := range candidateFiles {
		base := fileBaseName(cf.Name)
		candidateBuckets[cf.Size] = append(candidateBuckets[cf.Size], base)
	}

	for _, sf := range sourceFiles {
		sourceBase := fileBaseName(sf.Name)
		bucket := candidateBuckets[sf.Size]

		found := slices.Contains(bucket, sourceBase)
		if !found {
			return true
		}
	}

	return false
}

func planRequiresRenames(sourceFiles, candidateFiles qbt.TorrentFiles) bool {
	plan, _ := buildFileRenamePlan(sourceFiles, candidateFiles)
	for _, instr := range plan {
		if instr.oldPath != "" && instr.newPath != "" && instr.oldPath != instr.newPath {
			return true
		}
	}
	return false
}
