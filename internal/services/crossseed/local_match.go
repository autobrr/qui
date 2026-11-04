// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"syscall"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlink"
	"github.com/autobrr/qui/pkg/sharedextents"
)

// newLocalMatch creates a LocalMatch from instance and torrent data.
func newLocalMatch(instance *models.Instance, cached *qbittorrent.CrossInstanceTorrentView, matchType string) LocalMatch {
	return LocalMatch{
		InstanceID:    instance.ID,
		InstanceName:  instance.Name,
		Hash:          cached.Hash,
		Name:          cached.Name,
		Size:          cached.Size,
		Progress:      cached.Progress,
		SavePath:      cached.SavePath,
		ContentPath:   cached.ContentPath,
		Category:      cached.Category,
		Tags:          cached.Tags,
		State:         string(cached.State),
		Tracker:       cached.Tracker,
		TrackerHealth: string(cached.TrackerHealth),
		MatchType:     matchType,
	}
}

// localMatchContext holds source file data for file-level matching.
// Source files are fetched on first use, so a source without a link check
// reads no files.
type localMatchContext struct {
	ctx                   context.Context
	svc                   *Service
	sourceInstanceID      int
	sourceInstance        *models.Instance // scopes sourceFileIDs; nil reads as the qui host
	sourceHash            string
	sourceSavePath        string
	sourceContentPath     string
	sourceHasFileIdentity bool
	sourceLocal           bool
	// trustedIdentityOnly limits linked matches to instances with
	// TrustedIdentity. A delete dialog can add linked matches to the delete,
	// and identity that a remote host reports never expands a destructive
	// action.
	trustedIdentityOnly bool

	// Lazy-loaded source file data
	fetched        bool
	sourceFilesErr error            // Error from fetching/parsing source files
	sourceFiles    qbt.TorrentFiles // without the files that contentFiles removes
	sourceData     sameDataFiles

	// Lazy-loaded FileIDs of the source's hard-linked files (nlink > 1),
	// used to detect hardlinked cross-seed copies.
	fileIDsFetched bool
	sourceFileIDs  map[fsops.FileKey]struct{}

	// Local verification errors (first error per category, for strict mode).
	candidateFilesErr error
	verificationErr   error
}

// getSourceFiles lazily fetches and caches the source file list.
func (m *localMatchContext) getSourceFiles() error {
	if m.fetched {
		return m.sourceFilesErr
	}
	m.fetched = true

	srcFiles, err := m.svc.getTorrentFilesCached(m.ctx, m.sourceInstanceID, m.sourceHash)
	if err != nil {
		m.sourceFilesErr = err
		return err
	}

	// Treat empty file list as an error - qBittorrent should always return files for a valid torrent.
	// An empty list could indicate a stale/removed torrent or API issue, and silently skipping
	// overlap checks would produce false negatives in delete dialogs.
	if len(srcFiles) == 0 {
		m.sourceFilesErr = fmt.Errorf("source torrent %s returned empty file list", normalizeHash(m.sourceHash))
		return m.sourceFilesErr
	}

	normalizer := normalizerForService(m.svc)
	m.sourceFiles = contentFiles(srcFiles, normalizer)
	m.sourceData = newSameDataFiles(m.sourceSavePath, srcFiles, normalizer)
	return nil
}

// getSourceFileIDs lazily stats the source torrent's files on the local filesystem
// and caches the FileIDs of its hard-linked files (nlink > 1). Only files with
// extra links can be shared with another torrent, so nlink == 1 files are skipped.
// Fetch errors are recorded by getSourceFiles for strict mode; stat failures are
// best-effort skips since a missing file carries no hardlink evidence.
func (m *localMatchContext) getSourceFileIDs() map[fsops.FileKey]struct{} {
	if m.fileIDsFetched {
		return m.sourceFileIDs
	}
	m.fileIDsFetched = true

	if err := m.getSourceFiles(); err != nil {
		return nil
	}

	backend, err := m.svc.getBackendForInstance(m.ctx, m.sourceInstanceID, m.identity())
	if err != nil {
		// Unlike a per-file stat failure, a backend outage discards ALL
		// hardlink evidence for the instance — record it so strict mode
		// (delete dialogs) fails the check instead of reading "no cross-seeds".
		if m.verificationErr == nil {
			m.verificationErr = fmt.Errorf("resolve filesystem backend for instance %d: %w", m.sourceInstanceID, err)
		}
		return nil
	}

	ids := make(map[fsops.FileKey]struct{})
	if err := forEachLocalFileID(m.ctx, backend, m.sourceSavePath, m.sourceFiles, func(id hardlink.FileID, nlink uint64) bool {
		if key, ok := fsops.FileKeyOf(id, m.sourceInstance); ok && nlink > 1 {
			ids[key] = struct{}{}
		}
		return true
	}); err != nil && m.verificationErr == nil {
		m.verificationErr = fmt.Errorf("source torrent %s: %w", normalizeHash(m.sourceHash), err)
	}
	m.sourceFileIDs = ids
	return m.sourceFileIDs
}

func candidateSharesSourceFileID(
	ctx context.Context,
	backend fsops.Backend,
	candidateInstance *models.Instance,
	sourceIDs map[fsops.FileKey]struct{},
	candidateSavePath string,
	candidateFiles qbt.TorrentFiles,
) (bool, error) {
	shared := false
	err := forEachLocalFileID(ctx, backend, candidateSavePath, candidateFiles, func(id hardlink.FileID, _ uint64) bool {
		key, ok := fsops.FileKeyOf(id, candidateInstance)
		if _, found := sourceIDs[key]; ok && found {
			shared = true
			return false
		}
		return true
	})
	if shared {
		return true, nil
	}
	return false, err
}

// identity is the capability an instance needs for its file identity to
// count in this match. The backends ask for it again, since the instance
// list the match started from can be older than the row.
func (m *localMatchContext) identity() models.FilesystemCapability {
	if m.trustedIdentityOnly {
		return models.CapabilityTrustedIdentity
	}
	return models.CapabilityIdentity
}

func (s *Service) localLinkedMatchType(
	matchCtx *localMatchContext,
	candidateInstance *models.Instance,
	candidate *qbittorrent.CrossInstanceTorrentView,
) string {
	if matchCtx == nil || !matchCtx.sourceHasFileIdentity ||
		candidateInstance == nil || candidate == nil || candidate.ContentPath == "" {
		return ""
	}
	if !models.FilesystemCapabilitiesOf(candidateInstance).Has(matchCtx.identity()) {
		return ""
	}

	if err := matchCtx.getSourceFiles(); err != nil {
		return ""
	}
	sourceIDs := matchCtx.getSourceFileIDs()
	// The allocation check is a syscall on qui's own disk, so both paths must be local.
	var filesShareAllocation func(sourcePath, candidatePath string) (bool, error)
	if matchCtx.sourceLocal && models.FilesystemAccessMode(candidateInstance) == models.FilesystemModeLocal {
		filesShareAllocation = s.filesShareAllocation
		if filesShareAllocation == nil && sharedextents.Supported {
			filesShareAllocation = sharedextents.FilesShareAllocation
		}
	}
	if len(sourceIDs) == 0 && filesShareAllocation == nil {
		return ""
	}

	candidateFiles, ok := s.getLocalMatchCandidateFiles(matchCtx, candidate)
	if !ok {
		return ""
	}
	// A linked subtitle alone does not make a different video a cross-seed.
	candidateFiles = contentFiles(candidateFiles, normalizerForService(s))

	candidateBackend, err := s.getBackendForInstance(matchCtx.ctx, candidateInstance.ID, matchCtx.identity())
	if err != nil {
		// A backend outage discards all evidence for the candidate — record it
		// so strict mode fails the check instead of reading "no cross-seeds".
		if matchCtx.verificationErr == nil {
			matchCtx.verificationErr = fmt.Errorf("resolve filesystem backend for instance %d: %w", candidateInstance.ID, err)
		}
		return ""
	}

	shared, err := candidateSharesSourceFileID(matchCtx.ctx, candidateBackend, candidateInstance, sourceIDs, candidate.SavePath, candidateFiles)
	if shared {
		return matchTypeHardlink
	}
	if err != nil && matchCtx.verificationErr == nil {
		matchCtx.verificationErr = fmt.Errorf("candidate torrent %s: %w", normalizeHash(candidate.Hash), err)
	}

	if filesShareAllocation == nil {
		return ""
	}
	sourceBackend, err := s.getBackendForInstance(matchCtx.ctx, matchCtx.sourceInstanceID, matchCtx.identity())
	if err != nil {
		if matchCtx.verificationErr == nil {
			matchCtx.verificationErr = fmt.Errorf("resolve filesystem backend for instance %d: %w", matchCtx.sourceInstanceID, err)
		}
		return ""
	}
	pairs := pairLocalTorrentFiles(
		matchCtx.ctx,
		sourceBackend, matchCtx.sourceInstance,
		candidateBackend, candidateInstance,
		matchCtx.sourceSavePath,
		matchCtx.sourceFiles,
		candidate.SavePath,
		candidateFiles,
	)
	for _, pair := range pairs {
		shared, err := filesShareAllocation(pair.sourcePath, pair.candidatePath)
		if errors.Is(err, sharedextents.ErrUnsupported) {
			continue
		}
		if err != nil {
			if matchCtx.verificationErr == nil {
				matchCtx.verificationErr = err
			}
			return ""
		}
		if shared {
			return matchTypeReflink
		}
	}
	return ""
}

func (s *Service) getLocalMatchCandidateFiles(
	matchCtx *localMatchContext,
	candidate *qbittorrent.CrossInstanceTorrentView,
) (qbt.TorrentFiles, bool) {
	candidateFiles, err := s.getTorrentFilesCached(matchCtx.ctx, candidate.InstanceID, candidate.Hash)
	if err == nil && len(candidateFiles) == 0 {
		// An empty file list means a stale torrent or API issue, not evidence
		// that no linked files exist.
		err = fmt.Errorf("candidate torrent %s returned empty file list", normalizeHash(candidate.Hash))
	}
	if err != nil {
		if matchCtx.candidateFilesErr == nil {
			matchCtx.candidateFilesErr = err
		}
		return nil, false
	}
	return candidateFiles, true
}

// forEachLocalFileID stats each torrent file under savePath through the instance's
// filesystem backend and invokes fn with its FileID and link count until fn returns
// false. The save path must be absolute; file names that escape it are refused so
// malicious torrent metadata cannot probe arbitrary filesystem locations, and
// refusals are returned as an error because a name that resolves to nothing carries
// no evidence either way. Files that cannot be statted are skipped.
func forEachLocalFileID(ctx context.Context, backend fsops.Backend, savePath string, files qbt.TorrentFiles, fn func(id hardlink.FileID, nlink uint64) bool) error {
	return forEachLocalTorrentFile(ctx, backend, savePath, files, func(_ qbt.TorrentFile, _ string, info *fsops.LstatInfo) bool {
		if info.FileID.IsZero() {
			return true
		}
		return fn(info.FileID, info.Nlinks)
	})
}

type localFilePair struct {
	sourcePath    string
	candidatePath string
}

type localTorrentFile struct {
	file           qbt.TorrentFile
	fullPath       string
	fileID         fsops.FileKey
	hasFileID      bool
	normalizedPath string
	basename       string
}

func pairLocalTorrentFiles(
	ctx context.Context,
	sourceBackend fsops.Backend,
	sourceInstance *models.Instance,
	candidateBackend fsops.Backend,
	candidateInstance *models.Instance,
	sourceSavePath string,
	sourceFiles qbt.TorrentFiles,
	candidateSavePath string,
	candidateFiles qbt.TorrentFiles,
) []localFilePair {
	source := collectLocalTorrentFiles(ctx, sourceBackend, sourceInstance, sourceSavePath, sourceFiles)
	candidate := collectLocalTorrentFiles(ctx, candidateBackend, candidateInstance, candidateSavePath, candidateFiles)

	sourceByPath := indexLocalFiles(source, func(file localTorrentFile) string {
		return localFileSizeKey(file.normalizedPath, file.file.Size)
	})
	candidateByPath := indexLocalFiles(candidate, func(file localTorrentFile) string {
		return localFileSizeKey(file.normalizedPath, file.file.Size)
	})
	sourceByBasename := indexLocalFiles(source, func(file localTorrentFile) string {
		return localFileSizeKey(file.basename, file.file.Size)
	})
	candidateByBasename := indexLocalFiles(candidate, func(file localTorrentFile) string {
		return localFileSizeKey(file.basename, file.file.Size)
	})

	var pairs []localFilePair
	pairedSource := make(map[int]struct{})
	pairedCandidate := make(map[int]struct{})
	for key, sourceIndexes := range sourceByPath {
		candidateIndexes := candidateByPath[key]
		if len(sourceIndexes) != 1 || len(candidateIndexes) != 1 {
			continue
		}
		sourceIndex := sourceIndexes[0]
		candidateIndex := candidateIndexes[0]
		if sameLocalFile(source[sourceIndex], candidate[candidateIndex]) {
			continue
		}
		pairs = append(pairs, localFilePair{
			sourcePath:    source[sourceIndex].fullPath,
			candidatePath: candidate[candidateIndex].fullPath,
		})
		pairedSource[sourceIndex] = struct{}{}
		pairedCandidate[candidateIndex] = struct{}{}
	}

	for key, sourceIndexes := range sourceByBasename {
		candidateIndexes := candidateByBasename[key]
		if len(sourceIndexes) != 1 || len(candidateIndexes) != 1 {
			continue
		}
		sourceIndex := sourceIndexes[0]
		candidateIndex := candidateIndexes[0]
		if _, ok := pairedSource[sourceIndex]; ok {
			continue
		}
		if _, ok := pairedCandidate[candidateIndex]; ok {
			continue
		}
		if sameLocalFile(source[sourceIndex], candidate[candidateIndex]) {
			continue
		}
		pairs = append(pairs, localFilePair{
			sourcePath:    source[sourceIndex].fullPath,
			candidatePath: candidate[candidateIndex].fullPath,
		})
	}
	return pairs
}

func collectLocalTorrentFiles(ctx context.Context, backend fsops.Backend, instance *models.Instance, savePath string, files qbt.TorrentFiles) []localTorrentFile {
	localFiles := make([]localTorrentFile, 0, len(files))
	// Unresolvable names are already recorded by the FileID pass over both torrents
	// in localLinkedMatchType, which runs before any pairing.
	_ = forEachLocalTorrentFile(ctx, backend, savePath, files, func(file qbt.TorrentFile, fullPath string, info *fsops.LstatInfo) bool {
		if file.Size == 0 {
			return true
		}
		normalizedPath := normalizeTorrentRelativePath(file.Name)
		fileID, hasFileID := fsops.FileKeyOf(info.FileID, instance)
		localFiles = append(localFiles, localTorrentFile{
			file:           file,
			fullPath:       fullPath,
			fileID:         fileID,
			hasFileID:      hasFileID,
			normalizedPath: normalizedPath,
			basename:       path.Base(normalizedPath),
		})
		return true
	})
	return localFiles
}

func sameLocalFile(source, candidate localTorrentFile) bool {
	return source.hasFileID && candidate.hasFileID && source.fileID == candidate.fileID
}

func indexLocalFiles(files []localTorrentFile, keyFn func(localTorrentFile) string) map[string][]int {
	index := make(map[string][]int, len(files))
	for i, file := range files {
		key := keyFn(file)
		index[key] = append(index[key], i)
	}
	return index
}

func localFileSizeKey(name string, size int64) string {
	return name + "|" + strconv.FormatInt(size, 10)
}

// normalizeTorrentRelativePath keys a name for pairing. Every name reaching it
// has already cleared resolveLocalTorrentFile, so backslashes were refused, not
// rewritten.
func normalizeTorrentRelativePath(name string) string {
	return strings.ToLower(path.Clean(name))
}

// forEachLocalTorrentFile resolves each torrent file under savePath and invokes fn
// until fn returns false. It returns the first name that cannot be mapped to a path
// under savePath at all: such a file can never yield link evidence, so callers with a
// localMatchContext must fail closed instead of reporting "not linked". It also
// returns the first Lstat failure other than a missing path, such as a permission
// error, which hides link evidence the same way. A missing or non-regular file is a
// silent skip, because partially downloaded torrents are normal. A non-absolute
// savePath returns nil.
func forEachLocalTorrentFile(
	ctx context.Context,
	backend fsops.Backend,
	savePath string,
	files qbt.TorrentFiles,
	fn func(file qbt.TorrentFile, fullPath string, info *fsops.LstatInfo) bool,
) error {
	d := backend.Paths()
	base := d.Clean(d.FromSlash(savePath))
	if !d.IsAbs(base) {
		return nil
	}

	// Resolve every name first, so a backend with a batch lstats them in one
	// round trip. A backend without one lstats inside the loop, so a callback
	// that stops early costs one lstat, not one per file.
	type resolved struct {
		file     qbt.TorrentFile
		fullPath string // empty when the name does not resolve under base
	}
	entries := make([]resolved, 0, len(files))
	paths := make([]string, 0, len(files))
	for _, file := range files {
		fullPath, ok := resolveLocalTorrentFile(d, base, file.Name)
		if !ok {
			entries = append(entries, resolved{file: file})
			continue
		}
		entries = append(entries, resolved{file: file, fullPath: fullPath})
		paths = append(paths, fullPath)
	}
	batch, batched := backend.(fsops.BatchStater)
	var infos []*fsops.LstatInfo
	var errs []error
	if batched {
		infos, errs = batch.LstatBatch(ctx, paths)
	}

	var firstErr error
	next := 0
	for _, entry := range entries {
		if entry.fullPath == "" {
			if firstErr == nil {
				firstErr = fmt.Errorf("torrent file name %q cannot be resolved under the save path", entry.file.Name)
			}
			continue
		}
		var info *fsops.LstatInfo
		var err error
		if batched {
			info, err = infos[next], errs[next]
		} else {
			info, err = backend.Lstat(ctx, entry.fullPath)
		}
		next++
		if err != nil {
			// ENOTDIR: a parent of the path is a file, so this file is missing too.
			if firstErr == nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
				firstErr = err
			}
			continue
		}
		if !info.Mode.IsRegular() {
			continue
		}
		if !fn(entry.file, entry.fullPath, info) {
			return firstErr
		}
	}
	return firstErr
}

func resolveLocalTorrentFile(d fsops.PathDialect, base, name string) (string, bool) {
	// Names carrying a literal backslash are rejected rather than rewritten:
	// backslash is a legal filename byte on Linux, and inventing a nested
	// path from it makes the file read as missing while it exists.
	if name == "" || strings.ContainsRune(name, '\\') ||
		strings.HasPrefix(name, "/") ||
		hasWindowsDrivePrefix(name) {
		return "", false
	}

	cleanName := path.Clean(name)
	if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
		return "", false
	}

	fullPath := d.Join(base, d.FromSlash(cleanName))
	rel, err := d.Rel(base, fullPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+d.Separator()) {
		return "", false
	}
	return fullPath, true
}

func hasWindowsDrivePrefix(name string) bool {
	return len(name) >= 2 &&
		((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) &&
		name[1] == ':'
}
