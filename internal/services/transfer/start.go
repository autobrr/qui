// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"bytes"
	"context"
	"errors"
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
	// Placement is where the torrent lands. An empty save path is the save
	// path of the category on the target, or its default save path for no
	// category. ContentLayout is ignored: the target gets the layout that is
	// on disk.
	Placement Placement
	Options   Options
	StartedBy StartedBy
	RuleID    *int
}

// RefusalError is why qui does not start a Transfer, for the user. It wraps a
// sentinel such as ErrMoving when a caller must tell the case apart.
type RefusalError struct {
	Reason string
	err    error
}

func (r *RefusalError) Error() string { return r.Reason }

func (r *RefusalError) Unwrap() error { return r.err }

// ErrMoving is the refusal of a torrent that qBittorrent is moving, alone or
// as a member of the group. The Transfer can start once the move ends.
var ErrMoving = errors.New("qBittorrent is moving this torrent")

// refuse builds a refusal. A %w verb keeps its error for errors.Is.
func refuse(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	return &RefusalError{Reason: err.Error(), err: errors.Unwrap(err)}
}

const (
	reasonV2      = "v2 and hybrid torrents are unsupported"
	reasonSkipped = "Torrents with skipped files are unsupported"
)

// planned is the Transfer to record, with the Shared-file group as check
// read it.
type planned struct {
	record *Record
	group  []*source
	// stay holds the hashes of the cross-seeds on hardlinks that stay on the
	// source.
	stay []string
	// versions holds those of stay that have the same files as the torrent
	// of the request. Only a Transfer that a rule starts has them.
	versions []string
}

// check runs the entry checks and plans the Transfer.
func (s *Service) check(ctx context.Context, req Request) (*planned, error) {
	hash := strings.ToLower(strings.TrimSpace(req.Hash))
	if hash == "" {
		return nil, refuse("Name the torrent to transfer.")
	}
	if req.SourceInstanceID == req.TargetInstanceID {
		return nil, refuse("The Transfer source and the Transfer target are the same instance.")
	}
	if err := s.waitsForUser(ctx, req, hash); err != nil {
		return nil, err
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
	group, stay, err := s.groupOf(ctx, source.ID, src, way)
	if err != nil {
		return nil, err
	}
	others := make([]string, 0, len(group)-1)
	for _, member := range group[1:] {
		others = append(others, member.torrent.Hash)
	}
	versions, err := s.ruleChecks(ctx, req, source, src, stay, others)
	if err != nil {
		return nil, err
	}

	placement := req.Placement
	placement.ContentLayout, placement.AutoTMM = "", false
	placement.SavePathOfTarget = placement.SavePath == ""
	targetFiles, err := s.deps.Files.GetBackend(ctx, target.ID)
	if err != nil {
		return nil, fmt.Errorf("read the files of %s: %w", target.Name, err)
	}
	if placement.SavePathOfTarget {
		if placement.SavePath, placement.AutoTMM, err = s.categorySavePath(ctx, target, targetFiles, placement.Category); err != nil {
			return nil, err
		}
	}
	td := pathsOf(target, targetFiles, placement.SavePath)
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
	leaveFilesToSource(members, way, options.After)
	startedBy := req.StartedBy
	if startedBy == "" {
		startedBy = StartedByUser
	}
	var logged []LogLine
	if len(stay) > 0 {
		logged = []LogLine{{At: s.deps.Now(), Message: stayLine(len(stay), source, target)}}
	}
	return &planned{stay: stay, versions: versions, group: group, record: &Record{
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
		Log:              logged,
		Members:          members,
		Size:             src.torrent.Size,
	}}, nil
}

// leaveFilesToSource keeps the members of a Transfer with no copy on manual
// management when the source keeps seeding. The target reads the files of the
// source then. A target that manages the torrent moves them when the folder
// of its category changes, and the source would lose them.
func leaveFilesToSource(members []Member, way Way, after After) {
	if way != WayNone || after != AfterKeepSeeding {
		return
	}
	for i := range members {
		members[i].Placement.AutoTMM = false
	}
}

// ruleChecks runs the checks that only a Transfer of a rule gets, and returns
// ErrStays or ErrWaitsForUser when the rule must not start it. versions holds
// the torrents of stay that have the same files as first.
func (s *Service) ruleChecks(ctx context.Context, req Request, source *models.Instance, first *source, stay, others []string) (versions []string, err error) {
	if req.StartedBy != StartedByRule {
		return nil, nil
	}
	if versions, err = s.versionsOf(ctx, source, first, stay); err != nil {
		return nil, err
	}
	if err := s.alreadyPasses(ctx, req.SourceInstanceID, req.TargetInstanceID, versions); err != nil {
		return nil, err
	}
	return versions, s.waitsForUser(ctx, req, others...)
}

// alreadyPasses returns ErrStays when a Transfer holds one of versions, the
// cross-seeds on hardlinks that have the same files and that this Transfer
// leaves on the source, or the target has one. A rule matches each torrent of
// such a set, and a Transfer torrent passes the same files for each Transfer
// that starts.
func (s *Service) alreadyPasses(ctx context.Context, sourceID, targetID int, versions []string) error {
	if len(versions) == 0 {
		return nil
	}
	if slices.ContainsFunc(versions, func(hash string) bool { return s.deps.Store.IsClaimed(sourceID, hash) }) {
		return ErrStays
	}
	_, onTarget, err := s.deps.Sync.HasTorrentByAnyHash(ctx, targetID, versions)
	if err != nil {
		return fmt.Errorf("read the torrents of instance %d: %w", targetID, err)
	}
	if onTarget {
		return ErrStays
	}
	return nil
}

// waitsForUser returns ErrWaitsForUser for a Transfer that a rule starts when
// the last Transfer of one of the torrents failed for good.
func (s *Service) waitsForUser(ctx context.Context, req Request, hashes ...string) error {
	if req.StartedBy != StartedByRule {
		return nil
	}
	for _, hash := range hashes {
		waits, err := s.deps.Store.WaitsForUser(ctx, req.SourceInstanceID, hash)
		if err != nil {
			return err
		}
		if waits {
			return ErrWaitsForUser
		}
	}
	return nil
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

// categorySavePath is the folder where the qBittorrent of target saves a
// torrent of category under automatic management, and its default save path
// for no category. It also reports whether automatic management is the mode
// that target gives a new torrent.
func (s *Service) categorySavePath(ctx context.Context, target *models.Instance, files fsops.Backend, category string) (folder string, automatic bool, err error) {
	prefs, err := s.deps.Sync.GetAppPreferences(ctx, target.ID)
	if err != nil {
		return "", false, fmt.Errorf("read the default save path of %s: %w", target.Name, err)
	}
	if category == "" {
		return prefs.SavePath, prefs.AutoTmmEnabled, nil
	}
	folder, err = s.deps.Sync.CategorySaveFolder(ctx, pathsOf(target, files, prefs.SavePath), target.ID, &prefs, category)
	if err != nil {
		return "", false, fmt.Errorf("read the save path of category %q on %s: %w", category, target.Name, err)
	}
	return folder, prefs.AutoTmmEnabled, nil
}

// pathsOf is the path dialect of the save paths of inst: the dialect of its
// files, or for an instance whose files qui cannot reach, the one that the
// form of savePath shows. A Windows qBittorrent behind a Linux qui still
// reports Windows paths.
func pathsOf(inst *models.Instance, files fsops.Backend, savePath string) fsops.PathDialect {
	if models.FilesystemAccessMode(inst) == models.FilesystemModeNone {
		return fsops.ClientPaths(savePath)
	}
	return files.Paths()
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
	if src.torrent.State == qbt.TorrentStateMoving {
		return &RefusalError{Reason: fmt.Sprintf("qBittorrent is moving %s. A Transfer can start once the move ends.", src.torrent.Name),
			err: ErrMoving}
	}
	if src.torrent.InfohashV2 != "" || src.info.HasV2() {
		return refuse(reasonV2)
	}
	if slices.ContainsFunc(src.files, func(f qbt.TorrentFile) bool { return f.Priority == 0 }) {
		return refuse(reasonSkipped)
	}
	if src.torrent.Progress < 1 || slices.ContainsFunc(src.files, func(f qbt.TorrentFile) bool { return f.Progress < 1 }) {
		return refuse("Only complete torrents can move, and %s is not complete.", src.torrent.Name)
	}
	plan, err := planFiles(src.info, src.files)
	if err != nil {
		return refuse("The files of %s do not match its .torrent: %v", src.torrent.Name, err)
	}
	if name := takenName(src.info, plan); name != "" {
		return refuse("%s has the original name of another file of %s. Rename the files on the source so that no file "+
			"takes the original name of another.", name, src.torrent.Name)
	}
	return nil
}

// torrent reads one torrent from a fresh sync, nil when it is not there.
func (s *Service) torrent(ctx context.Context, instanceID int, hash string) (*qbt.Torrent, error) {
	return s.read(ctx, instanceID, hash, true)
}

// read reads one torrent, nil when it is not there. A read that is not fresh
// takes the Sync snapshot, and syncs only when that is older than
// Config.SnapshotAge.
func (s *Service) read(ctx context.Context, instanceID int, hash string, fresh bool) (*qbt.Torrent, error) {
	filter := qbittorrent.TorrentRefFilter{Hashes: []string{hash}}
	var torrents []*qbt.Torrent
	var err error
	if fresh {
		torrents, err = s.deps.Sync.GetTorrentRefsFresh(ctx, instanceID, filter)
	} else {
		torrents, err = s.deps.Sync.GetTorrentRefsWithin(ctx, instanceID, filter, s.deps.Config.SnapshotAge)
	}
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
