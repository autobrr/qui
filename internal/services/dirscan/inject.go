// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/fsutil"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/pathutil"
	"github.com/autobrr/qui/pkg/reflinktree"
)

const (
	injectModeHardlink = "hardlink"
	injectModeReflink  = "reflink"
	injectModeRegular  = "regular"

	qbitContentLayoutOriginal    = "Original"
	qbitContentLayoutNoSubfolder = "NoSubfolder"
)

// errSettleQueueFull means the settle module could not take the torrent, so
// nothing rechecks or resumes it automatically.
var errSettleQueueFull = errors.New("recheck and resume queue full, manual intervention required")

// errLeftLocalMode means a run that walked local paths would read them on another host.
var errLeftLocalMode = errors.New("instance does not have local filesystem access")

// errRemoteTargetChanged means the remote instance has another mode or server than the one the run walked.
var errRemoteTargetChanged = errors.New("remote instance changed since the scan read it")

// Injector handles downloading and injecting torrents into qBittorrent.
type Injector struct {
	jackettService            JackettDownloader
	aligner                   *crossseed.Aligner
	adder                     *crossseed.Adder
	torrentChecker            TorrentChecker
	trackerCustomizationStore trackerCustomizationProvider
	backendPool               *fsops.Pool
}

// JackettDownloader is the interface for downloading torrent files.
type JackettDownloader interface {
	DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error)
}

// TorrentChecker is the interface for checking if torrents exist in qBittorrent.
type TorrentChecker interface {
	HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error)
}

type trackerCustomizationProvider interface {
	List(ctx context.Context) ([]*models.TrackerCustomization, error)
}

// NewInjector creates a new injector.
func NewInjector(
	jackettService JackettDownloader,
	aligner *crossseed.Aligner,
	adder *crossseed.Adder,
	torrentChecker TorrentChecker,
	trackerCustomizationStore trackerCustomizationProvider,
	backendPool *fsops.Pool,
) *Injector {
	return &Injector{
		jackettService:            jackettService,
		aligner:                   aligner,
		adder:                     adder,
		torrentChecker:            torrentChecker,
		trackerCustomizationStore: trackerCustomizationStore,
		backendPool:               backendPool,
	}
}

func (i *Injector) TorrentExistsAny(ctx context.Context, instanceID int, hashes []string) (bool, error) {
	if i.torrentChecker == nil {
		return false, nil
	}
	if len(hashes) == 0 {
		return false, nil
	}
	_, exists, err := i.torrentChecker.HasTorrentByAnyHash(ctx, instanceID, hashes)
	if err != nil {
		return false, fmt.Errorf("check torrent exists: %w", err)
	}
	return exists, nil
}

// InjectRequest contains parameters for injecting a torrent.
type InjectRequest struct {
	// InstanceID is the target qBittorrent instance.
	InstanceID int

	// ScanInstance is the row the run walked the searchee through.
	ScanInstance *models.Instance

	// Check is the Pre-add check that cleared the torrent for InstanceID.
	Check crossseed.PreAddCheck

	// TorrentBytes contains the .torrent file contents.
	TorrentBytes []byte

	// ParsedTorrent is the parsed torrent metadata for TorrentBytes.
	ParsedTorrent *ParsedTorrent

	// Searchee that was matched.
	Searchee *Searchee

	// MatchResult is the file-level match for this searchee and ParsedTorrent.
	MatchResult *MatchResult

	// SearchResult that matched the searchee.
	SearchResult *jackett.SearchResult

	// SavePath is the path where qBittorrent should save the torrent.
	// This should point to the parent directory of the searchee.
	SavePath string

	// QbitPathPrefix is an optional path prefix to apply for container path mapping.
	// If set, replaces the searchee's path prefix for qBittorrent injection.
	QbitPathPrefix string

	// Category to assign to the torrent.
	Category string

	// Tags to assign to the torrent.
	Tags []string

	// StartPaused adds the torrent in paused state.
	StartPaused bool

	// DownloadMissingFiles allows partial link tree injections to proceed.
	// When true, the torrent is added paused, rechecked, and optionally resumed.
	DownloadMissingFiles bool
}

// InjectResult contains the result of an injection attempt.
type InjectResult struct {
	// Success is true if the torrent was added successfully.
	Success bool

	// Class is the Result class of the add. It is failed when Inject stops
	// before the Adder.
	Class crossseed.ResultClass

	// SettleQueueFull is set when the added torrent stays paused because the
	// Settle queue was full. Class is then added.
	SettleQueueFull bool

	// Mode describes how the torrent was added: hardlink, reflink, or regular.
	Mode string

	// SavePath is the save path used when adding the torrent.
	SavePath string

	// Error message if injection failed.
	ErrorMessage string
}

// Inject downloads a torrent and injects it into qBittorrent.
func (i *Injector) Inject(ctx context.Context, req *InjectRequest) (*InjectResult, error) {
	result := &InjectResult{Class: crossseed.ClassFailed}

	if err := i.validateInjectRequest(req); err != nil {
		return result, err
	}
	if i.adder == nil {
		return result, errors.New("torrent adder is nil")
	}

	// Every read below takes this one backend, whose row resolveTarget compared
	// with the run's: the searchee's paths only mean something on that server.
	backend, instance, err := i.resolveTarget(ctx, req)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result, err
	}

	savePath, addMode, linkCreated, err := i.prepareInjection(ctx, instance, backend, req)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result, err
	}

	result.Mode = addMode
	result.SavePath = savePath

	hasUnmatchedFiles := len(req.MatchResult.UnmatchedTorrentFiles) > 0
	linkTree := isLinkTreeMode(addMode)

	// In regular (reuse) mode the torrent keeps its own folder/file names (minus the root for
	// stripRoot plans, added with NoSubfolder). When those differ from the on-disk paths we
	// matched, qBittorrent reports "Missing Files" until they are renamed to match. Scoped to
	// full matches: partial matches are added unpaused so
	// they can download the missing files, which is incompatible with the pause-rename-recheck
	// dance here. Link-tree modes build the on-disk layout to match the torrent, so they never
	// need this either.
	regularFullMatch := addMode == injectModeRegular && !hasUnmatchedFiles
	var alignPlan alignmentPlan
	if regularFullMatch {
		isDir, err := searcheeIsDir(ctx, backend, req.Searchee.Path)
		if err != nil {
			result.ErrorMessage = err.Error()
			return result, err
		}
		alignPlan = buildAlignmentPlan(backend.Paths(), req, isDir)
	}
	alignmentNeeded := regularFullMatch && alignPlan.needed()

	// Reject partial link tree injections when downloading missing files is disabled.
	if linkTree && hasUnmatchedFiles && !req.DownloadMissingFiles {
		i.rollbackLinkTree(ctx, linkCreated, savePath, backend)
		return result, fmt.Errorf("partial match has %d missing files; enable 'Download missing files' to allow",
			len(req.MatchResult.UnmatchedTorrentFiles))
	}

	// Keep qBittorrent's on-disk layout aligned with the existing files/hardlink tree, even if the
	// instance default content layout is "Create subfolder".
	placement := crossseed.AddPlacement{
		SavePath:      savePath,
		Category:      req.Category,
		Tags:          req.Tags,
		ContentLayout: qbitContentLayoutOriginal,
	}
	// A foldered torrent matched to a loose on-disk file has no folder to rename the root to;
	// NoSubfolder makes qBittorrent strip the root from every stored path so the (rootless-style)
	// plan lines up with the file where it actually lives.
	if regularFullMatch && alignPlan.stripRoot {
		placement.ContentLayout = qbitContentLayoutNoSubfolder
	}

	var afterAdd func(context.Context, []string) (string, bool)
	if alignmentNeeded {
		afterAdd = func(ctx context.Context, _ []string) (string, bool) {
			return i.align(ctx, req, alignPlan)
		}
	}

	added := i.adder.Add(ctx, crossseed.AddRequest{
		Check:   req.Check,
		Torrent: req.TorrentBytes,
		Facts: crossseed.AddFacts{
			LinkTree:       linkTree,
			PendingFiles:   hasUnmatchedFiles,
			Align:          alignmentNeeded,
			DiscMarker:     discMarker(req.ParsedTorrent),
			LinkedPaths:    linkedTorrentPaths(req, addMode),
			SkipAutoResume: req.StartPaused,
			StartRunning:   !req.StartPaused,
			DirScan:        true,
		},
		Placement: placement,
		AfterAdd:  afterAdd,
	})
	result.Class = added.Class()

	switch {
	case added.Outcome == crossseed.AddRefused:
		i.rollbackLinkTree(ctx, linkCreated, savePath, backend)
		result.ErrorMessage = fmt.Sprintf("failed to add torrent: %v", added.Err)
		return result, fmt.Errorf("add torrent: %w", added.Err)
	case added.Outcome == crossseed.AddLeftPaused:
		// If the paths could not be aligned to the on-disk files, report failure so the run
		// surfaces it instead of silently stranding a paused, mismatched torrent that will
		// never recheck to 100%.
		result.ErrorMessage = "alignment failed; torrent added paused for inspection"
	case added.Settled.QueueFull && added.Paused:
		// Nothing rechecks or resumes the paused torrent. A running add downloads without Settle.
		result.SettleQueueFull = true
		result.ErrorMessage = "torrent added paused: " + errSettleQueueFull.Error()
	default:
		result.Success = true
	}
	return result, nil
}

func isLinkTreeMode(mode string) bool {
	return mode == injectModeHardlink || mode == injectModeReflink
}

// linkedTorrentPaths returns the torrent paths of a hardlink add's matched files.
// A reflink clone is copy-on-write, so a reflink add returns nil and skips the
// linked-file check (ADR 0004).
func linkedTorrentPaths(req *InjectRequest, addMode string) map[string]struct{} {
	if addMode != injectModeHardlink {
		return nil
	}
	linked := make(map[string]struct{}, len(req.MatchResult.MatchedFiles))
	for _, pair := range req.MatchResult.MatchedFiles {
		linked[pair.TorrentFile.Path] = struct{}{}
	}
	return linked
}

func (i *Injector) validateInjectRequest(req *InjectRequest) error {
	if req == nil {
		return errors.New("inject request is nil")
	}
	if req.ScanInstance == nil {
		return errors.New("inject request missing scan instance")
	}
	if req.ParsedTorrent == nil || len(req.TorrentBytes) == 0 {
		return errors.New("inject request missing torrent bytes or parsed torrent")
	}
	if req.Searchee == nil || req.Searchee.Path == "" {
		return errors.New("inject request missing searchee")
	}
	if req.MatchResult == nil || len(req.MatchResult.MatchedFiles) == 0 {
		return errors.New("inject request missing valid match result")
	}
	return nil
}

// resolveTarget is the backend every read and write of one injection uses, and
// its row. An instance that left the mode or server the run walked refuses this
// add, and the run goes on to its next match.
func (i *Injector) resolveTarget(ctx context.Context, req *InjectRequest) (fsops.Backend, *models.Instance, error) {
	if i.backendPool == nil {
		return nil, nil, errors.New("filesystem backend pool not configured")
	}
	backend, instance, err := i.backendPool.Require(ctx, req.InstanceID, models.CapabilityRead)
	if err != nil && !errors.Is(err, fsops.ErrNotCapable) {
		return nil, nil, fmt.Errorf("get filesystem backend: %w", err)
	}
	walked := fsops.WalkedEndpoint(req.ScanInstance)
	if err == nil && fsops.WalkedEndpoint(instance) == walked {
		return backend, instance, nil
	}
	if walked != "" {
		return nil, nil, fmt.Errorf("get filesystem backend: instance %d: %w", req.InstanceID, errRemoteTargetChanged)
	}
	// ErrNotCapable only comes from an instance with no filesystem access.
	mode := models.FilesystemModeNone
	if err == nil {
		mode = models.FilesystemAccessMode(instance)
	}
	return nil, nil, fmt.Errorf("get filesystem backend: instance %d is in %s filesystem mode: %w", req.InstanceID, mode, errLeftLocalMode)
}

func (i *Injector) prepareInjection(
	ctx context.Context,
	instance *models.Instance,
	backend fsops.Backend,
	req *InjectRequest,
) (savePath, mode string, linkCreated *fsops.TreeCreateResult, err error) {
	if instance == nil {
		return "", "", nil, errors.New("instance is nil")
	}

	if !instance.UseReflinks && !instance.UseHardlinks {
		savePath, err = i.calculateSavePath(ctx, backend, req)
		return savePath, injectModeRegular, nil, err
	}

	plan, linkMode, created, linkErr := i.materializeLinkTree(ctx, instance, backend, req)
	if linkErr == nil {
		if plan == nil || plan.RootDir == "" {
			return "", "", nil, errors.New("link-tree plan missing root dir")
		}
		return plan.RootDir, linkMode, created, nil
	}

	// A lost connection says nothing about links on that host, so it isn't a
	// reason to leave link mode.
	if !instance.FallbackToRegularMode || errors.Is(linkErr, fsops.ErrConnectionLost) {
		return "", "", nil, linkErr
	}

	i.logLinkTreeFallback(instance, linkErr)
	savePath, err = i.calculateSavePath(ctx, backend, req)
	return savePath, injectModeRegular, nil, err
}

func (i *Injector) logLinkTreeFallback(instance *models.Instance, err error) {
	linkMode := injectModeHardlink
	if instance.UseReflinks {
		linkMode = injectModeReflink
	}

	fallbackReason := "link-tree creation failed"
	if errors.Is(err, syscall.EXDEV) {
		fallbackReason = "invalid cross-device link"
	}

	log.Warn().
		Err(err).
		Int("instanceID", instance.ID).
		Str("instanceName", instance.Name).
		Str("linkMode", linkMode).
		Str("reason", fallbackReason).
		Msg("dirscan: falling back to regular mode")
}

// rollbackLinkTree removes what the link-tree creator recorded in the result,
// never the whole plan: target paths can be shared with an earlier successful
// injection for the same release (discussion #2282). The result lists the
// folder that crossseed.LinkIntoNewFolder reserved. The root dir can be the
// base dir itself, so it is not removed here.
func (i *Injector) rollbackLinkTree(ctx context.Context, created *fsops.TreeCreateResult, rootDir string, backend fsops.Backend) {
	if created == nil || rootDir == "" || backend == nil {
		return
	}

	// Rollback must run even when the injection failed because the run was
	// cancelled — otherwise the partial link tree survives on disk.
	ctx = context.WithoutCancel(ctx)
	if err := backend.RemoveTree(ctx, created); err != nil {
		log.Warn().Err(err).Str("rootDir", rootDir).Msg("dirscan: failed to rollback link tree")
	}
}

// calculateSavePath determines the save path for the torrent.
func (i *Injector) calculateSavePath(ctx context.Context, backend fsops.Backend, req *InjectRequest) (string, error) {
	// Start with the provided save path or derive from searchee
	savePath := req.SavePath
	if savePath == "" {
		// Default: use the parent directory of the searchee path.
		// This avoids double-nesting when the incoming torrent already has a root folder.
		savePath = backend.Paths().Dir(req.Searchee.Path)

		// Special case: for directory searchees, if the incoming torrent is rootless (no common root folder),
		// use the searchee directory directly so single-file/rootless torrents land inside that folder.
		useSearchee, err := shouldUseSearcheeDirectory(ctx, backend, req.Searchee.Path, req.ParsedTorrent)
		if err != nil {
			return "", err
		}
		if useSearchee {
			savePath = req.Searchee.Path
		}
	}

	// Apply path prefix mapping for containers
	if req.QbitPathPrefix != "" {
		savePath = applyPathMapping(savePath, backend.Paths().Dir(req.Searchee.Path), req.QbitPathPrefix)
	}

	return savePath, nil
}

func shouldUseSearcheeDirectory(ctx context.Context, backend fsops.Backend, searcheePath string, parsed *ParsedTorrent) (bool, error) {
	if searcheePath == "" || parsed == nil {
		return false, nil
	}

	candidateFiles := make([]hardlinktree.TorrentFile, 0, len(parsed.Files))
	for _, f := range parsed.Files {
		candidateFiles = append(candidateFiles, hardlinktree.TorrentFile{Path: f.Path, Size: f.Size})
	}
	if hardlinktree.HasCommonRootFolder(candidateFiles) {
		return false, nil
	}
	return searcheeIsDir(ctx, backend, searcheePath)
}

// applyPathMapping replaces the original path prefix with the qBittorrent path prefix.
// Example:
//
//	original: /data/usenet/completed/Movie.Name/
//	searcheePath: /data/usenet/completed
//	qbitPrefix: /downloads/completed
//	result: /downloads/completed/Movie.Name/
func applyPathMapping(savePath, searcheePath, qbitPrefix string) string {
	// If savePath starts with searcheePath, replace that portion with qbitPrefix
	if suffix, found := strings.CutPrefix(savePath, searcheePath); found {
		return qbitPrefix + suffix
	}
	// If no match, just use the qbitPrefix as-is
	return qbitPrefix
}

// discMarker returns the disc layout folder (BDMV, VIDEO_TS) of the torrent, or empty.
func discMarker(parsed *ParsedTorrent) string {
	files := make(qbt.TorrentFiles, 0, len(parsed.Files))
	for _, f := range parsed.Files {
		files = append(files, qbt.TorrentFile{Name: f.Path, Size: f.Size})
	}
	return crossseed.PolicyForSourceFiles(files).DiscMarker
}

func (i *Injector) materializeLinkTree(ctx context.Context, instance *models.Instance, backend fsops.Backend, req *InjectRequest) (*hardlinktree.TreePlan, string, *fsops.TreeCreateResult, error) {
	if err := validateLinkTreeInstance(instance); err != nil {
		return nil, "", nil, err
	}
	if req == nil || req.ParsedTorrent == nil || req.MatchResult == nil {
		return nil, "", nil, errors.New("link-tree request is missing required data")
	}

	incomingFiles := buildLinkTreeIncomingFiles(req.ParsedTorrent)

	linkableFiles, existingFiles, err := buildLinkTreeMatchedFiles(req.MatchResult)
	if err != nil {
		return nil, "", nil, err
	}

	incomingTrackerDomain := crossseed.ParseTorrentAnnounceDomain(req.TorrentBytes)
	trackerDisplayName := i.resolveTrackerDisplayName(ctx, incomingTrackerDomain, indexerName(req.SearchResult))

	var (
		plan    *hardlinktree.TreePlan
		mode    string
		created *fsops.TreeCreateResult
	)
	err = crossseed.LinkIntoMatchingBaseDir(ctx, instance.HardlinkBaseDir, existingFiles[0].AbsPath, backend, func(baseDir string) error {
		if err := backend.MkdirAll(ctx, baseDir, fsutil.LinkTreeBaseDirMode); err != nil {
			return fmt.Errorf("create hardlink base dir: %w", err)
		}

		presetDir := buildLinkPresetDir(backend.Paths(), baseDir, instance, trackerDisplayName)
		var err error
		created, err = crossseed.LinkIntoNewFolder(ctx, backend, presetDir, req.ParsedTorrent.InfoHash, req.ParsedTorrent.Name, incomingFiles, func(destDir string) (*fsops.TreeCreateResult, error) {
			var err error
			plan, err = hardlinktree.BuildPlan(backend.Paths(), linkableFiles, existingFiles, hardlinktree.LayoutOriginal, req.ParsedTorrent.Name, destDir)
			if err != nil {
				// Debug: dump file data so we can diagnose BuildPlan mismatches.
				for idx, lf := range linkableFiles {
					log.Debug().
						Int("idx", idx).
						Str("path", lf.Path).
						Int64("size", lf.Size).
						Msg("dirscan: linkable file (candidate)")
				}
				for idx, ef := range existingFiles {
					log.Debug().
						Int("idx", idx).
						Str("absPath", ef.AbsPath).
						Str("relPath", ef.RelPath).
						Int64("size", ef.Size).
						Msg("dirscan: existing file")
				}
				log.Warn().
					Err(err).
					Int("instanceID", instance.ID).
					Str("instanceName", instance.Name).
					Str("torrentName", req.ParsedTorrent.Name).
					Msg("dirscan: failed to build link plan")
				return nil, humanizeLinkPlanError(err)
			}

			var treeCreated *fsops.TreeCreateResult
			mode, treeCreated, err = i.createLinkTree(ctx, instance, baseDir, existingFiles, plan, backend)
			return treeCreated, err
		})
		return err
	})
	if errors.Is(err, crossseed.ErrNoMatchingBaseDir) {
		return nil, "", nil, fmt.Errorf("select hardlink base dir: %w", err)
	}
	if err != nil {
		return nil, "", nil, err
	}

	return plan, mode, created, nil
}

func humanizeLinkPlanError(err error) error {
	if err == nil {
		return nil
	}

	const prefix = "couldn't prepare linked files for this release"

	var linkErr *hardlinktree.LinkPlanError
	if !errors.As(err, &linkErr) {
		return errors.New(prefix)
	}

	switch {
	case errors.Is(err, hardlinktree.ErrNoMatchingFile):
		file := linkErr.File
		return fmt.Errorf("%s: no matching local source file was found for a required release file (%s). The local file may be missing, renamed, or a different size", prefix, file)
	case errors.Is(err, hardlinktree.ErrNoAvailableFile):
		file := linkErr.File
		return fmt.Errorf("%s: no usable local source file remained for (%s)", prefix, file)
	case errors.Is(err, hardlinktree.ErrCouldNotMatch):
		file := linkErr.File
		return fmt.Errorf("%s: couldn't map a required release file to a local source file (%s)", prefix, file)
	default:
		return errors.New(prefix)
	}
}

func (i *Injector) resolveTrackerDisplayName(ctx context.Context, incomingTrackerDomain, indexerName string) string {
	var customizations []*models.TrackerCustomization
	if i.trackerCustomizationStore != nil {
		if customs, err := i.trackerCustomizationStore.List(ctx); err == nil {
			customizations = customs
		}
	}
	return models.ResolveTrackerDisplayName(incomingTrackerDomain, indexerName, customizations)
}

func validateLinkTreeInstance(instance *models.Instance) error {
	if instance == nil {
		return errors.New("instance is nil")
	}
	if !models.FilesystemCapabilitiesOf(instance).Write {
		return errors.New("instance does not have filesystem write access")
	}
	if instance.HardlinkBaseDir == "" {
		return errors.New("hardlink base directory is not configured")
	}
	if !instance.UseReflinks && !instance.UseHardlinks {
		return errors.New("no link mode enabled")
	}
	return nil
}

func indexerName(result *jackett.SearchResult) string {
	if result == nil {
		return ""
	}
	return result.Indexer
}

func buildLinkTreeIncomingFiles(parsed *ParsedTorrent) []hardlinktree.TorrentFile {
	files := make([]hardlinktree.TorrentFile, 0, len(parsed.Files))
	for _, f := range parsed.Files {
		files = append(files, hardlinktree.TorrentFile{Path: f.Path, Size: f.Size})
	}
	return files
}

func buildLinkTreeMatchedFiles(match *MatchResult) ([]hardlinktree.TorrentFile, []hardlinktree.ExistingFile, error) {
	if match == nil || len(match.MatchedFiles) == 0 {
		return nil, nil, errors.New("no matched files available for link-tree creation")
	}

	linkableFiles := make([]hardlinktree.TorrentFile, 0, len(match.MatchedFiles))
	seenLinkable := make(map[string]struct{}, len(match.MatchedFiles))

	existingFiles := make([]hardlinktree.ExistingFile, 0, len(match.MatchedFiles))
	for _, pair := range match.MatchedFiles {
		key := pair.TorrentFile.Path + "\x00" + strconv.FormatInt(pair.TorrentFile.Size, 10)
		if _, ok := seenLinkable[key]; !ok {
			seenLinkable[key] = struct{}{}
			linkableFiles = append(linkableFiles, hardlinktree.TorrentFile{Path: pair.TorrentFile.Path, Size: pair.TorrentFile.Size})
		}

		existingFiles = append(existingFiles, hardlinktree.ExistingFile{
			AbsPath: pair.SearcheeFile.Path,
			RelPath: pair.TorrentFile.Path,
			Size:    pair.TorrentFile.Size,
		})
	}

	if len(linkableFiles) == 0 || len(existingFiles) == 0 {
		return nil, nil, errors.New("no matched files available for link-tree creation")
	}

	return linkableFiles, existingFiles, nil
}

func (i *Injector) createLinkTree(ctx context.Context, instance *models.Instance, selectedBaseDir string, existingFiles []hardlinktree.ExistingFile, plan *hardlinktree.TreePlan, backend fsops.Backend) (string, *fsops.TreeCreateResult, error) {
	if instance.UseReflinks {
		supported, reason, err := backend.SupportsReflink(ctx, selectedBaseDir)
		if err != nil {
			return "", nil, fmt.Errorf("check reflink support: %w", err)
		}
		if !supported {
			return "", nil, fmt.Errorf("%w: %s", reflinktree.ErrReflinkUnsupported, reason)
		}
		created, err := backend.ReflinkTree(ctx, plan)
		if err != nil {
			return "", nil, fmt.Errorf("create reflink tree: %w", err)
		}
		return injectModeReflink, created, nil
	}

	if instance.UseHardlinks {
		sameFS, err := backend.SameFilesystem(ctx, existingFiles[0].AbsPath, selectedBaseDir)
		if err != nil {
			return "", nil, fmt.Errorf("verify same filesystem: %w", err)
		}
		if !sameFS {
			return "", nil, fmt.Errorf(
				"hardlink source (%s) and destination (%s) are on different filesystems",
				existingFiles[0].AbsPath,
				selectedBaseDir,
			)
		}

		created, err := backend.HardlinkTree(ctx, plan)
		if err != nil {
			if errors.Is(err, syscall.EXDEV) {
				return "", nil, fmt.Errorf(
					"create hardlink tree: %w (hardlinks cannot cross filesystems; put your scanned directory and hardlink base dir on the same mount, or enable reflinks if supported)",
					err,
				)
			}
			return "", nil, fmt.Errorf("create hardlink tree: %w", err)
		}
		return injectModeHardlink, created, nil
	}

	return "", nil, errors.New("no link mode enabled")
}

// buildLinkPresetDir applies the instance's hardlink directory preset to
// baseDir. crossseed.LinkIntoNewFolder picks the folder for the tree in it.
func buildLinkPresetDir(d fsops.PathDialect, baseDir string, instance *models.Instance, trackerDisplayName string) string {
	switch instance.HardlinkDirPreset {
	case "by-tracker":
		display := trackerDisplayName
		if display == "" {
			display = "Unknown"
		}
		return d.Join(baseDir, pathutil.SanitizePathSegment(display))
	case "by-instance":
		return d.Join(baseDir, pathutil.SanitizePathSegment(instance.Name))
	default: // "flat" or unknown
		return baseDir
	}
}
