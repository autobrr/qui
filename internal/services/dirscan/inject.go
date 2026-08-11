// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/qui/internal/models"
	qbsync "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/fsutil"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/pathutil"
	"github.com/autobrr/qui/pkg/reflinktree"
	"github.com/rs/zerolog/log"
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

// Injector handles downloading and injecting torrents into qBittorrent.
type Injector struct {
	jackettService            JackettDownloader
	syncManager               TorrentManager
	torrentChecker            TorrentChecker
	instanceStore             InstanceProvider
	trackerCustomizationStore trackerCustomizationProvider
}

// JackettDownloader is the interface for downloading torrent files.
type JackettDownloader interface {
	DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error)
}

// TorrentAdder adds torrents and drives their lifecycle in qBittorrent.
type TorrentAdder interface {
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) (*qbt.TorrentAddResponse, error)
	BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error
	ResumeWhenComplete(instanceID int, hashes []string, opts qbsync.ResumeWhenCompleteOptions)
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
	torrentChecker TorrentChecker,
	instanceStore InstanceProvider,
	trackerCustomizationStore trackerCustomizationProvider,
) *Injector {
	return &Injector{
		jackettService:            jackettService,
		syncManager:               syncManager,
		torrentChecker:            torrentChecker,
		instanceStore:             instanceStore,
		trackerCustomizationStore: trackerCustomizationStore,
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

	savePath, addMode, linkCreated, err := i.prepareInjection(ctx, instance, req)
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
	alignPlan := buildAlignmentPlan(req, searcheePathIsDir(req.Searchee.Path))
	regularFullMatch := addMode == injectModeRegular && !hasUnmatchedFiles
	alignmentNeeded := regularFullMatch && alignPlan.needed()

	// Reject partial link tree injections when downloading missing files is disabled.
	if partialLinkTree && !req.DownloadMissingFiles {
		i.rollbackLinkTree(linkCreated, savePath)
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
		i.rollbackLinkTree(linkCreated, savePath)
		result.ErrorMessage = fmt.Sprintf("failed to add torrent: %v", err)
		return result, fmt.Errorf("add torrent: %w", err)
	}

	result.TorrentHash = req.ParsedTorrent.InfoHash

	switch {
	case partialLinkTree:
		if err := i.triggerRecheckForPartialLinkTree(req); err != nil {
			result.ErrorMessage = fmt.Sprintf("torrent added but recheck failed: %v", err)
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
		// Regular (non-aligned) adds are not force-paused, so a failed recheck is non-fatal here:
		// log and continue rather than failing the injection.
		_ = i.triggerRecheckForPausedPartial(req, regularAddNeedsRecheck)
	}

	result.Success = true
	return result, nil
}

func isLinkTreeMode(mode string) bool {
	return mode == injectModeHardlink || mode == injectModeReflink
}

func isCheckingState(state qbt.TorrentState) bool {
	switch state { //nolint:exhaustive // only checking states matter
	case qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingUp,
		qbt.TorrentStateCheckingResumeData, qbt.TorrentStateAllocating:
		return true
	}
	return false
}

// triggerRecheckForPartialLinkTree handles partial link tree injections:
// trigger a force recheck, then optionally resume the torrent once checking completes.
// The torrent was added paused (forced) so qBit doesn't try to use the incomplete link tree.
// Returns an error if the recheck cannot be scheduled, since the torrent would be stuck
// in a forced-paused state with no way to recover.
func (i *Injector) triggerRecheckForPartialLinkTree(req *InjectRequest) error {
	if i == nil || i.syncManager == nil || req == nil || req.ParsedTorrent == nil {
		return errors.New("missing injector components for recheck")
	}

	hash := req.ParsedTorrent.InfoHash
	ctx := context.Background()

	if err := i.syncManager.BulkAction(ctx, req.InstanceID, []string{hash}, "recheck"); err != nil {
		return fmt.Errorf("trigger recheck for partial link tree: %w", err)
	}

	// If the user wanted the torrent running, resume it after the recheck finishes.
	// If StartPaused=true, leave it paused (we just honor the user's setting).
	if !req.StartPaused {
		i.resumeAfterRecheck(req.InstanceID, hash)
	}
	return nil
}

// resumeAfterRecheck polls the torrent state in a background goroutine and
// resumes the torrent once it exits checking states. This is used for partial
// link tree injections where we temporarily forced the torrent paused for a
// safe recheck, but the user's StartPaused=false means they want it running.
func (i *Injector) resumeAfterRecheck(instanceID int, hash string) {
	if i.torrentChecker == nil || i.syncManager == nil {
		return
	}

	go func() {
		const (
			pollInterval = 5 * time.Second
			timeout      = 10 * time.Minute
		)

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		// Wait for the sync cache to show a checking state, proving qBit
		// actually started the recheck, then wait for it to leave that state.
		// Without this two-phase check the first poll can see a stale
		// stopped/paused state (cache not yet refreshed) and resume before
		// the recheck runs. qBit's StopCondition::FilesChecked would then
		// re-stop the torrent after checking, leaving it stuck.
		// Even after resume succeeds, keep polling until running state is
		// stable so a late files-checked stop can be retried.
		const (
			maxResumeAttempts = 3
			stablePolls       = 2
		)

		sawChecking := false
		recheckComplete := false
		awaitingResumeConfirmation := false
		resumeAttempts := 0
		readyPolls := 0
		resumeConfirmedPolls := 0
		for {
			select {
			case <-ctx.Done():
				log.Debug().
					Int("instanceID", instanceID).
					Str("hash", hash).
					Bool("sawChecking", sawChecking).
					Bool("recheckComplete", recheckComplete).
					Int("resumeAttempts", resumeAttempts).
					Msg("dirscan: resumeAfterRecheck timed out")
				return
			case <-ticker.C:
			}

			torrent, found, err := i.torrentChecker.HasTorrentByAnyHash(ctx, instanceID, []string{hash})
			if err != nil || !found || torrent == nil {
				continue
			}

			if isCheckingState(torrent.State) {
				sawChecking = true
				readyPolls = 0
				resumeConfirmedPolls = 0
				continue
			}

			if awaitingResumeConfirmation {
				if isResumeConfirmedState(torrent.State) {
					resumeConfirmedPolls++
					if resumeConfirmedPolls < stablePolls {
						continue
					}
					log.Info().
						Int("instanceID", instanceID).
						Str("hash", hash).
						Str("state", string(torrent.State)).
						Int("attempts", resumeAttempts).
						Msg("dirscan: confirmed torrent resumed after partial link tree recheck")
					return
				}

				resumeConfirmedPolls = 0
				if !isPausedOrStoppedState(torrent.State) {
					continue
				}
			}

			// Two ways to know the recheck finished:
			// 1. We observed a checking state and it transitioned out.
			// 2. We never caught the checking state (fast disk / small
			//    torrent) but Completed > 0 proves qBit verified data.
			//    A freshly added paused torrent has Completed == 0 until
			//    the recheck runs.
			if sawChecking || torrent.Completed > 0 {
				recheckComplete = true
			}
			if !recheckComplete {
				continue
			}
			readyPolls++
			if !sawChecking && readyPolls < stablePolls {
				continue
			}

			if resumeAttempts >= maxResumeAttempts {
				log.Warn().
					Int("instanceID", instanceID).
					Str("hash", hash).
					Str("state", string(torrent.State)).
					Int("attempts", resumeAttempts).
					Msg("dirscan: resume attempts after partial link tree recheck exhausted")
				return
			}

			resumeAttempts++
			if err := i.syncManager.BulkAction(ctx, instanceID, []string{hash}, "resume"); err != nil {
				log.Warn().
					Err(err).
					Int("instanceID", instanceID).
					Str("hash", hash).
					Int("attempt", resumeAttempts).
					Int("maxAttempts", maxResumeAttempts).
					Msg("dirscan: failed to resume torrent after recheck")
				continue
			}

			awaitingResumeConfirmation = true
			log.Info().
				Int("instanceID", instanceID).
				Str("hash", hash).
				Int("attempt", resumeAttempts).
				Msg("dirscan: resumed torrent after partial link tree recheck")
		}
	}()
}

func isResumeConfirmedState(state qbt.TorrentState) bool {
	switch state { //nolint:exhaustive // only running states confirm resume
	case qbt.TorrentStateUploading,
		qbt.TorrentStateStalledUp,
		qbt.TorrentStateQueuedUp,
		qbt.TorrentStateForcedUp,
		qbt.TorrentStateDownloading,
		qbt.TorrentStateStalledDl,
		qbt.TorrentStateQueuedDl,
		qbt.TorrentStateForcedDl,
		qbt.TorrentStateMetaDl:
		return true
	}
	return false
}

func isPausedOrStoppedState(state qbt.TorrentState) bool {
	switch state { //nolint:exhaustive // only stopped states need resume retries
	case qbt.TorrentStatePausedUp,
		qbt.TorrentStateStoppedUp,
		qbt.TorrentStatePausedDl,
		qbt.TorrentStateStoppedDl:
		return true
	}
	return false
}

// triggerRecheckForPausedPartial verifies regular-mode partial or policy-forced
// full-recheck matches after add, and only queues resume when the request did
// not ask to stay paused. Returns an error if the recheck could not be scheduled so
// callers that force-paused the torrent (alignment) can surface the failure instead
// of leaving it stranded.
func (i *Injector) triggerRecheckForPausedPartial(req *InjectRequest, needsRecheck bool) error {
	if i == nil || i.syncManager == nil || req == nil || req.ParsedTorrent == nil || req.MatchResult == nil {
		return nil
	}
	if !needsRecheck {
		return nil
	}

	hash := req.ParsedTorrent.InfoHash
	ctx := context.Background()
	if err := i.syncManager.BulkAction(ctx, req.InstanceID, []string{hash}, "recheck"); err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", req.InstanceID).
			Str("hash", hash).
			Msg("dirscan: failed to trigger recheck after add")
		return fmt.Errorf("trigger recheck after add: %w", err)
	}

	if !req.StartPaused {
		i.syncManager.ResumeWhenComplete(req.InstanceID, []string{hash}, qbsync.ResumeWhenCompleteOptions{
			Timeout: 60 * time.Minute,
		})
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

func (i *Injector) prepareInjection(
	ctx context.Context,
	instance *models.Instance,
	req *InjectRequest,
) (savePath, mode string, linkCreated *hardlinktree.Created, err error) {
	if instance == nil {
		return "", "", nil, errors.New("instance is nil")
	}

	if !instance.UseReflinks && !instance.UseHardlinks {
		return i.calculateSavePath(req), injectModeRegular, nil, nil
	}

	plan, linkMode, created, linkErr := i.materializeLinkTree(ctx, instance, req)
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
	return i.calculateSavePath(req), injectModeRegular, nil, nil
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

// rollbackLinkTree removes what the link-tree creator recorded in the handle,
// never the whole plan: target paths can be shared with an earlier successful
// injection for the same release (discussion #2282). The root dir is removed
// only when empty.
func (i *Injector) rollbackLinkTree(created *hardlinktree.Created, rootDir string) {
	if created == nil || rootDir == "" {
		return
	}

	if err := created.Rollback(); err != nil {
		log.Warn().Err(err).Str("rootDir", rootDir).Msg("dirscan: failed to rollback link tree")
	}
	_ = os.Remove(rootDir)
}

// calculateSavePath determines the save path for the torrent.
func (i *Injector) calculateSavePath(req *InjectRequest) string {
	// Start with the provided save path or derive from searchee
	savePath := req.SavePath
	if savePath == "" {
		// Default: use the parent directory of the searchee path.
		// This avoids double-nesting when the incoming torrent already has a root folder.
		savePath = filepath.Dir(req.Searchee.Path)

		// Special case: for directory searchees, if the incoming torrent is rootless (no common root folder),
		// use the searchee directory directly so single-file/rootless torrents land inside that folder.
		if req.ParsedTorrent != nil && shouldUseSearcheeDirectory(req.Searchee.Path, req.ParsedTorrent) {
			savePath = req.Searchee.Path
		}
	}

	// Apply path prefix mapping for containers
	if req.QbitPathPrefix != "" {
		savePath = applyPathMapping(savePath, filepath.Dir(req.Searchee.Path), req.QbitPathPrefix)
	}

	return savePath
}

func shouldUseSearcheeDirectory(searcheePath string, parsed *ParsedTorrent) bool {
	if searcheePath == "" || parsed == nil {
		return false
	}

	fi, err := os.Stat(searcheePath)
	if err != nil || !fi.IsDir() {
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

func (i *Injector) materializeLinkTree(ctx context.Context, instance *models.Instance, req *InjectRequest) (*hardlinktree.TreePlan, string, *hardlinktree.Created, error) {
	if err := validateLinkTreeInstance(instance); err != nil {
		return nil, "", nil, err
	}
	if req == nil || req.ParsedTorrent == nil || req.MatchResult == nil {
		return nil, "", nil, errors.New("link-tree request is missing required data")
	}

	incomingFiles := buildLinkTreeIncomingFiles(req.ParsedTorrent)
	needsIsolation := !hardlinktree.HasCommonRootFolder(incomingFiles)

	linkableFiles, existingFiles, err := buildLinkTreeMatchedFiles(req.MatchResult)
	if err != nil {
		return nil, "", nil, err
	}

	selectedBaseDir, err := crossseed.FindMatchingBaseDir(instance.HardlinkBaseDir, existingFiles[0].AbsPath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("select hardlink base dir: %w", err)
	}
	if err := os.MkdirAll(selectedBaseDir, fsutil.LinkTreeBaseDirMode); err != nil {
		return nil, "", nil, fmt.Errorf("create hardlink base dir: %w", err)
	}

	incomingTrackerDomain := crossseed.ParseTorrentAnnounceDomain(req.TorrentBytes)
	trackerDisplayName := i.resolveTrackerDisplayName(ctx, incomingTrackerDomain, indexerName(req.SearchResult))
	destDir := buildLinkDestDir(selectedBaseDir, instance, req.ParsedTorrent.InfoHash, req.ParsedTorrent.Name, needsIsolation, trackerDisplayName)

	plan, err := hardlinktree.BuildPlan(linkableFiles, existingFiles, hardlinktree.LayoutOriginal, req.ParsedTorrent.Name, destDir)
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
		return nil, "", nil, humanizeLinkPlanError(err)
	}

	mode, created, err := i.createLinkTree(instance, selectedBaseDir, existingFiles, plan)
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
	if !instance.HasLocalFilesystemAccess {
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

func (i *Injector) createLinkTree(instance *models.Instance, selectedBaseDir string, existingFiles []hardlinktree.ExistingFile, plan *hardlinktree.TreePlan) (string, *hardlinktree.Created, error) {
	if instance.UseReflinks {
		if supported, reason := reflinktree.SupportsReflink(selectedBaseDir); !supported {
			return "", nil, fmt.Errorf("%w: %s", reflinktree.ErrReflinkUnsupported, reason)
		}
		created, err := reflinktree.Create(plan)
		if err != nil {
			return "", nil, fmt.Errorf("create reflink tree: %w", err)
		}
		return injectModeReflink, created, nil
	}

	if instance.UseHardlinks {
		sameFS, err := fsutil.SameFilesystem(existingFiles[0].AbsPath, selectedBaseDir)
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

		created, err := hardlinktree.Create(plan)
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

func buildLinkDestDir(baseDir string, instance *models.Instance, torrentHash, torrentName string, needsIsolation bool, trackerDisplayName string) string {
	isolationFolder := ""
	if needsIsolation {
		isolationFolder = pathutil.IsolationFolderName(torrentHash, torrentName)
	}

	switch instance.HardlinkDirPreset {
	case "by-tracker":
		display := trackerDisplayName
		if display == "" {
			display = "Unknown"
		}
		if isolationFolder != "" {
			return filepath.Join(baseDir, pathutil.SanitizePathSegment(display), isolationFolder)
		}
		return filepath.Join(baseDir, pathutil.SanitizePathSegment(display))

	case "by-instance":
		if isolationFolder != "" {
			return filepath.Join(baseDir, pathutil.SanitizePathSegment(instance.Name), isolationFolder)
		}
		return filepath.Join(baseDir, pathutil.SanitizePathSegment(instance.Name))

	default: // "flat" or unknown
		return filepath.Join(baseDir, pathutil.IsolationFolderName(torrentHash, torrentName))
	}
}

func applyAddPolicy(options map[string]string, policy crossseed.AddPolicy) {
	policy.ApplyToAddOptions(options)
}
