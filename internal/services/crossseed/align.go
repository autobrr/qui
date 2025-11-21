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
)

type fileRenameInstruction struct {
	oldPath string
	newPath string
}

// alignCrossSeedContentPaths renames the incoming cross-seed torrent (display name, folders, files)
// so that it matches the layout of the already-seeded torrent we're borrowing data from.
func (s *Service) alignCrossSeedContentPaths(
	ctx context.Context,
	instanceID int,
	torrentHash string,
	sourceTorrentName string,
	matchedTorrent *qbt.Torrent,
	expectedSourceFiles qbt.TorrentFiles,
	candidateFiles qbt.TorrentFiles,
) {
	if matchedTorrent == nil {
		return
	}

	sourceRelease := s.releaseCache.Parse(sourceTorrentName)
	matchedRelease := s.releaseCache.Parse(matchedTorrent.Name)

	if len(expectedSourceFiles) == 0 || len(candidateFiles) == 0 {
		return
	}

	if !s.waitForTorrentAvailability(ctx, instanceID, torrentHash, crossSeedRenameWaitTimeout) {
		log.Warn().
			Int("instanceID", instanceID).
			Str("torrentHash", torrentHash).
			Msg("Cross-seed torrent not visible yet, skipping rename alignment")
		return
	}

	trimmedSourceName := strings.TrimSpace(sourceTorrentName)
	trimmedMatchedName := strings.TrimSpace(matchedTorrent.Name)
	if shouldRenameTorrentDisplay(sourceRelease, matchedRelease) && trimmedMatchedName != "" && trimmedSourceName != trimmedMatchedName {
		if err := s.syncManager.RenameTorrent(ctx, instanceID, torrentHash, trimmedMatchedName); err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Msg("Failed to rename cross-seed torrent to match existing torrent name")
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
		return
	}

	sourceFiles := expectedSourceFiles
	if currentFiles, err := s.getTorrentFilesFromStash(ctx, instanceID, torrentHash); err == nil && len(currentFiles) > 0 {
		sourceFiles = currentFiles
	}

	sourceRoot := detectCommonRoot(sourceFiles)
	targetRoot := detectCommonRoot(candidateFiles)

	rootRenamed := false
	if sourceRoot != "" && targetRoot != "" && sourceRoot != targetRoot {
		if err := s.syncManager.RenameTorrentFolder(ctx, instanceID, torrentHash, sourceRoot, targetRoot); err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Str("from", sourceRoot).
				Str("to", targetRoot).
				Msg("Failed to rename cross-seed root folder to match existing torrent")
		} else {
			rootRenamed = true
			log.Debug().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Str("from", sourceRoot).
				Str("to", targetRoot).
				Msg("Renamed cross-seed root folder to match existing torrent")
		}
	}

	plan, unmatched := buildFileRenamePlan(sourceFiles, candidateFiles)
	if len(plan) == 0 {
		if len(unmatched) > 0 {
			log.Debug().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Int("unmatchedFiles", len(unmatched)).
				Msg("Skipping cross-seed file renames because no confident mappings were found")
		}
		return
	}

	renamed := 0
	for _, instr := range plan {
		oldPath := instr.oldPath
		if rootRenamed {
			oldPath = adjustPathForRootRename(oldPath, sourceRoot, targetRoot)
		}

		if oldPath == instr.newPath || oldPath == "" || instr.newPath == "" {
			continue
		}

		if err := s.syncManager.RenameTorrentFile(ctx, instanceID, torrentHash, oldPath, instr.newPath); err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Str("from", oldPath).
				Str("to", instr.newPath).
				Msg("Failed to rename cross-seed file to match existing torrent")
			continue
		}
		renamed++
	}

	if renamed == 0 && !rootRenamed {
		return
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

		torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
		if err == nil {
			for _, t := range torrents {
				if t.Hash == hash || t.InfohashV1 == hash || t.InfohashV2 == hash {
					return true
				}
			}
		} else {
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
	prefix := oldRoot + "/"
	if strings.HasPrefix(path, prefix) {
		return newRoot + "/" + strings.TrimPrefix(path, prefix)
	}
	return path
}

func shouldRenameTorrentDisplay(newRelease, matchedRelease rls.Release) bool {
	// Keep episode torrents named after the episode even when pointing at season pack files
	if newRelease.Series > 0 && newRelease.Episode > 0 &&
		matchedRelease.Series > 0 && matchedRelease.Episode == 0 {
		return false
	}
	return true
}

func shouldAlignFilesWithCandidate(newRelease, matchedRelease rls.Release) bool {
	if newRelease.Series > 0 && newRelease.Episode > 0 &&
		matchedRelease.Series > 0 && matchedRelease.Episode == 0 {
		return false
	}
	return true
}
