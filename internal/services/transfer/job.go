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

// failure ends a Transfer with a reason for the user. Its format may wrap
// an error with %w.
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

// sourceGone is a change of the source after which no retry finds the files
// where the Transfer reads them.
func sourceGone(what string) error {
	return permanent("The torrent changed on the source during the copy: %s.", what)
}

// job is one run of a Transfer of a torrent and its Shared-file group.
type job struct {
	svc            *Service
	rec            *Record
	source, target *models.Instance
	// crossSeed is the Cross-seed instance of the target when a member
	// lands there.
	crossSeed *models.Instance
	// parts holds the members in the order of the record, the torrent of the
	// request first.
	parts []*part
	// files holds each file of the group once, and byRel finds one by its
	// path below the root.
	files []*groupFile
	byRel map[string]*groupFile
	// srcFiles is nil on a source, and blind is set on a target, of a
	// Transfer torrent whose files qui cannot reach.
	srcFiles fsops.Backend
	blind    bool
	dstFiles fsops.Backend
	root     fsops.Root
	// rootDir is the folder on the target that holds every member, in the
	// path dialect rootPaths.
	rootDir   string
	rootPaths fsops.PathDialect
	reserved  int64
	// helper is the infohash of a helper torrent on the target that qui has
	// not removed yet.
	helper         string
	stoppedSource  bool
	changedNothing bool
	// made holds the temporary files on the target, with their size when the
	// run found them.
	made map[*groupFile]int64
	// unanswered holds the names on the target where a create or a link of
	// this run lost its answer, so the target may have made them.
	unanswered map[string]bool
	// away holds the instances that were deleted or changed their endpoint.
	away map[int]bool
	// flushedPieces counts the pieces that qui wrote and flushed in this run,
	// also on a target that does not confirm a flush.
	flushedPieces int
	// meter counts the bytes that reached the target.
	meter *meter
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

// run takes the steps that the Transfer did not finish. Once qui placed
// the files, the copy and its proof are done.
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
		// A Transfer torrent that fell back leaves the rest to the copy.
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

// step records a step and the state before qui takes the step.
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

// prepare reads both sides again, checks the free space, and opens the root
// on the target with no file at any path that the copy writes.
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
		// pass reserves what each Transfer torrent still needs after its check.
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

// space is a read of the free space on the target. freed is what the runs
// gave back on the target before the read.
type space struct {
	free, freed int64
	known       bool
}

// freeSpace is not known on a blind target before qBittorrent 5.3, which
// reports it only at its default save path.
func (j *job) freeSpace(ctx context.Context) (space, error) {
	read := space{freed: j.svc.freedOn(j.target.ID), known: true}
	if !j.blind {
		stat, err := j.dstFiles.Statfs(ctx, j.rootDir)
		if err != nil {
			return space{}, temporary("qui cannot read the free space on %s: %w", j.target.Name, targetError{err})
		}
		read.free = stat.BytesAvailable
		return read, nil
	}
	free, known, err := j.svc.clientFreeSpace(ctx, j.target, j.rootPaths, j.rootDir)
	if err != nil || !known {
		return space{}, err
	}
	read.free = free
	return read, nil
}

// clientFreeSpace reads the free space at dir on target as its qBittorrent
// reports it. Before 5.3 qBittorrent reports it only at its default save
// path, so elsewhere it is not known.
func (s *Service) clientFreeSpace(ctx context.Context, target *models.Instance, d fsops.PathDialect, dir string) (free int64, known bool, err error) {
	free, err = s.deps.Sync.GetFreeSpaceAtPath(ctx, target.ID, dir)
	if !errors.Is(err, qbittorrent.ErrFreeSpaceAtPathUnsupported) {
		if err != nil {
			return 0, false, temporary("qui cannot read the free space on %s: %v", target.Name, err)
		}
		return free, true, nil
	}
	prefs, err := s.deps.Sync.GetAppPreferences(ctx, target.ID)
	if err != nil {
		return 0, false, temporary("qui cannot read the default save path of %s: %v", target.Name, err)
	}
	if d.Clean(prefs.SavePath) != dir {
		return 0, false, nil
	}
	if free, err = s.deps.Sync.GetFreeSpace(ctx, target.ID); err != nil {
		return 0, false, temporary("qui cannot read the free space on %s: %v", target.Name, err)
	}
	return free, true, nil
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

// onTCPAndUTP returns the instances whose qBittorrent uses "TCP and μTP".
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

// bittorrentTCPAndUTP is "TCP and μTP" in qBittorrent's "Peer connection
// protocol".
const bittorrentTCPAndUTP = 0

// need is how many bytes the copy writes on the target: each file that the
// target lacks, once.
func (j *job) need() int64 {
	var need int64
	for _, f := range j.files {
		size, made := j.made[f]
		switch {
		case f.onTarget || f.linked || f.linksCopy || j.linksCopyLater(f):
		case made:
			need += f.size - size
		default:
			need += f.size
		}
	}
	return need
}

// reserve reserves bytes of the free space on the target for the run. It
// reserves nothing, and reports false, when the free space is not known.
func (j *job) reserve(ctx context.Context, bytes int64) (bool, error) {
	read, err := j.freeSpace(ctx)
	if err != nil || !read.known {
		return read.known, err
	}
	return true, j.claim(bytes, read)
}

// claim reserves bytes of the free space that qui read on the target. A disk
// without the bytes fails the Transfer. When only the other Transfers to the
// target stand in the way, with what they reserved or wrote since the read,
// the Transfer fails for a retry, because the shortage ends with them.
func (j *job) claim(bytes int64, read space) error {
	left := read.free - j.reserved
	if left < bytes {
		return permanent("Not enough free space on %s: not enough free space on the target: the Transfer needs %s, and %s is free",
			j.target.Name, formatBytes(bytes), formatBytes(max(left, 0)))
	}
	if others, ok := j.svc.reserve(j.target.ID, bytes, read, j.reserved); !ok {
		return temporary("Not enough free space on %s for now: the Transfer needs %s, and %s is free, but other Transfers to %s take %s of it.",
			j.target.Name, formatBytes(bytes), formatBytes(left), j.target.Name, formatBytes(others))
	}
	j.reserved += bytes
	return nil
}

// unclaim gives back bytes that the run reserved and wrote: the free space
// on the target shows them now.
func (j *job) unclaim(bytes int64) {
	j.svc.unreserve(j.target.ID, bytes)
	j.reserved -= bytes
}

// readParts reads each member on the source again, and places it below the
// root of the Transfer on the target. It sets the parts only with the files
// of the whole group, so that a cleanup after a failure reads them again.
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

// checkSource fails when a torrent of the Transfer moves or moved on the
// source, was renamed, is rechecked, or has other files than at the start of
// the copy.
func (j *job) checkSource(ctx context.Context) error {
	torrents, err := j.svc.deps.Sync.GetTorrentRefsFresh(ctx, j.source.ID, qbittorrent.TorrentRefFilter{Hashes: j.hashes()})
	if err != nil {
		return temporary("qui cannot read the torrents on %s: %v", j.source.Name, err)
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
		case isChecking(torrent.State) || torrent.Progress < 1:
			return sourceChanged("qBittorrent checks it again")
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

func isChecking(state qbt.TorrentState) bool {
	return slices.Contains([]qbt.TorrentState{qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingResumeData}, state)
}

// retryAfter is the least time between a temporary failure and its retry.
const retryAfter = time.Hour

// cleanupContext outlives the run, so that a stop does not cut a cleanup
// short. A shutdown waits for it only until its own limit; the cleanup step
// is recorded first, so the next start finishes a cleanup that the exit cut.
func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
}

// rollBack ends a cancelled Transfer: it takes back what the Transfer did.
func (j *job) rollBack(ctx context.Context) {
	ctx, cancel := cleanupContext(ctx)
	defer cancel()
	defer j.release()
	// A Transfer records the prepare step before it changes anything. One
	// that never got there has nothing to take back, so its end asks the
	// instances nothing.
	if j.rec.Kind == KindTransfer && (!j.reached(stepPrepare) || j.changedNothing) {
		j.logf("Cancelled.")
		_ = j.finish(ctx, StateCancelled)
		return
	}
	// The check marks the instances that the cleanup must not touch.
	_ = j.checkInstances(ctx)
	j.logf("Cancelled.")
	_ = j.cleanUp(ctx, StateCancelled)
}

// cleanUpAgain finishes a cleanup that a stop interrupted, and sends the
// failure that the stop held back.
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

// end records how the run ended. A temporary failure keeps the partial data
// and the claims until its retry. A permanent failure takes back what the
// Transfer did, and waits for the user.
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

// notify sends the end of the Transfer as an event of the source instance.
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

// cleanUp records the step clean up, takes back what the Transfer did, and
// finishes it in state. It returns the error of finish.
func (j *job) cleanUp(ctx context.Context, state State) error {
	j.rec.State = state
	j.rec.Steps = append(j.rec.Steps, Step{Name: stepCleanUp, At: j.svc.deps.Now()})
	j.save(ctx)
	j.cleanup(ctx)
	return j.finish(ctx, state)
}

// finish records the end of the Transfer and releases its torrents. On an
// error the Transfer stays unfinished, and the next start of qui ends it
// again, so the caller sends the event of the end only on success.
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

// release frees what the run holds, and keeps the files.
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

// remove removes the files that exist.
func (j *job) remove(ctx context.Context, names ...string) {
	for _, name := range names {
		if err := j.root.Remove(ctx, name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			j.logf("qui could not remove %s on %s: %v", j.targetPath(name), j.target.Name, err)
		}
	}
}

// targetPath is the path on the target of a slash name below the root.
func (j *job) targetPath(name string) string {
	return j.rootPaths.Join(j.rootDir, j.rootPaths.FromSlash(name))
}

func (j *job) actor() qbittorrent.DeleteActor {
	if j.rec.StartedBy == StartedByRule {
		return qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorAutomation}
	}
	return qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorUser}
}
