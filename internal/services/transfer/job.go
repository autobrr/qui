// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
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

// job is one run of a Transfer of one torrent.
type job struct {
	svc            *Service
	rec            *Record
	source, target *models.Instance
	src            *source
	plan           []file
	srcFiles       fsops.Backend
	root           fsops.Root
	reserved       int64
	proven         []byte
	// helper is the infohash of a helper torrent on the target that qui has
	// not removed yet.
	helper string
	stoppedSource bool
	// headStartProven is set when the check of the files that were on the
	// target passed every piece.
	headStartProven bool
	// made holds the temporary files on the target by the index of their
	// file, with their size when the run found them.
	made map[int]int64
	// away holds the instances that were deleted or changed their endpoint.
	away map[int]bool
}

func (j *job) member() *Member { return &j.rec.Members[0] }

func (j *job) hash() string { return j.member().Hash }

func (j *job) savePath() string { return j.member().Placement.SavePath }

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
		if err := j.copyAll(ctx); err != nil {
			return err
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

// prepare reads both sides again, checks the free space, and opens the save
// path on the target with no file at any path that the copy writes.
func (j *job) prepare(ctx context.Context) error {
	resumed := j.reached(stepPrepare)
	j.rec.RetryAt, j.rec.FailureClass, j.rec.FailureReason = time.Time{}, "", ""
	if err := j.step(ctx, StateCopying, stepPrepare); err != nil {
		return err
	}
	var err error
	if j.srcFiles, j.source, err = j.svc.deps.Files.Require(ctx, j.rec.SourceInstanceID, models.CapabilityReadBytes); err != nil {
		return temporary("qui cannot read the files of the Transfer source: %v", err)
	}
	targetFiles, target, err := j.svc.deps.Files.Require(ctx, j.rec.TargetInstanceID, models.CapabilityWriteBytes)
	if err != nil {
		return temporary("qui cannot write the files of the Transfer target: %v", err)
	}
	j.target = target
	opener, ok := targetFiles.(fsops.RootOpener)
	if !ok {
		return permanent("qui cannot write new files on %s.", j.target.Name)
	}

	if j.src, err = j.svc.readSource(ctx, j.source.ID, j.hash()); err != nil {
		return err
	}
	if j.src == nil {
		return sourceGone("it left the source")
	}
	if err := j.src.movable(); err != nil {
		return permanent("%v", err)
	}
	if j.src.torrent.SavePath != j.member().SourceSavePath {
		return sourceGone("it moved")
	}
	if j.plan, err = planFiles(j.src.info, j.src.files); err != nil {
		return permanent("The files of the torrent do not match its .torrent: %v", err)
	}

	if j.root, err = opener.OpenRoot(ctx, j.savePath()); err != nil {
		return permanent("qui cannot open the save path %s on %s: %v", j.savePath(), j.target.Name, err)
	}
	find := j.findOnTarget
	if resumed {
		find = j.findOnTargetAgain
	}
	if err := find(ctx); err != nil {
		return err
	}

	need := copiedBytes(j.plan)
	for _, size := range j.made {
		need -= size
	}
	if j.reached(stepPlace) {
		need = 0
	}
	stat, err := targetFiles.Statfs(ctx, j.savePath())
	if err != nil {
		return temporary("qui cannot read the free space on %s: %v", j.target.Name, err)
	}
	if err := j.svc.reserve(j.target.ID, need, stat.BytesAvailable); err != nil {
		return permanent("Not enough free space on %s: %v", j.target.Name, err)
	}
	j.reserved = need
	if !resumed {
		j.logf("Copying %s over SFTP from %s to %s.", formatBytes(need), j.source.Name, j.target.Name)
		return nil
	}
	if err := j.removeHelpers(ctx); err != nil {
		return temporary("qui cannot remove its helper torrents from %s: %v", j.target.Name, err)
	}
	j.logf("Resumed: %d of %d pieces are proven, and %s are left to copy.",
		provenCount(j.member().ProvenPieces), j.src.info.NumPieces(), formatBytes(need))
	return nil
}

// checkSource fails when the torrent on the source moved, was renamed, is
// rechecked, or has other files than at the start of the copy.
func (j *job) checkSource(ctx context.Context) error {
	torrent, err := j.svc.torrent(ctx, j.source.ID, j.hash())
	if err != nil {
		return temporary("qui cannot read the torrent on %s: %v", j.source.Name, err)
	}
	switch {
	case torrent == nil:
		return sourceGone("it left the source")
	case torrent.SavePath != j.src.torrent.SavePath:
		return sourceGone("it moved")
	case torrent.Name != j.src.torrent.Name:
		return sourceChanged("it was renamed")
	case isChecking(torrent.State) || torrent.Progress < 1:
		return sourceChanged("qBittorrent checks it again")
	}
	files, err := j.svc.deps.Sync.GetTorrentFiles(qbittorrent.WithForceFilesRefresh(ctx), j.source.ID, j.hash())
	if err != nil {
		return temporary("qui cannot read the files of the torrent on %s: %v", j.source.Name, err)
	}
	if files == nil {
		return sourceGone("it left the source")
	}
	plan, err := planFiles(j.src.info, *files)
	if err != nil || !slices.Equal(names(plan), names(j.plan)) {
		return sourceGone("a file was renamed")
	}
	return nil
}

func isChecking(state qbt.TorrentState) bool {
	return slices.Contains([]qbt.TorrentState{qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingResumeData}, state)
}

// retryAfter is the least time between a temporary failure and its retry.
const retryAfter = time.Hour

func (j *job) cleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(j.svc.ctx), 5*time.Minute)
}

// rollBack ends a cancelled Transfer: it takes back what the Transfer did.
func (j *job) rollBack() {
	ctx, cancel := j.cleanupContext()
	defer cancel()
	defer j.release()
	// The check marks the instances that the cleanup must not touch.
	_ = j.checkInstances(ctx)
	j.logf("Cancelled.")
	j.cleanUp(ctx, StateCancelled)
}

// cleanUpAgain finishes a cleanup that a stop interrupted.
func (j *job) cleanUpAgain() {
	ctx, cancel := j.cleanupContext()
	defer cancel()
	defer j.release()
	_ = j.checkInstances(ctx)
	j.cleanup(ctx)
	j.finish(ctx, j.rec.State)
}

// end records how the run ended. A temporary failure keeps the partial data
// and the claims until its retry. A permanent failure takes back what the
// Transfer did, and waits for the user.
func (j *job) end(runErr error) {
	ctx, cancel := j.cleanupContext()
	defer cancel()
	defer j.release()

	if runErr == nil {
		j.logf("Done.")
		j.finish(ctx, StateDone)
		return
	}
	class, reason := FailureTemporary, runErr.Error()
	if f, ok := errors.AsType[*failure](runErr); ok {
		class = f.class
	} else if r, ok := errors.AsType[*RefusalError](runErr); ok {
		class, reason = FailurePermanent, r.Reason
	}
	if off, ok := errors.AsType[*instanceOff](runErr); ok {
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
		j.cleanUp(ctx, StateFailed)
		return
	}

	j.rec.State = StateFailed
	j.rec.RetryAt = j.svc.deps.Now().Add(retryAfter)
	j.logf("Failed: %s qui tries again in an hour, and keeps what it copied.", reason)
	if j.reaches(j.target) {
		if err := j.removeHelpers(ctx); err != nil {
			j.logf("qui could not remove its helper torrents from %s: %v", j.target.Name, err)
		}
	}
	if j.stoppedSource {
		j.startSource(ctx)
	}
	j.save(ctx)
}

func (j *job) save(ctx context.Context) {
	if err := j.svc.deps.Store.Save(ctx, j.rec); err != nil {
		log.Error().Err(err).Int64("transfer", j.rec.ID).Msg("transfer: cannot save the Transfer")
	}
}

// cleanUp records the step clean up, takes back what the Transfer did, and
// finishes it in state.
func (j *job) cleanUp(ctx context.Context, state State) {
	j.rec.State = state
	j.rec.Steps = append(j.rec.Steps, Step{Name: stepCleanUp, At: j.svc.deps.Now()})
	j.save(ctx)
	j.cleanup(ctx)
	j.finish(ctx, state)
}

// finish records the end of the Transfer and releases its torrents.
func (j *job) finish(ctx context.Context, state State) {
	j.rec.State = state
	j.rec.Steps = append(j.rec.Steps, Step{Name: string(state), At: j.svc.deps.Now()})
	j.save(ctx)
	if err := j.svc.deps.Store.Finish(ctx, j.rec.ID, state); err != nil {
		log.Error().Err(err).Int64("transfer", j.rec.ID).Msg("transfer: cannot finish the Transfer")
	}
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
	if err := j.svc.deps.Sync.BulkAction(ctx, j.source.ID, []string{j.hash()}, "resume"); err != nil {
		j.logf("qui could not start the torrent on %s again: %v", j.source.Name, err)
		return
	}
	j.stoppedSource = false
}

// remove removes the files that exist and reports whether it removed one.
func (j *job) remove(ctx context.Context, names ...string) bool {
	removed := false
	for _, name := range names {
		err := j.root.Remove(ctx, name)
		switch {
		case err == nil:
			removed = true
		case !errors.Is(err, fs.ErrNotExist):
			j.logf("qui could not remove %s on %s: %v", path.Join(j.savePath(), name), j.target.Name, err)
		}
	}
	return removed
}

func (j *job) actor() qbittorrent.DeleteActor {
	if j.rec.StartedBy == StartedByRule {
		return qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorAutomation}
	}
	return qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorUser}
}
