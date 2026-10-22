// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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

	qbitBoolTrue  = "true"
	qbitBoolFalse = "false"

	qbitContentLayoutOriginal    = "Original"
	qbitContentLayoutNoSubfolder = "NoSubfolder"
)

// errSettleQueueFull means the settle module could not take the torrent, so
// nothing rechecks or resumes it automatically.
var errSettleQueueFull = errors.New("recheck and resume queue full, manual intervention required")

// Injector handles downloading and injecting torrents into qBittorrent.
type Injector struct {
	jackettService            JackettDownloader
	syncManager               TorrentManager
	settler                   *crossseed.Settler
	torrentChecker            TorrentChecker
	instanceStore             InstanceProvider
	trackerCustomizationStore trackerCustomizationProvider
	backendPool               *fsops.Pool
}

// JackettDownloader is the interface for downloading torrent files.
type JackettDownloader interface {
	DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error)
}

// TorrentAdder adds torrents to qBittorrent.
type TorrentAdder interface {
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) (*qbt.TorrentAddResponse, error)
}

// TorrentPathAligner renames an injected torrent's internal folder/file names to match the
// on-disk files it matched, and reads the current names back to confirm the renames landed.
type TorrentPathAligner interface {
	RenameTorrentFile(ctx context.Context, instanceID int, hash, oldPath, newPath string) error
	RenameTorrentFolder(ctx context.Context, instanceID int, hash, oldPath, newPath string) error
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
}

// TorrentManager is the combined capability the Injector depends on: adding torrents and aligning
// their content paths. Callers can depend on the narrower TorrentAdder/TorrentPathAligner instead.
type TorrentManager interface {
	TorrentAdder
	TorrentPathAligner
}

// TorrentChecker is the interface for checking if torrents exist in qBittorrent.
type TorrentChecker interface {
	HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error)
}

type InstanceProvider interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
}

type trackerCustomizationProvider interface {
	List(ctx context.Context) ([]*models.TrackerCustomization, error)
}

// NewInjector creates a new injector.
func NewInjector(
	jackettService JackettDownloader,
	syncManager TorrentManager,
	settler *crossseed.Settler,
	torrentChecker TorrentChecker,
	instanceStore InstanceProvider,
	trackerCustomizationStore trackerCustomizationProvider,
	backendPool *fsops.Pool,
) *Injector {
	return &Injector{
		jackettService:            jackettService,
		syncManager:               syncManager,
		settler:                   settler,
		torrentChecker:            torrentChecker,
		instanceStore:             instanceStore,
		trackerCustomizationStore: trackerCustomizationStore,
		backendPool:               backendPool,
	}
}

// TorrentExists checks if a torrent with the given hash already exists in qBittorrent.
func (i *Injector) TorrentExists(ctx context.Context, instanceID int, hash string) (bool, error) {
	if i.torrentChecker == nil {
		return false, nil
	}
	_, exists, err := i.torrentChecker.HasTorrentByAnyHash(ctx, instanceID, []string{hash})
	if err != nil {
		return false, fmt.Errorf("check torrent exists: %w", err)
	}
	return exists, nil
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

	// TorrentHash is the hash of the added torrent.
	TorrentHash string

	// Mode describes how the torrent was added: hardlink, reflink, or regular.
	Mode string

	// SavePath is the save path used when adding the torrent.
	SavePath string

	// Error message if injection failed.
	ErrorMessage string
}

// Inject downloads a torrent and injects it into qBittorrent.
func (i *Injector) Inject(ctx context.Context, req *InjectRequest) (*InjectResult, error) {
	result := &InjectResult{}

	if err := i.validateInjectRequest(req); err != nil {
		return result, err
	}
	if i.instanceStore == nil {
		return result, errors.New("instance store is nil")
	}
	if i.syncManager == nil {
		return result, errors.New("torrent adder is nil")
	}

	instance, err := i.instanceStore.Get(ctx, req.InstanceID)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("failed to load instance: %v", err)
		return result, fmt.Errorf("get instance: %w", err)
	}

	// Every read below takes this one backend. A scan admits the instance on
	// local access minutes before it injects, and a fresh resolve per read
	// could land on the SSH host if local access was turned off since.
	backend, err := i.localBackend(ctx, instance.ID)
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

	addPolicy := addPolicyForInjectRequest(req)
	hasUnmatchedFiles := len(req.MatchResult.UnmatchedTorrentFiles) > 0
	regularAddNeedsRecheck := hasUnmatchedFiles || addPolicy.ForcePaused
	partialLinkTree := isLinkTreeMode(addMode) && hasUnmatchedFiles

	// In regular (reuse) mode the torrent keeps its own folder/file names (minus the root for
	// stripRoot plans, added with NoSubfolder). When those differ from the on-disk paths we
	// matched, qBittorrent reports "Missing Files" until they are renamed to match. Scoped to
	// full matches: partial matches are added unpaused so
	// they can download the missing files, which is incompatible with the pause-rename-recheck
	// dance here. Link-tree modes build the on-disk layout to match the torrent, so they never
	// need this either.
	alignPlan := buildAlignmentPlan(req, searcheePathIsDir(ctx, backend, req.Searchee.Path))
	regularFullMatch := addMode == injectModeRegular && !hasUnmatchedFiles
	alignmentNeeded := regularFullMatch && alignPlan.needed()

	// Reject partial link tree injections when downloading missing files is disabled.
	if partialLinkTree && !req.DownloadMissingFiles {
		i.rollbackLinkTree(ctx, linkCreated, savePath, backend)
		return result, fmt.Errorf("partial match has %d missing files; enable 'Download missing files' to allow",
			len(req.MatchResult.UnmatchedTorrentFiles))
	}

	options := i.buildAddOptions(req, savePath)
	// A foldered torrent matched to a loose on-disk file has no folder to rename the root to;
	// NoSubfolder makes qBittorrent strip the root from every stored path so the (rootless-style)
	// plan lines up with the file where it actually lives.
	if regularFullMatch && alignPlan.stripRoot {
		options["contentLayout"] = qbitContentLayoutNoSubfolder
	}
	// Skip the on-add hash check for full matches (the data is already verified by the on-disk
	// files we matched). Alignment adds MUST keep skip_checking too: qBittorrent blocks file/folder
	// rename operations while a torrent is being verified, so letting it check the pre-rename paths
	// would stall the renames. The manual recheck after alignment does the real verification.
	if !hasUnmatchedFiles {
		options["skip_checking"] = qbitBoolTrue
	}

	// Force paused for partial link tree injections (recheck before qBit uses the incomplete
	// tree) and for alignment injections (rename the paths before qBit acts on the wrong ones).
	if partialLinkTree || alignmentNeeded {
		options["paused"] = qbitBoolTrue
		options["stopped"] = qbitBoolTrue
	}

	applyAddPolicy(options, addPolicy)

	// Add the torrent to qBittorrent
	if _, err := i.syncManager.AddTorrent(ctx, req.InstanceID, req.TorrentBytes, options); err != nil {
		i.rollbackLinkTree(ctx, linkCreated, savePath, backend)
		result.ErrorMessage = fmt.Sprintf("failed to add torrent: %v", err)
		return result, fmt.Errorf("add torrent: %w", err)
	}

	result.TorrentHash = req.ParsedTorrent.InfoHash

	switch {
	case partialLinkTree:
		if err := i.triggerRecheckForPartialLinkTree(req, addMode); err != nil {
			result.ErrorMessage = "torrent added paused: " + err.Error()
			return result, fmt.Errorf("partial link tree recheck: %w", err)
		}
	case alignmentNeeded:
		// The torrent was added force-paused. If the paths could not be aligned to the on-disk
		// files, report failure so the run surfaces it instead of silently stranding a paused,
		// mismatched torrent that will never recheck to 100%.
		if !i.alignAndRecheck(ctx, req, alignPlan) {
			result.ErrorMessage = "content path alignment failed; torrent added paused for inspection"
			return result, nil
		}
	default:
		// A full settle queue strands only an add that the policy force-paused (a disc
		// layout). Other regular adds run, so the settle module logs it and the injection continues.
		if err := i.triggerRecheckForPausedPartial(req, regularAddNeedsRecheck); err != nil && addPolicy.ForcePaused {
			result.ErrorMessage = "torrent added paused: " + err.Error()
			return result, nil
		}
	}

	result.Success = true
	return result, nil
}

func isLinkTreeMode(mode string) bool {
	return mode == injectModeHardlink || mode == injectModeReflink
}

// triggerRecheckForPartialLinkTree settles a partial link tree add. The torrent
// was added force-paused so qBittorrent does not use the incomplete tree before
// its recheck. Unless the user asked to start paused, it resumes after the
// recheck. Returns an error when the torrent would stay paused with no
// automatic recheck or resume.
func (i *Injector) triggerRecheckForPartialLinkTree(req *InjectRequest, addMode string) error {
	outcome := i.settler.Settle(context.Background(), crossseed.AddedTorrent{
		InstanceID:     req.InstanceID,
		Hashes:         []string{req.ParsedTorrent.InfoHash},
		LinkedPaths:    linkedTorrentPaths(req, addMode),
		Gate:           crossseed.GateAfterRecheck,
		SkipAutoResume: req.StartPaused,
	})
	if outcome.QueueFull {
		return errSettleQueueFull
	}
	return nil
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

// triggerRecheckForPausedPartial verifies regular-mode partial or policy-forced
// full-recheck matches after add. Unless the request asked to stay paused, the
// torrent resumes once nothing is left. Returns an error when the torrent would
// stay paused with no automatic recheck or resume, so callers that force-paused
// the torrent (alignment) can surface the failure instead of leaving it stranded.
func (i *Injector) triggerRecheckForPausedPartial(req *InjectRequest, needsRecheck bool) error {
	if !needsRecheck {
		return nil
	}
	outcome := i.settler.Settle(context.Background(), crossseed.AddedTorrent{
		InstanceID:     req.InstanceID,
		Hashes:         []string{req.ParsedTorrent.InfoHash},
		Gate:           crossseed.GateComplete,
		SkipAutoResume: req.StartPaused,
	})
	if outcome.QueueFull {
		return errSettleQueueFull
	}
	return nil
}

func (i *Injector) validateInjectRequest(req *InjectRequest) error {
	if req == nil {
		return errors.New("inject request is nil")
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

// localBackend is the backend every read and write of one injection uses.
func (i *Injector) localBackend(ctx context.Context, instanceID int) (fsops.Backend, error) {
	if i.backendPool == nil {
		return nil, errors.New("filesystem backend pool not configured")
	}
	backend, err := i.backendPool.LocalBackend(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get filesystem backend: %w", err)
	}
	return backend, nil
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
		return i.calculateSavePath(ctx, backend, req), injectModeRegular, nil, nil
	}

	plan, linkMode, created, linkErr := i.materializeLinkTree(ctx, instance, backend, req)
	if linkErr == nil {
		if plan == nil || plan.RootDir == "" {
			return "", "", nil, errors.New("link-tree plan missing root dir")
		}
		return plan.RootDir, linkMode, created, nil
	}

	if !instance.FallbackToRegularMode {
		return "", "", nil, linkErr
	}

	i.logLinkTreeFallback(instance, linkErr)
	return i.calculateSavePath(ctx, backend, req), injectModeRegular, nil, nil
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
func (i *Injector) calculateSavePath(ctx context.Context, backend fsops.Backend, req *InjectRequest) string {
	// Start with the provided save path or derive from searchee
	savePath := req.SavePath
	if savePath == "" {
		// Default: use the parent directory of the searchee path.
		// This avoids double-nesting when the incoming torrent already has a root folder.
		savePath = filepath.Dir(req.Searchee.Path)

		// Special case: for directory searchees, if the incoming torrent is rootless (no common root folder),
		// use the searchee directory directly so single-file/rootless torrents land inside that folder.
		if req.ParsedTorrent != nil && shouldUseSearcheeDirectory(ctx, backend, req.Searchee.Path, req.ParsedTorrent) {
			savePath = req.Searchee.Path
		}
	}

	// Apply path prefix mapping for containers
	if req.QbitPathPrefix != "" {
		savePath = applyPathMapping(savePath, filepath.Dir(req.Searchee.Path), req.QbitPathPrefix)
	}

	return savePath
}

func shouldUseSearcheeDirectory(ctx context.Context, backend fsops.Backend, searcheePath string, parsed *ParsedTorrent) bool {
	if searcheePath == "" || parsed == nil || backend == nil {
		return false
	}

	fi, err := backend.Stat(ctx, searcheePath)
	if err != nil || !fi.IsDir {
		return false
	}

	candidateFiles := make([]hardlinktree.TorrentFile, 0, len(parsed.Files))
	for _, f := range parsed.Files {
		candidateFiles = append(candidateFiles, hardlinktree.TorrentFile{Path: f.Path, Size: f.Size})
	}
	return !hardlinktree.HasCommonRootFolder(candidateFiles)
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

// buildAddOptions builds the options map for adding a torrent.
func (i *Injector) buildAddOptions(req *InjectRequest, savePath string) map[string]string {
	options := make(map[string]string)

	// Disable autoTMM to use our explicit save path
	options["autoTMM"] = qbitBoolFalse

	// Keep qBittorrent's on-disk layout aligned with the existing files/hardlink tree, even if the
	// instance default content layout is "Create subfolder".
	options["contentLayout"] = qbitContentLayoutOriginal
	// Backwards compatibility for older qBittorrent versions (<4.3.2).
	options["root_folder"] = qbitBoolFalse

	// Set the save path
	options["savepath"] = savePath

	// Set category if provided
	if req.Category != "" {
		options["category"] = req.Category
	}

	// Set tags if provided
	if len(req.Tags) > 0 {
		options["tags"] = strings.Join(req.Tags, ",")
	}

	// Set paused state
	if req.StartPaused {
		options["paused"] = qbitBoolTrue
		options["stopped"] = qbitBoolTrue
	}

	return options
}

func addPolicyForInjectRequest(req *InjectRequest) crossseed.AddPolicy {
	if req == nil || req.ParsedTorrent == nil {
		return crossseed.AddPolicy{}
	}

	files := make(qbt.TorrentFiles, 0, len(req.ParsedTorrent.Files))
	for _, f := range req.ParsedTorrent.Files {
		files = append(files, qbt.TorrentFile{
			Name: f.Path,
			Size: f.Size,
		})
	}

	return crossseed.PolicyForSourceFiles(files)
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
	// Dirscan builds its link destinations with host filepath and walks through
	// Pool.LocalBackend, so a remote instance stays out even with Write (#2942)
	// until #2919 moves it to the backend path dialect.
	if models.FilesystemAccessMode(instance) != models.FilesystemModeLocal {
		return errors.New("instance does not have local filesystem access enabled")
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

func applyAddPolicy(options map[string]string, policy crossseed.AddPolicy) {
	policy.ApplyToAddOptions(options)
}
