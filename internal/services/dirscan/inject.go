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

	qbitContentLayoutOriginal = "Original"
)

// Injector handles downloading and injecting torrents into qBittorrent.
type Injector struct {
	jackettService            JackettDownloader
	syncManager               TorrentAdder
	torrentChecker            TorrentChecker
	instanceStore             InstanceProvider
	trackerCustomizationStore trackerCustomizationProvider
}

// JackettDownloader is the interface for downloading torrent files.
type JackettDownloader interface {
	DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error)
}

// TorrentAdder is the interface for adding torrents to qBittorrent.
type TorrentAdder interface {
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) error
	BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error
	ResumeWhenComplete(instanceID int, hashes []string, opts qbsync.ResumeWhenCompleteOptions)
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
	syncManager TorrentAdder,
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

	savePath, addMode, linkPlan, err := i.prepareInjection(ctx, instance, req)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result, err
	}

	result.Mode = addMode
	result.SavePath = savePath

	options := i.buildAddOptions(req, savePath)
	if len(req.MatchResult.UnmatchedTorrentFiles) == 0 {
		options["skip_checking"] = qbitBoolTrue
	}

	i.applyAddPolicy(options, req)

	// Add the torrent to qBittorrent
	if err := i.syncManager.AddTorrent(ctx, req.InstanceID, req.TorrentBytes, options); err != nil {
		i.rollbackLinkTree(addMode, linkPlan)
		result.ErrorMessage = fmt.Sprintf("failed to add torrent: %v", err)
		return result, fmt.Errorf("add torrent: %w", err)
	}

	i.triggerRecheckForPausedPartial(ctx, req)

	result.Success = true
	result.TorrentHash = req.ParsedTorrent.InfoHash
	return result, nil
}

func (i *Injector) triggerRecheckForPausedPartial(ctx context.Context, req *InjectRequest) {
	if i == nil || i.syncManager == nil || req == nil || req.ParsedTorrent == nil || req.MatchResult == nil {
		return
	}
	if !req.StartPaused {
		return
	}
	if len(req.MatchResult.UnmatchedTorrentFiles) == 0 {
		return
	}

	hash := req.ParsedTorrent.InfoHash
	if err := i.syncManager.BulkAction(ctx, req.InstanceID, []string{hash}, "recheck"); err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", req.InstanceID).
			Str("hash", hash).
			Msg("dirscan: failed to trigger recheck after add")
		return
	}

	i.syncManager.ResumeWhenComplete(req.InstanceID, []string{hash}, qbsync.ResumeWhenCompleteOptions{
		Timeout: 60 * time.Minute,
	})
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
) (savePath, mode string, linkPlan *hardlinktree.TreePlan, err error) {
	if instance == nil {
		return "", "", nil, errors.New("instance is nil")
	}

	if !instance.UseReflinks && !instance.UseHardlinks {
		return i.calculateSavePath(req), injectModeRegular, nil, nil
	}

	plan, linkMode, linkErr := i.materializeLinkTree(ctx, instance, req)
	if linkErr == nil {
		if plan == nil || plan.RootDir == "" {
			return "", "", nil, errors.New("link-tree plan missing root dir")
		}
		return plan.RootDir, linkMode, plan, nil
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

func (i *Injector) rollbackLinkTree(mode string, plan *hardlinktree.TreePlan) {
	if plan == nil || plan.RootDir == "" {
		return
	}

	var rollbackErr error
	switch mode {
	case injectModeHardlink:
		rollbackErr = hardlinktree.Rollback(plan)
	case injectModeReflink:
		rollbackErr = reflinktree.Rollback(plan)
	default:
		return
	}

	if rollbackErr != nil {
		log.Warn().Err(rollbackErr).Str("rootDir", plan.RootDir).Str("mode", mode).Msg("dirscan: failed to rollback link tree")
	}
	_ = os.Remove(plan.RootDir)
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

func (i *Injector) materializeLinkTree(ctx context.Context, instance *models.Instance, req *InjectRequest) (*hardlinktree.TreePlan, string, error) {
	if err := validateLinkTreeInstance(instance); err != nil {
		return nil, "", err
	}
	if req == nil || req.ParsedTorrent == nil || req.MatchResult == nil {
		return nil, "", errors.New("link-tree request is missing required data")
	}

	incomingFiles := buildLinkTreeIncomingFiles(req.ParsedTorrent)
	needsIsolation := !hardlinktree.HasCommonRootFolder(incomingFiles)

	incomingTrackerDomain := crossseed.ParseTorrentAnnounceDomain(req.TorrentBytes)
	trackerDisplayName := i.resolveTrackerDisplayName(ctx, incomingTrackerDomain, indexerName(req.SearchResult))
	destDir := buildLinkDestDir(instance, req.ParsedTorrent.InfoHash, req.ParsedTorrent.Name, needsIsolation, trackerDisplayName)

	if err := os.MkdirAll(instance.HardlinkBaseDir, 0o750); err != nil {
		return nil, "", fmt.Errorf("create hardlink base dir: %w", err)
	}

	linkableFiles, existingFiles, err := buildLinkTreeMatchedFiles(req.MatchResult)
	if err != nil {
		return nil, "", err
	}

	plan, err := hardlinktree.BuildPlan(linkableFiles, existingFiles, hardlinktree.LayoutOriginal, req.ParsedTorrent.Name, destDir)
	if err != nil {
		return nil, "", fmt.Errorf("build link plan: %w", err)
	}

	mode, err := i.createLinkTree(instance, existingFiles, plan)
	if err != nil {
		return nil, "", err
	}

	return plan, mode, nil
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
			Size:    pair.SearcheeFile.Size,
		})
	}

	if len(linkableFiles) == 0 || len(existingFiles) == 0 {
		return nil, nil, errors.New("no matched files available for link-tree creation")
	}

	return linkableFiles, existingFiles, nil
}

func (i *Injector) createLinkTree(instance *models.Instance, existingFiles []hardlinktree.ExistingFile, plan *hardlinktree.TreePlan) (string, error) {
	if instance.UseReflinks {
		if supported, reason := reflinktree.SupportsReflink(instance.HardlinkBaseDir); !supported {
			return "", fmt.Errorf("%w: %s", reflinktree.ErrReflinkUnsupported, reason)
		}
		if err := reflinktree.Create(plan); err != nil {
			return "", fmt.Errorf("create reflink tree: %w", err)
		}
		return injectModeReflink, nil
	}

	if instance.UseHardlinks {
		sameFS, err := fsutil.SameFilesystem(existingFiles[0].AbsPath, instance.HardlinkBaseDir)
		if err != nil {
			return "", fmt.Errorf("verify same filesystem: %w", err)
		}
		if !sameFS {
			return "", fmt.Errorf(
				"hardlink source (%s) and destination (%s) are on different filesystems",
				existingFiles[0].AbsPath,
				instance.HardlinkBaseDir,
			)
		}

		if err := hardlinktree.Create(plan); err != nil {
			if errors.Is(err, syscall.EXDEV) {
				return "", fmt.Errorf(
					"create hardlink tree: %w (hardlinks cannot cross filesystems; put your scanned directory and hardlink base dir on the same mount, or enable reflinks if supported)",
					err,
				)
			}
			return "", fmt.Errorf("create hardlink tree: %w", err)
		}
		return injectModeHardlink, nil
	}

	return "", errors.New("no link mode enabled")
}

func buildLinkDestDir(instance *models.Instance, torrentHash, torrentName string, needsIsolation bool, trackerDisplayName string) string {
	baseDir := instance.HardlinkBaseDir

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

func (i *Injector) applyAddPolicy(options map[string]string, req *InjectRequest) {
	if req == nil || req.ParsedTorrent == nil {
		return
	}

	files := make(qbt.TorrentFiles, 0, len(req.ParsedTorrent.Files))
	for _, f := range req.ParsedTorrent.Files {
		files = append(files, qbt.TorrentFiles{{
			Name: f.Path,
			Size: f.Size,
		}}...)
	}

	policy := crossseed.PolicyForSourceFiles(files)
	policy.ApplyToAddOptions(options)
}

// InjectBatch injects multiple torrents.
// Returns results for each injection attempt.
func (i *Injector) InjectBatch(ctx context.Context, requests []*InjectRequest) []*InjectResult {
	results := make([]*InjectResult, len(requests))

	for idx, req := range requests {
		result, err := i.Inject(ctx, req)
		if err != nil {
			// Error is already captured in result.ErrorMessage
			results[idx] = result
			continue
		}
		results[idx] = result
	}

	return results
}
