// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/notifications"
)

type failure struct {
	class FailureClass
	err   error
}

func (f *failure) Error() string { return f.err.Error() }

func (f *failure) Unwrap() error { return f.err }

func permanent(format string, args ...any) error {
	return &failure{class: FailurePermanent, err: fmt.Errorf(format, args...)}
}

func temporary(format string, args ...any) error {
	return &failure{class: FailureTemporary, err: fmt.Errorf(format, args...)}
}

func sourceChanged(what string) error {
	return temporary("The torrent changed on the source during the copy: %s.", what)
}

func sourceGone(what string) error {
	return permanent("The torrent changed on the source during the copy: %s.", what)
}

type job struct {
	svc            *Service
	rec            *Record
	source, target *models.Instance
	crossSeed      *models.Instance
	parts          []*part
	files          []*groupFile
	byRel          map[string]*groupFile
	srcFiles       fsops.Backend
	blind          bool
	dstFiles       fsops.Backend
	root           fsops.Root
	rootDir        string
	rootPaths      fsops.PathDialect
	reserved       int64
	helper         string
	stoppedSource  bool
	changedNothing bool
	made           map[*groupFile]int64
	unanswered     map[string]bool
	away           map[int]bool
	flushedPieces  int
	meter          *meter
}

func (j *job) hashes() []string {
	out := make([]string, len(j.rec.Members))
	for i := range j.rec.Members {
		out[i] = j.rec.Members[i].Hash
	}
	return out
}

func (j *job) torrents(article string) string {
	if len(j.rec.Members) == 1 {
		return article + " torrent"
	}
	return fmt.Sprintf("%s %d torrents", article, len(j.rec.Members))
}

func (j *job) its() string {
	if len(j.rec.Members) == 1 {
		return "its"
	}
	return "their"
}

func (j *job) tempName(name string) string {
	return fmt.Sprintf("%s.qui-transfer-%d", name, j.rec.ID)
}

func (j *job) run(ctx context.Context) error {
	if j.reached(stepRemoveFromSource) {
		return j.finishRemoval(ctx)
	}
	if err := j.prepare(ctx); err != nil {
		return err
	}
	if !j.reached(stepPlace) {
		if err := j.headStart(ctx); err != nil {
			return err
		}
		if j.rec.Way == WayBitTorrent {
			if err := j.passByBitTorrent(ctx); err != nil {
				return err
			}
		}
		if j.rec.Way != WayBitTorrent {
			if err := j.copyAll(ctx); err != nil {
				return err
			}
		}
		if err := j.prove(ctx); err != nil {
			return err
		}
	}
	if err := j.place(ctx); err != nil {
		return err
	}
	return j.after(ctx)
}

func (j *job) step(ctx context.Context, state State, name string) error {
	return j.stepWith(ctx, state, name, "")
}

func (j *job) stepWith(ctx context.Context, state State, name, detail string) error {
	if err := j.checkInstances(ctx); err != nil {
		return err
	}
	j.rec.State = state
	j.rec.Steps = append(j.rec.Steps, Step{Name: name, At: j.svc.deps.Now(), Detail: detail})
	return j.svc.deps.Store.Save(ctx, j.rec)
}

func (j *job) logf(format string, args ...any) {
	j.rec.Log = append(j.rec.Log, LogLine{At: j.svc.deps.Now(), Message: fmt.Sprintf(format, args...)})
}

func (j *job) prepare(ctx context.Context) error {
	resumed := j.reached(stepPrepare)
	j.rec.RetryAt, j.rec.FailureClass, j.rec.FailureReason = time.Time{}, "", ""
	if err := j.step(ctx, StateCopying, stepPrepare); err != nil {
		return err
	}
	bitTorrent := j.rec.Way == WayBitTorrent
	var err error
	if !bitTorrent || models.FilesystemAccessMode(j.source) != models.FilesystemModeNone {
		if j.srcFiles, j.source, err = j.svc.deps.Files.Require(ctx, j.rec.SourceInstanceID, models.CapabilityReadBytes); err != nil {
			return temporary("qui cannot read the files of the Transfer source: %v", err)
		}
	}
	j.blind = bitTorrent && models.FilesystemAccessMode(j.target) == models.FilesystemModeNone
	if j.blind {
		if j.dstFiles, err = j.svc.deps.Files.GetBackend(ctx, j.rec.TargetInstanceID); err != nil {
			return temporary("qui cannot read the paths of the Transfer target: %v", err)
		}
	} else if j.dstFiles, j.target, err = j.svc.deps.Files.Require(ctx, j.rec.TargetInstanceID, models.CapabilityWriteBytes); err != nil {
		return temporary("qui cannot write the files of the Transfer target: %v", err)
	}

	if err := j.readParts(ctx, pathsOf(j.target, j.dstFiles, j.rec.Members[0].Placement.SavePath)); err != nil {
		return err
	}
	if err := j.recordNames(ctx); err != nil {
		return err
	}
	if err := j.land(ctx); err != nil {
		j.changedNothing = !resumed
		return err
	}
	if !j.blind {
		if err := j.openRoot(ctx, resumed); err != nil {
			return err
		}
	}

	need := j.need()
	if j.reached(stepPlace) {
		need = 0
	}
	var known bool
	if j.blind {
		var read space
		read, err = j.freeSpace(ctx)
		known = read.known
	} else {
		known, err = j.reserve(ctx, need)
	}
	if err != nil {
		return err
	}
	if !resumed {
		j.logStart(ctx, need, known)
		return nil
	}
	if err := j.removeHelpers(ctx); err != nil {
		return temporary("qui cannot remove its helper torrents: %v", err)
	}
	if j.reached(stepPlace) {
		j.logf("Resumed: the files are in place on %s.", j.target.Name)
		return nil
	}
	proven, pieces := 0, 0
	for _, p := range j.parts {
		proven += provenCount(p.ProvenPieces)
		pieces += p.src.info.NumPieces()
	}
	j.logf("Resumed: %d of %d pieces are proven, and %s are left to copy.", proven, pieces, formatBytes(need))
	return nil
}

func (j *job) openRoot(ctx context.Context, resumed bool) error {
	opener, ok := j.dstFiles.(fsops.RootOpener)
	if !ok {
		return permanent("qui cannot write new files on %s.", j.target.Name)
	}
	var err error
	j.root, err = opener.OpenRoot(ctx, j.rootDir)
	switch {
	case errors.Is(err, fsops.ErrConnectionLost):
		return temporary("qui cannot open the save path %s on %s: %w", j.rootDir, j.target.Name, err)
	case err != nil:
		return permanent("qui cannot open the save path %s on %s: %v", j.rootDir, j.target.Name, err)
	}
	find := j.findOnTarget
	if resumed {
		find = j.findOnTargetAgain
	}
	if err := find(ctx); err != nil {
		return err
	}
	if j.rec.Way == WayLocal && !j.reached(stepPlace) {
		return j.linkFiles(ctx, j.dstFiles)
	}
	return nil
}

func (j *job) logStart(ctx context.Context, need int64, known bool) {
	if !known {
		j.logf("Free space unknown on %s: before qBittorrent 5.3, qBittorrent reports it only at its default save path.", j.target.Name)
	}
	switch j.rec.Way {
	case WayNone:
		j.logf("No copy: %s sees the same files as %s.", j.target.Name, j.source.Name)
		return
	case WayBitTorrent:
		j.logf("Passing %s by a Transfer torrent from %s to %s.", formatBytes(need), j.source.Name, j.target.Name)
		if j.blind {
			j.logf("qui cannot see the files on %s, so a file with other content at the same path can be replaced.", j.target.Name)
		}
		j.hint(ctx)
		return
	case WayLocal, WaySFTP:
	}
	how := "over SFTP"
	if j.rec.Way == WayLocal {
		how = "on qui's own disk"
	}
	linked, size := linkedBytes(j.files)
	if linked > 0 {
		j.logf("Linking %d files (%s) %s from %s to %s.", linked, formatBytes(size), how, j.source.Name, j.target.Name)
	}
	if n, size := j.hardlinkBytes(); n > 0 {
		j.logf("%d files (%s) are hardlinks of other files on %s, so qui copies them once and links them on %s.", n, formatBytes(size), j.source.Name, j.target.Name)
	}
	if need > 0 || linked == 0 {
		j.logf("Copying %s %s from %s to %s.", formatBytes(need), how, j.source.Name, j.target.Name)
	}
}

func (j *job) hint(ctx context.Context) {
	for _, inst := range j.svc.onTCPAndUTP(ctx, j.source, j.target) {
		j.rec.Log = append(j.rec.Log, LogLine{
			At: j.svc.deps.Now(),
			Message: fmt.Sprintf(`%s uses qBittorrent's default "TCP and μTP". In qui's tests, "TCP only" was about three times faster. `+
				"qui does not change this setting.", inst.Name),
			Hint: &Hint{Kind: HintTCPAndUTP, InstanceID: inst.ID},
		})
	}
}

func (s *Service) onTCPAndUTP(ctx context.Context, instances ...*models.Instance) []*models.Instance {
	var out []*models.Instance
	for _, inst := range instances {
		prefs, err := s.deps.Sync.GetAppPreferences(ctx, inst.ID)
		if err == nil && prefs.BittorrentProtocol == bittorrentTCPAndUTP {
			out = append(out, inst)
		}
	}
	return out
}

const bittorrentTCPAndUTP = 0

func (j *job) readParts(ctx context.Context, d fsops.PathDialect) error {
	var (
		err   error
		parts []*part
	)
	saves := make([]string, len(j.rec.Members))
	for i := range j.rec.Members {
		saves[i] = j.rec.Members[i].Placement.SavePath
	}
	j.rootDir, j.rootPaths = topFolder(d, saves), d
	for i := range j.rec.Members {
		p := &part{Member: &j.rec.Members[i], position: i}
		if p.src, err = j.svc.readSource(ctx, j.source.ID, p.Hash); err != nil {
			return err
		}
		if p.src == nil {
			return sourceGone("it left the source")
		}
		if err := p.src.movable(); err != nil {
			return permanent("%v", err)
		}
		if p.src.torrent.SavePath != p.SourceSavePath {
			return sourceGone("it moved")
		}
		if p.plan, err = planFiles(p.src.info, p.src.files); err != nil {
			return permanent("The files of the torrent do not match its .torrent: %v", err)
		}
		rel, err := d.Rel(j.rootDir, d.Clean(p.Placement.SavePath))
		if err != nil {
			return permanent("qui cannot place %s on %s: %v", p.Name, j.target.Name, err)
		}
		if p.dir = d.ToSlash(rel); p.dir == "." {
			p.dir = ""
		}
		parts = append(parts, p)
	}
	for _, p := range parts {
		if p.TargetInstanceID == j.target.ID || j.crossSeed != nil {
			continue
		}
		if j.svc.deps.CrossSeeds == nil {
			return permanent("qui cannot add cross-seeds on the Cross-seed instance of %s.", j.target.Name)
		}
		if j.crossSeed, err = j.svc.instance(ctx, p.TargetInstanceID); err != nil {
			return permanent("%v", err)
		}
		if p.Placement.Endpoint.Changed(j.crossSeed) {
			j.away[j.crossSeed.ID] = true
			return permanent("The Cross-seed instance %s changed its address, its SSH server, or its file access since the Transfer started, "+
				"so the Transfer stopped. qui cleans up nothing on %s.", j.crossSeed.Name, j.crossSeed.Name)
		}
	}
	if j.useRecordedNames(parts) {
		return sourceGone("a file was renamed")
	}
	if j.files, j.byRel, err = groupFiles(parts); err != nil {
		return permanent("The torrents of the group do not agree on their files: %v", err)
	}
	j.parts = parts
	j.findHardlinks(ctx)
	return nil
}

func (j *job) checkSource(ctx context.Context) error {
	torrents, err := j.checkedSource(ctx)
	if err != nil {
		return err
	}
	for _, p := range j.parts {
		i := slices.IndexFunc(torrents, func(t *qbt.Torrent) bool { return strings.EqualFold(t.Hash, p.Hash) })
		if i < 0 {
			return sourceGone("it left the source")
		}
		torrent := torrents[i]
		switch {
		case torrent.SavePath != p.src.torrent.SavePath || torrent.State == qbt.TorrentStateMoving:
			return sourceGone("it moved")
		case torrent.Name != p.src.torrent.Name:
			return sourceChanged("it was renamed")
		case torrent.Progress < 1:
			return sourceChanged("qBittorrent checked it and found pieces that are missing")
		}
		files, err := j.svc.deps.Sync.GetTorrentFiles(qbittorrent.WithForceFilesRefresh(ctx), j.source.ID, p.Hash)
		if err != nil {
			return temporary("qui cannot read the files of the torrent on %s: %v", j.source.Name, err)
		}
		if files == nil {
			return sourceGone("it left the source")
		}
		plan, err := planFiles(p.src.info, *files)
		if err != nil || !slices.Equal(names(plan), names(p.plan)) {
			return sourceGone("a file was renamed")
		}
	}
	return nil
}

func (j *job) checkedSource(ctx context.Context) ([]*qbt.Torrent, error) {
	for poll := range j.svc.deps.Config.CheckPolls {
		if err := j.pause(ctx, poll); err != nil {
			return nil, err
		}
		torrents, err := j.svc.deps.Sync.GetTorrentRefsFresh(ctx, j.source.ID, qbittorrent.TorrentRefFilter{Hashes: j.hashes()})
		if err != nil {
			return nil, temporary("qui cannot read the torrents on %s: %v", j.source.Name, err)
		}
		if !slices.ContainsFunc(torrents, func(t *qbt.Torrent) bool { return isChecking(t.State) }) {
			return torrents, nil
		}
		if poll == 0 {
			j.logf("qBittorrent checks the torrent on %s. qui waits for the check to end.", j.source.Name)
		}
	}
	return nil, sourceChanged("the check of qBittorrent did not end")
}

func isChecking(state qbt.TorrentState) bool {
	return slices.Contains([]qbt.TorrentState{qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingResumeData}, state)
}

const retryAfter = time.Hour

func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
}

func (j *job) rollBack(ctx context.Context) {
	ctx, cancel := cleanupContext(ctx)
	defer cancel()
	defer j.release()
	if j.rec.Kind == KindTransfer && (!j.reached(stepPrepare) || j.changedNothing) {
		j.logf("Cancelled.")
		_ = j.finish(ctx, StateCancelled)
		return
	}
	_ = j.checkInstances(ctx)
	j.logf("Cancelled.")
	_ = j.cleanUp(ctx, StateCancelled)
}

func (j *job) cleanUpAgain(ctx context.Context) {
	ctx, cancel := cleanupContext(ctx)
	defer cancel()
	defer j.release()
	_ = j.checkInstances(ctx)
	j.cleanup(ctx)
	if err := j.finish(ctx, j.rec.State); err == nil && j.rec.State == StateFailed {
		j.notify(ctx, notifications.EventTransferFailed)
	}
}

func (j *job) end(ctx context.Context, runErr error) {
	ctx, cancel := cleanupContext(ctx)
	defer cancel()
	defer j.release()

	if runErr == nil {
		j.logf("Done.")
		if err := j.finish(ctx, StateDone); err == nil {
			j.notify(ctx, notifications.EventTransferFinished)
		}
		return
	}
	class, reason := FailureTemporary, runErr.Error()
	if f, ok := errors.AsType[*failure](runErr); ok {
		class = f.class
	} else if r, ok := errors.AsType[*RefusalError](runErr); ok {
		class, reason = FailurePermanent, r.Reason
	}
	if off, ok := errors.AsType[*instanceOffError](runErr); ok {
		j.rec.State = StateWaiting
		j.logf("Waits until %s is turned on again.", off.name)
		if j.stoppedSource {
			j.startSource(ctx)
		}
		j.save(ctx)
		return
	}
	j.rec.FailureClass, j.rec.FailureReason = class, reason
	if class == FailurePermanent {
		j.logf("Failed: %s", reason)
		if err := j.cleanUp(ctx, StateFailed); err == nil {
			j.notify(ctx, notifications.EventTransferFailed)
		}
		return
	}

	j.rec.State = StateFailed
	j.rec.RetryAt = j.svc.deps.Now().Add(retryAfter)
	j.logf("Failed: %s qui tries again in an hour, and keeps what it copied.", reason)
	if err := j.removeHelpers(ctx); err != nil {
		j.logf("qui could not remove its helper torrents: %v", err)
	}
	if j.stoppedSource {
		j.startSource(ctx)
	}
	j.save(ctx)
	j.notify(ctx, notifications.EventTransferFailed)
}

func (j *job) notify(ctx context.Context, eventType notifications.EventType) {
	if j.svc.deps.Notifier == nil {
		return
	}
	first := j.rec.Members[0]
	lines := []string{"Torrent: " + first.Name}
	if len(j.rec.Members) > 1 {
		lines = append(lines, fmt.Sprintf("Torrents: %d", len(j.rec.Members)))
	}
	target := fmt.Sprintf("Instance %d", j.rec.TargetInstanceID)
	if j.target != nil {
		target = j.target.Name
	}
	lines = append(lines, "Target: "+target)
	switch {
	case eventType != notifications.EventTransferFailed:
	case j.rec.FailureClass == FailureTemporary:
		lines = append(lines, "Error: "+j.rec.FailureReason+" qui tries again in an hour.")
	default:
		lines = append(lines, "Error: "+j.rec.FailureReason)
	}
	event := notifications.Event{
		Type:        eventType,
		InstanceID:  j.rec.SourceInstanceID,
		TorrentName: first.Name,
		TorrentHash: first.Hash,
		Message:     strings.Join(lines, "\n"),
	}
	if j.source != nil {
		event.InstanceName = j.source.Name
	}
	j.svc.deps.Notifier.Notify(ctx, event)
}

func (j *job) save(ctx context.Context) {
	if err := j.svc.deps.Store.Save(ctx, j.rec); err != nil {
		log.Error().Err(err).Int64("transfer", j.rec.ID).Msg("transfer: cannot save the Transfer")
	}
}

func (j *job) cleanUp(ctx context.Context, state State) error {
	j.rec.State = state
	j.rec.Steps = append(j.rec.Steps, Step{Name: stepCleanUp, At: j.svc.deps.Now()})
	j.save(ctx)
	j.cleanup(ctx)
	return j.finish(ctx, state)
}

func (j *job) finish(ctx context.Context, state State) error {
	j.rec.State = state
	j.rec.Steps = append(j.rec.Steps, Step{Name: string(state), At: j.svc.deps.Now()})
	j.save(ctx)
	if err := j.svc.deps.Store.Finish(ctx, j.rec.ID, state); err != nil {
		log.Error().Err(err).Int64("transfer", j.rec.ID).Msg("transfer: cannot finish the Transfer")
		return err
	}
	return nil
}

func (j *job) release() {
	if j.root != nil {
		_ = j.root.Close()
	}
	if j.reserved > 0 {
		j.svc.unreserve(j.target.ID, j.reserved)
	}
}

func (j *job) startSource(ctx context.Context) {
	if hashes := j.stoppedByQui(); len(hashes) > 0 {
		if err := j.svc.deps.Sync.BulkAction(ctx, j.source.ID, hashes, "resume"); err != nil {
			j.logf("qui could not start %s on %s again: %v", j.torrents("the"), j.source.Name, err)
			return
		}
	}
	j.stoppedSource = false
}

func (j *job) remove(ctx context.Context, names ...string) {
	for _, name := range names {
		if err := j.root.Remove(ctx, name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			j.logf("qui could not remove %s on %s: %v", j.targetPath(name), j.target.Name, err)
		}
	}
}

func (j *job) targetPath(name string) string {
	return j.rootPaths.Join(j.rootDir, j.rootPaths.FromSlash(name))
}

func (j *job) actor() qbittorrent.DeleteActor {
	if j.rec.StartedBy == StartedByRule {
		return qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorAutomation}
	}
	return qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorUser}
}
