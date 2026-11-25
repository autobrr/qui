// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/services/linktree"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

const (
	injectModeHardlink = "hardlink"
	injectModeReflink  = "reflink"
	injectModeRegular  = "regular"

	qbitContentLayoutOriginal    = "Original"
	qbitContentLayoutNoSubfolder = "NoSubfolder"
)

var errSettleQueueFull = errors.New("recheck and resume queue full, manual intervention required")

var errLeftLocalMode = errors.New("instance does not have local filesystem access")

var errRemoteTargetChanged = errors.New("remote instance changed since the scan read it")

type Injector struct {
	jackettService            JackettDownloader
	aligner                   *crossseed.Aligner
	adder                     *crossseed.Adder
	torrentChecker            TorrentChecker
	trackerCustomizationStore trackerCustomizationProvider
	backendPool               *fsops.Pool
}

type JackettDownloader interface {
	DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error)
}

type TorrentChecker interface {
	HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error)
}

type trackerCustomizationProvider interface {
	List(ctx context.Context) ([]*models.TrackerCustomization, error)
}

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

type InjectRequest struct {
	InstanceID int

	ScanInstance *models.Instance

	Check crossseed.PreAddCheck

	TorrentBytes []byte

	ParsedTorrent *ParsedTorrent

	Searchee *Searchee

	MatchResult *MatchResult

	SearchResult *jackett.SearchResult

	SavePath string

	QbitPathPrefix string

	Category string

	Tags []string

	StartPaused bool

	DownloadMissingFiles bool
}

type InjectResult struct {
	Success bool

	Class crossseed.ResultClass

	SettleQueueFull bool

	Mode string

	SavePath string

	ErrorMessage string
}

func (i *Injector) Inject(ctx context.Context, req *InjectRequest) (*InjectResult, error) {
	result := &InjectResult{Class: crossseed.ClassFailed}

	if err := i.validateInjectRequest(req); err != nil {
		return result, err
	}
	if i.adder == nil {
		return result, errors.New("torrent adder is nil")
	}

	backend, instance, err := i.resolveTarget(ctx, req)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result, err
	}

	savePath, addMode, tree, err := i.prepareInjection(ctx, instance, backend, req)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result, err
	}

	result.Mode = addMode
	result.SavePath = savePath

	hasUnmatchedFiles := len(req.MatchResult.UnmatchedTorrentFiles) > 0
	linkTree := isLinkTreeMode(addMode)

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

	if linkTree && hasUnmatchedFiles && !req.DownloadMissingFiles {
		rollbackLinkTree(ctx, tree)
		return result, fmt.Errorf("partial match has %d missing files; enable 'Download missing files' to allow",
			len(req.MatchResult.UnmatchedTorrentFiles))
	}

	placement := crossseed.AddPlacement{
		SavePath:      savePath,
		Category:      req.Category,
		Tags:          req.Tags,
		ContentLayout: qbitContentLayoutOriginal,
	}
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
		rollbackLinkTree(ctx, tree)
		result.ErrorMessage = fmt.Sprintf("failed to add torrent: %v", added.Err)
		return result, fmt.Errorf("add torrent: %w", added.Err)
	case added.Outcome == crossseed.AddLeftPaused:
		result.ErrorMessage = "alignment failed; torrent added paused for inspection"
	case added.Settled.QueueFull && added.Paused:
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
) (savePath, mode string, tree *linktree.Tree, err error) {
	if instance == nil {
		return "", "", nil, errors.New("instance is nil")
	}

	cfg, linkMode := linktree.ConfigOf(instance)
	if !linkMode {
		savePath, err = i.calculateSavePath(ctx, backend, req)
		return savePath, injectModeRegular, nil, err
	}

	tree, linkErr := i.materializeLinkTree(ctx, instance, cfg, backend, req)
	if linkErr == nil {
		return tree.SavePath, tree.Kind.String(), tree, nil
	}

	if failure, ok := errors.AsType[*linktree.Error](linkErr); !instance.FallbackToRegularMode || (ok && failure.Kind == linktree.ConnectionLost) {
		return "", "", nil, linkErr
	}

	i.logLinkTreeFallback(instance, cfg.Kind, linkErr)
	savePath, err = i.calculateSavePath(ctx, backend, req)
	return savePath, injectModeRegular, nil, err
}

func (i *Injector) logLinkTreeFallback(instance *models.Instance, kind linktree.LinkKind, err error) {
	fallbackReason := "link-tree creation failed"
	if errors.Is(err, syscall.EXDEV) {
		fallbackReason = "invalid cross-device link"
	}

	log.Warn().
		Err(err).
		Int("instanceID", instance.ID).
		Str("instanceName", instance.Name).
		Str("linkMode", kind.String()).
		Str("reason", fallbackReason).
		Msg("dirscan: falling back to regular mode")
}

func rollbackLinkTree(ctx context.Context, tree *linktree.Tree) {
	if tree == nil {
		return
	}
	if err := tree.Rollback(ctx); err != nil {
		log.Warn().Err(err).Str("savePath", tree.SavePath).Msg("dirscan: failed to rollback link tree")
	}
}

func (i *Injector) calculateSavePath(ctx context.Context, backend fsops.Backend, req *InjectRequest) (string, error) {
	savePath := req.SavePath
	if savePath == "" {
		savePath = backend.Paths().Dir(req.Searchee.Path)

		useSearchee, err := shouldUseSearcheeDirectory(ctx, backend, req.Searchee.Path, req.ParsedTorrent)
		if err != nil {
			return "", err
		}
		if useSearchee {
			savePath = req.Searchee.Path
		}
	}

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

func applyPathMapping(savePath, searcheePath, qbitPrefix string) string {
	if suffix, found := strings.CutPrefix(savePath, searcheePath); found {
		return qbitPrefix + suffix
	}
	return qbitPrefix
}

func discMarker(parsed *ParsedTorrent) string {
	files := make(qbt.TorrentFiles, 0, len(parsed.Files))
	for _, f := range parsed.Files {
		files = append(files, qbt.TorrentFile{Name: f.Path, Size: f.Size})
	}
	return crossseed.PolicyForSourceFiles(files).DiscMarker
}

func (i *Injector) materializeLinkTree(ctx context.Context, instance *models.Instance, cfg linktree.Config, backend fsops.Backend, req *InjectRequest) (*linktree.Tree, error) {
	if err := validateLinkTreeInstance(instance); err != nil {
		return nil, err
	}

	torrentFiles := make([]string, 0, len(req.ParsedTorrent.Files))
	for _, f := range req.ParsedTorrent.Files {
		torrentFiles = append(torrentFiles, f.Path)
	}
	links := make([]linktree.Link, 0, len(req.MatchResult.MatchedFiles))
	for _, pair := range req.MatchResult.MatchedFiles {
		links = append(links, linktree.Link{TorrentPath: pair.TorrentFile.Path, SourcePath: pair.SearcheeFile.Path})
	}

	plan, err := linktree.Plan(ctx, linktree.Request{
		Backend:        backend,
		Config:         cfg,
		TrackerFolder:  linktree.TrackerFolderName(ctx, i.trackerCustomizationStore, crossseed.ParseTorrentAnnounceDomain(req.TorrentBytes), indexerName(req.SearchResult)),
		InstanceFolder: instance.Name,
		Torrent:        linktree.Torrent{InfoHash: req.ParsedTorrent.InfoHash, Name: req.ParsedTorrent.Name, Files: torrentFiles},
		Links:          links,
	})
	if err == nil {
		var tree *linktree.Tree
		if tree, err = plan.Create(ctx); err == nil {
			return tree, nil
		}
	}

	switch failure, _ := errors.AsType[*linktree.Error](err); {
	case failure.Kind == linktree.InvalidPlan:
		log.Warn().
			Err(err).
			Int("instanceID", instance.ID).
			Str("instanceName", instance.Name).
			Str("torrentName", req.ParsedTorrent.Name).
			Msg("dirscan: failed to build link plan")
		return nil, humanizeLinkPlanError(failure.Err)
	case errors.Is(err, syscall.EXDEV):
		return nil, fmt.Errorf(
			"%w (hardlinks cannot cross filesystems; put your scanned directory and hardlink base dir on the same mount, or enable reflinks if supported)",
			err,
		)
	}
	return nil, err
}

func humanizeLinkPlanError(err error) error {
	if err == nil {
		return nil
	}

	const prefix = "couldn't prepare linked files for this release"

	var linkErr *hardlinktree.LinkPlanError
	if !errors.As(err, &linkErr) {
		return fmt.Errorf("%s: %w", prefix, err)
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

func validateLinkTreeInstance(instance *models.Instance) error {
	if !models.FilesystemCapabilitiesOf(instance).Write {
		return errors.New("instance does not have filesystem write access")
	}
	if instance.HardlinkBaseDir == "" {
		return errors.New("hardlink base directory is not configured")
	}
	return nil
}

func indexerName(result *jackett.SearchResult) string {
	if result == nil {
		return ""
	}
	return result.Indexer
}
