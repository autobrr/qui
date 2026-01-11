// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/fsutil"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/pathutil"
	"github.com/autobrr/qui/pkg/reflinktree"
)

// Injector handles downloading and injecting torrents into qBittorrent.
type Injector struct {
	jackettService JackettDownloader
	syncManager    TorrentAdder
	torrentChecker TorrentChecker
	instanceStore  InstanceProvider
}

// JackettDownloader is the interface for downloading torrent files.
type JackettDownloader interface {
	DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error)
}

// TorrentAdder is the interface for adding torrents to qBittorrent.
type TorrentAdder interface {
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) error
}

// TorrentChecker is the interface for checking if torrents exist in qBittorrent.
type TorrentChecker interface {
	HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error)
}

type InstanceProvider interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
}

// NewInjector creates a new injector.
func NewInjector(
	jackettService JackettDownloader,
	syncManager TorrentAdder,
	torrentChecker TorrentChecker,
	instanceStore InstanceProvider,
) *Injector {
	return &Injector{
		jackettService: jackettService,
		syncManager:    syncManager,
		torrentChecker: torrentChecker,
		instanceStore:  instanceStore,
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

	// Error message if injection failed.
	ErrorMessage string
}

// Inject downloads a torrent and injects it into qBittorrent.
func (i *Injector) Inject(ctx context.Context, req *InjectRequest) (*InjectResult, error) {
	result := &InjectResult{}

	if req == nil {
		return result, errors.New("inject request is nil")
	}
	if req.ParsedTorrent == nil || len(req.TorrentBytes) == 0 {
		return result, errors.New("inject request missing torrent bytes or parsed torrent")
	}
	if req.MatchResult == nil || !req.MatchResult.IsMatch {
		return result, errors.New("inject request missing valid match result")
	}
	if i.instanceStore == nil {
		return result, errors.New("instance store is nil")
	}

	instance, err := i.instanceStore.Get(ctx, req.InstanceID)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("failed to load instance: %v", err)
		return result, fmt.Errorf("get instance: %w", err)
	}

	savePath := ""
	var addMode string
	if instance.UseReflinks || instance.UseHardlinks {
		savePath, addMode, err = i.materializeLinkTree(ctx, instance, req)
		if err != nil {
			if instance.FallbackToRegularMode {
				savePath = i.calculateSavePath(req)
				addMode = "regular"
			} else {
				result.ErrorMessage = err.Error()
				return result, err
			}
		}
	} else {
		savePath = i.calculateSavePath(req)
		addMode = "regular"
	}

	options := i.buildAddOptions(req, savePath)
	options["contentLayout"] = "Original"
	options["skip_checking"] = "true"
	if addMode != "regular" {
		options["savepath"] = savePath
	}

	i.applyAddPolicy(options, req)

	// Add the torrent to qBittorrent
	if err := i.syncManager.AddTorrent(ctx, req.InstanceID, req.TorrentBytes, options); err != nil {
		result.ErrorMessage = fmt.Sprintf("failed to add torrent: %v", err)
		return result, fmt.Errorf("add torrent: %w", err)
	}

	result.Success = true
	result.TorrentHash = req.ParsedTorrent.InfoHash
	return result, nil
}

// calculateSavePath determines the save path for the torrent.
func (i *Injector) calculateSavePath(req *InjectRequest) string {
	// Start with the provided save path or derive from searchee
	savePath := req.SavePath
	if savePath == "" {
		// Use the parent directory of the searchee path
		savePath = filepath.Dir(req.Searchee.Path)
	}

	// Apply path prefix mapping for containers
	if req.QbitPathPrefix != "" {
		savePath = applyPathMapping(savePath, filepath.Dir(req.Searchee.Path), req.QbitPathPrefix)
	}

	return savePath
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
	options["autoTMM"] = "false"

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
		//nolint:goconst // standard qBittorrent API values
		options["paused"] = "true"
		options["stopped"] = "true"
	}

	return options
}

func (i *Injector) materializeLinkTree(ctx context.Context, instance *models.Instance, req *InjectRequest) (savePath string, mode string, err error) {
	if instance == nil {
		return "", "", errors.New("instance is nil")
	}
	if !instance.HasLocalFilesystemAccess {
		return "", "", errors.New("instance does not have local filesystem access enabled")
	}
	if instance.HardlinkBaseDir == "" {
		return "", "", errors.New("hardlink base directory is not configured")
	}

	candidateFiles := make([]hardlinktree.TorrentFile, 0, len(req.ParsedTorrent.Files))
	for _, f := range req.ParsedTorrent.Files {
		candidateFiles = append(candidateFiles, hardlinktree.TorrentFile{Path: f.Path, Size: f.Size})
	}

	needsIsolation := !hardlinktree.HasCommonRootFolder(candidateFiles)
	incomingTrackerDomain := crossseed.ParseTorrentAnnounceDomain(req.TorrentBytes)
	destDir := buildLinkDestDir(instance, req.ParsedTorrent.InfoHash, req.ParsedTorrent.Name, needsIsolation, incomingTrackerDomain)

	if err := os.MkdirAll(instance.HardlinkBaseDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create hardlink base dir: %w", err)
	}

	existingFiles := make([]hardlinktree.ExistingFile, 0, len(req.MatchResult.MatchedFiles))
	for _, pair := range req.MatchResult.MatchedFiles {
		existingFiles = append(existingFiles, hardlinktree.ExistingFile{
			AbsPath: pair.SearcheeFile.Path,
			RelPath: pair.TorrentFile.Path,
			Size:    pair.SearcheeFile.Size,
		})
	}
	if len(existingFiles) == 0 {
		return "", "", errors.New("no matched files available for link-tree creation")
	}

	plan, err := hardlinktree.BuildPlan(candidateFiles, existingFiles, hardlinktree.LayoutOriginal, req.ParsedTorrent.Name, destDir)
	if err != nil {
		return "", "", fmt.Errorf("build link plan: %w", err)
	}

	if instance.UseReflinks {
		if supported, reason := reflinktree.SupportsReflink(instance.HardlinkBaseDir); !supported {
			return "", "", fmt.Errorf("%w: %s", reflinktree.ErrReflinkUnsupported, reason)
		}
		if err := reflinktree.Create(plan); err != nil {
			return "", "", fmt.Errorf("create reflink tree: %w", err)
		}
		return plan.RootDir, "reflink", nil
	}

	if instance.UseHardlinks {
		sameFS, err := fsutil.SameFilesystem(existingFiles[0].AbsPath, instance.HardlinkBaseDir)
		if err != nil {
			return "", "", fmt.Errorf("verify same filesystem: %w", err)
		}
		if !sameFS {
			return "", "", errors.New("hardlink source and destination are on different filesystems")
		}

		if err := hardlinktree.Create(plan); err != nil {
			return "", "", fmt.Errorf("create hardlink tree: %w", err)
		}
		return plan.RootDir, "hardlink", nil
	}

	return "", "", errors.New("no link mode enabled")
}

func buildLinkDestDir(instance *models.Instance, torrentHash, torrentName string, needsIsolation bool, incomingTrackerDomain string) string {
	baseDir := instance.HardlinkBaseDir

	isolationFolder := ""
	if needsIsolation {
		isolationFolder = pathutil.IsolationFolderName(torrentHash, torrentName)
	}

	switch instance.HardlinkDirPreset {
	case "by-tracker":
		display := incomingTrackerDomain
		if display == "" {
			display = "unknown-tracker"
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
