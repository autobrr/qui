// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
)

// Request is the Transfer of one torrent.
type Request struct {
	SourceInstanceID int
	Hash             string
	TargetInstanceID int
	// Placement is where the torrent lands. An empty save path is the
	// default save path of the target. ContentLayout is ignored: the target
	// gets the layout that is on disk.
	Placement Placement
	Options   Options
	StartedBy StartedBy
	RuleID    *int
}

// RefusalError is why qui does not start a Transfer, for the user.
type RefusalError struct{ Reason string }

func (r *RefusalError) Error() string { return r.Reason }

func refuse(format string, args ...any) error {
	return &RefusalError{Reason: fmt.Sprintf(format, args...)}
}

const (
	reasonV2      = "v2 and hybrid torrents are unsupported"
	reasonSkipped = "Torrents with skipped files are unsupported"
)

// check runs the entry checks and returns the Transfer to record.
func (s *Service) check(ctx context.Context, req Request) (*Record, error) {
	hash := strings.ToLower(strings.TrimSpace(req.Hash))
	if hash == "" {
		return nil, refuse("Name the torrent to transfer.")
	}
	if req.SourceInstanceID == req.TargetInstanceID {
		return nil, refuse("The Transfer source and the Transfer target are the same instance.")
	}
	source, err := s.instance(ctx, req.SourceInstanceID)
	if err != nil {
		return nil, err
	}
	target, err := s.instance(ctx, req.TargetInstanceID)
	if err != nil {
		return nil, err
	}
	for _, inst := range []*models.Instance{source, target} {
		if err := s.supported(ctx, inst); err != nil {
			return nil, err
		}
	}
	if err := checkWay(source, target, req.Options.ForcedWay); err != nil {
		return nil, err
	}
	if s.deps.Store.IsHelper(source.ID, hash) {
		return nil, refuse("qui added this torrent for a Transfer or a Share.")
	}

	src, err := s.readSource(ctx, source.ID, hash)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, refuse("%s has no torrent %s.", source.Name, hash)
	}
	if err := src.movable(); err != nil {
		return nil, err
	}
	if onTarget, err := s.has(ctx, target.ID, hash); err != nil {
		return nil, err
	} else if onTarget {
		return nil, refuse("Already on the target: %s has this torrent.", target.Name)
	}
	if shared, err := s.othersRead(ctx, source.ID, hash, false); err != nil {
		return nil, fmt.Errorf("find the torrents that share the files: %w", err)
	} else if shared {
		return nil, refuse("Other torrents on %s share the files of this torrent. qui cannot transfer a Shared-file group yet.", source.Name)
	}

	placement := req.Placement
	placement.ContentLayout = ""
	if placement.SavePath == "" {
		prefs, err := s.deps.Sync.GetAppPreferences(ctx, target.ID)
		if err != nil {
			return nil, fmt.Errorf("read the default save path of %s: %w", target.Name, err)
		}
		placement.SavePath = prefs.SavePath
	}
	if !filepath.IsAbs(placement.SavePath) {
		return nil, refuse("The save path %q on %s is not an absolute path.", placement.SavePath, target.Name)
	}
	placement.SavePath = filepath.Clean(placement.SavePath)

	options := req.Options
	if options.After == "" {
		options.After = AfterRemoveWithFiles
	}
	if !slices.Contains([]After{AfterRemoveWithFiles, AfterKeepFiles, AfterKeepSeeding}, options.After) {
		return nil, refuse("Unknown choice for after the transfer: %q.", options.After)
	}
	startedBy := req.StartedBy
	if startedBy == "" {
		startedBy = StartedByUser
	}
	return &Record{
		Kind:             KindTransfer,
		Way:              WaySFTP,
		State:            StateWaiting,
		StartedBy:        startedBy,
		RuleID:           req.RuleID,
		SourceInstanceID: source.ID,
		TargetInstanceID: target.ID,
		SourceEndpoint:   fsops.EndpointOf(source),
		TargetEndpoint:   fsops.EndpointOf(target),
		Options:          options,
		Steps:            []Step{{Name: "start", At: s.deps.Now()}},
		Members: []Member{{
			Hash:             hash,
			Name:             src.torrent.Name,
			SourceSavePath:   src.torrent.SavePath,
			TargetInstanceID: target.ID,
			Placement:        placement,
		}},
	}, nil
}

func (s *Service) instance(ctx context.Context, id int) (*models.Instance, error) {
	inst, err := s.deps.Instances.Get(ctx, id)
	if err != nil || inst == nil {
		return nil, refuse("Instance %d does not exist.", id)
	}
	return inst, nil
}

// supported refuses an instance before qBittorrent 5.1.
func (s *Service) supported(ctx context.Context, inst *models.Instance) error {
	client, err := s.deps.Sync.GetClient(ctx, inst.ID)
	if err != nil {
		return refuse("qui cannot reach qBittorrent on %s: %v", inst.Name, err)
	}
	if !client.Supports(qbittorrent.CapTransferAndShare) {
		return refuse("Transfer needs qBittorrent 5.1 or newer. Update qBittorrent on %s.", inst.Name)
	}
	return nil
}

// checkWay refuses a Transfer that the SFTP way cannot do: it reads the
// source over SSH and writes on qui's own disk.
func checkWay(source, target *models.Instance, forced Way) error {
	if forced != "" && forced != WaySFTP {
		return refuse("qui cannot transfer by %s yet.", forced)
	}
	if models.FilesystemAccessMode(source) != models.FilesystemModeRemote || models.FilesystemAccessMode(target) != models.FilesystemModeLocal {
		return refuse("For now, a Transfer needs SSH access to %s and %s on qui's own disk.", source.Name, target.Name)
	}
	return nil
}

// source is the torrent on the Transfer source as one fresh read found it.
type source struct {
	torrent qbt.Torrent
	files   qbt.TorrentFiles
	export  []byte
	info    *metainfo.Info
}

// readSource reads the torrent, its files and its .torrent fresh, and
// returns nil when the instance does not have the torrent.
func (s *Service) readSource(ctx context.Context, instanceID int, hash string) (*source, error) {
	torrent, err := s.torrent(ctx, instanceID, hash)
	if err != nil || torrent == nil {
		return nil, err
	}
	files, err := s.deps.Sync.GetTorrentFiles(qbittorrent.WithForceFilesRefresh(ctx), instanceID, hash)
	if err != nil {
		return nil, fmt.Errorf("read the files of the torrent: %w", err)
	}
	if files == nil {
		return nil, nil
	}
	export, _, _, err := s.deps.Sync.ExportTorrent(ctx, instanceID, hash)
	if err != nil {
		return nil, fmt.Errorf("export the torrent: %w", err)
	}
	mi, err := metainfo.Load(bytes.NewReader(export))
	if err != nil {
		return nil, fmt.Errorf("decode the exported torrent: %w", err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, fmt.Errorf("decode the exported torrent: %w", err)
	}
	return &source{torrent: *torrent, files: *files, export: export, info: &info}, nil
}

// movable refuses a torrent that a Transfer cannot move.
func (src *source) movable() error {
	if src.torrent.InfohashV2 != "" || src.info.HasV2() {
		return refuse(reasonV2)
	}
	if slices.ContainsFunc(src.files, func(f qbt.TorrentFile) bool { return f.Priority == 0 }) {
		return refuse(reasonSkipped)
	}
	if src.torrent.Progress < 1 || slices.ContainsFunc(src.files, func(f qbt.TorrentFile) bool { return f.Progress < 1 }) {
		return refuse("Only complete torrents can move, and %s is not complete.", src.torrent.Name)
	}
	if _, err := planFiles(src.info, src.files); err != nil {
		return refuse("The files of %s do not match its .torrent: %v", src.torrent.Name, err)
	}
	return nil
}

// torrent reads one torrent from a fresh sync, nil when it is not there.
func (s *Service) torrent(ctx context.Context, instanceID int, hash string) (*qbt.Torrent, error) {
	torrents, err := s.deps.Sync.GetTorrentRefsFresh(ctx, instanceID, qbittorrent.TorrentRefFilter{Hashes: []string{hash}})
	if err != nil {
		return nil, fmt.Errorf("read the torrents of instance %d: %w", instanceID, err)
	}
	if len(torrents) == 0 {
		return nil, nil
	}
	t := *torrents[0]
	return &t, nil
}

func (s *Service) has(ctx context.Context, instanceID int, hash string) (bool, error) {
	torrent, err := s.torrent(ctx, instanceID, hash)
	return torrent != nil, err
}

// othersRead reports whether another torrent on the instance reads the files
// of the torrent, and with otherInstances, a torrent on another instance. A
// torrent that qui could not check counts as read.
func (s *Service) othersRead(ctx context.Context, instanceID int, hash string, otherInstances bool) (bool, error) {
	res, err := s.deps.Groups.FindDeleteMatches(ctx, map[int][]string{instanceID: {hash}})
	if err != nil {
		return true, err
	}
	if otherInstances && len(res.Unchecked) > 0 {
		return true, nil
	}
	return slices.ContainsFunc(res.Matches, func(m crossseed.LocalMatch) bool {
		return otherInstances || m.InstanceID == instanceID
	}), nil
}
