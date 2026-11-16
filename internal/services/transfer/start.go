// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
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
	way, err := checkWay(source, target, req.Options.ForcedWay)
	if err != nil {
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
	group, err := s.groupOf(ctx, source.ID, src)
	if err != nil {
		return nil, err
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
	targetFiles, err := s.deps.Files.GetBackend(ctx, target.ID)
	if err != nil {
		return nil, fmt.Errorf("read the files of %s: %w", target.Name, err)
	}
	td := targetFiles.Paths()
	if !td.IsAbs(placement.SavePath) {
		return nil, refuse("The save path %q on %s is not an absolute path.", placement.SavePath, target.Name)
	}
	placement.SavePath = td.Clean(placement.SavePath)
	members, err := s.members(ctx, source, target, group, placement)
	if err != nil {
		return nil, err
	}
	if way == WayLocal && s.seesSameFiles(ctx, source, target, targetFiles, group, members) {
		way = WayNone
	}

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
		Way:              way,
		State:            StateWaiting,
		StartedBy:        startedBy,
		RuleID:           req.RuleID,
		SourceInstanceID: source.ID,
		TargetInstanceID: target.ID,
		SourceEndpoint:   fsops.EndpointOf(source),
		TargetEndpoint:   fsops.EndpointOf(target),
		Options:          options,
		Steps:            []Step{{Name: "start", At: s.deps.Now()}},
		Members:          members,
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

// checkWay picks a copy through qui when qui reaches the files of a side on
// its own disk, and else a Transfer torrent between the clients.
func checkWay(source, target *models.Instance, forced Way) (Way, error) {
	from, to := models.FilesystemAccessMode(source), models.FilesystemAccessMode(target)
	switch forced {
	case "":
		switch {
		case from == models.FilesystemModeNone || to == models.FilesystemModeNone:
			return WayBitTorrent, nil
		case from == models.FilesystemModeLocal && to == models.FilesystemModeLocal:
			return WayLocal, nil
		case from == models.FilesystemModeLocal || to == models.FilesystemModeLocal:
			return WaySFTP, nil
		}
		return WayBitTorrent, nil
	case WaySFTP:
		for _, inst := range []*models.Instance{source, target} {
			if models.FilesystemAccessMode(inst) == models.FilesystemModeNone {
				return "", refuse("A copy through qui needs file access to %s, on qui's own disk or over SSH.", inst.Name)
			}
		}
		if from == models.FilesystemModeLocal && to == models.FilesystemModeLocal {
			return WayLocal, nil
		}
		return WaySFTP, nil
	case WayBitTorrent:
		return WayBitTorrent, nil
	case WayLocal, WayNone:
	}
	return "", refuse("Unknown way: %q.", forced)
}

// seesSameFiles reports whether each file of the group, at its place on the
// target, is the file of the source, as the endpoint module tells it.
func (s *Service) seesSameFiles(ctx context.Context, source, target *models.Instance, targetFiles fsops.Backend, group []*source, members []Member) bool {
	srcFiles, err := s.deps.Files.GetBackend(ctx, source.ID)
	if err != nil {
		return false
	}
	d, td := srcFiles.Paths(), targetFiles.Paths()
	for i, src := range group {
		for _, f := range src.files {
			a, err := srcFiles.Lstat(ctx, d.Join(src.torrent.SavePath, d.FromSlash(f.Name)))
			if err != nil {
				return false
			}
			b, err := targetFiles.Lstat(ctx, td.Join(members[i].Placement.SavePath, td.FromSlash(f.Name)))
			if err != nil || fsops.CompareFiles(source, a.FileID, target, b.FileID) != fsops.FilesSame {
				return false
			}
		}
	}
	return true
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

// othersRead reports whether a torrent outside hashes reads their files: on
// the instance, and on another instance. A torrent that qui could not check
// counts as read.
func (s *Service) othersRead(ctx context.Context, instanceID int, hashes []string) (bool, error) {
	res, err := s.deps.Groups.FindDeleteMatches(ctx, map[int][]string{instanceID: hashes})
	if err != nil {
		return true, err
	}
	return len(res.Matches) > 0 || len(res.Unchecked) > 0, nil
}
