// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// failure ends a Transfer with a reason for the user.
type failure struct {
	class  FailureClass
	reason string
}

func (f *failure) Error() string { return f.reason }

func permanent(format string, args ...any) error {
	return &failure{class: FailurePermanent, reason: fmt.Sprintf(format, args...)}
}

func temporary(format string, args ...any) error {
	return &failure{class: FailureTemporary, reason: fmt.Sprintf(format, args...)}
}

func sourceChanged(what string) error {
	return temporary("The torrent changed on the source during the copy: %s.", what)
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
	// added is set once qui asked the target to add the real torrent. From
	// then on qui keeps the files on the target.
	added bool
	// placed lists the files that qui renamed into place.
	placed        []string
	stoppedSource bool
}

func (j *job) member() *Member { return &j.rec.Members[0] }

func (j *job) hash() string { return j.member().Hash }

func (j *job) savePath() string { return j.member().Placement.SavePath }

func (j *job) tempName(name string) string {
	return fmt.Sprintf("%s.qui-transfer-%d", name, j.rec.ID)
}

func (j *job) run(ctx context.Context) error {
	if err := j.prepare(ctx); err != nil {
		return err
	}
	if err := j.copy(ctx); err != nil {
		return err
	}
	if err := j.prove(ctx); err != nil {
		return err
	}
	if err := j.place(ctx); err != nil {
		return err
	}
	return j.after(ctx)
}

// step records a step and the state before qui takes the step.
func (j *job) step(ctx context.Context, state State, name string) error {
	j.rec.State = state
	j.rec.Steps = append(j.rec.Steps, Step{Name: name, At: j.svc.deps.Now()})
	return j.svc.deps.Store.Save(ctx, j.rec)
}

func (j *job) logf(format string, args ...any) {
	j.rec.Log = append(j.rec.Log, LogLine{At: j.svc.deps.Now(), Message: fmt.Sprintf(format, args...)})
}

// prepare reads both sides again, checks the free space, and opens the save
// path on the target with no file at any path that the copy writes.
func (j *job) prepare(ctx context.Context) error {
	if err := j.step(ctx, StateCopying, "prepare"); err != nil {
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
	if j.rec.SourceEndpoint.Changed(j.source) || j.rec.TargetEndpoint.Changed(j.target) {
		return permanent("An instance of the Transfer changed its address, its SSH server, or its file access.")
	}
	opener, ok := targetFiles.(fsops.RootOpener)
	if !ok {
		return permanent("qui cannot write new files on %s.", j.target.Name)
	}

	if j.src, err = j.svc.readSource(ctx, j.source.ID, j.hash()); err != nil {
		return err
	}
	if j.src == nil {
		return sourceChanged("it left the source")
	}
	if err := j.src.movable(); err != nil {
		return permanent("%v", err)
	}
	if j.src.torrent.SavePath != j.member().SourceSavePath {
		return sourceChanged("it moved")
	}
	if j.plan, err = planFiles(j.src.info, j.src.files); err != nil {
		return permanent("The files of the torrent do not match its .torrent: %v", err)
	}

	need := diskBytes(j.plan)
	stat, err := targetFiles.Statfs(ctx, nearestFolder(ctx, targetFiles, j.savePath()))
	if err != nil {
		return temporary("qui cannot read the free space on %s: %v", j.target.Name, err)
	}
	if err := j.svc.reserve(j.target.ID, need, stat.BytesAvailable); err != nil {
		return permanent("Not enough free space on %s: %v", j.target.Name, err)
	}
	j.reserved = need

	if j.root, err = opener.OpenRoot(ctx, j.savePath()); err != nil {
		return permanent("qui cannot open the save path %s on %s: %v", j.savePath(), j.target.Name, err)
	}
	for _, name := range names(j.plan) {
		for _, candidate := range []string{name, j.tempName(name), j.tempName(name) + incompleteExt} {
			if _, err := j.root.Lstat(ctx, candidate); !errors.Is(err, fs.ErrNotExist) {
				return permanent("Conflict: a file is in the way on %s at %s.", j.target.Name, path.Join(j.savePath(), candidate))
			}
		}
	}
	j.logf("Copying %s over SFTP from %s to %s.", formatBytes(need), j.source.Name, j.target.Name)
	return nil
}

// nearestFolder is path or its closest parent that exists.
func nearestFolder(ctx context.Context, files fsops.Backend, dir string) string {
	paths := files.Paths()
	for {
		if _, err := files.Lstat(ctx, dir); err == nil {
			return dir
		}
		parent := paths.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
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
		return sourceChanged("it left the source")
	case torrent.SavePath != j.src.torrent.SavePath:
		return sourceChanged("it moved")
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
		return sourceChanged("it left the source")
	}
	plan, err := planFiles(j.src.info, *files)
	if err != nil || !slices.Equal(names(plan), names(j.plan)) {
		return sourceChanged("a file was renamed")
	}
	return nil
}

func isChecking(state qbt.TorrentState) bool {
	return slices.Contains([]qbt.TorrentState{qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingResumeData}, state)
}

// end finishes the Transfer as done, or as failed after the cleanup.
func (j *job) end(runErr error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(j.svc.ctx), 5*time.Minute)
	defer cancel()
	defer j.release()

	state := StateDone
	if runErr != nil {
		state = StateFailed
		if f, ok := errors.AsType[*failure](runErr); ok {
			j.rec.FailureClass, j.rec.FailureReason = f.class, f.reason
		} else if r, ok := errors.AsType[*RefusalError](runErr); ok {
			j.rec.FailureClass, j.rec.FailureReason = FailurePermanent, r.Reason
		} else {
			j.rec.FailureClass, j.rec.FailureReason = FailureTemporary, runErr.Error()
		}
		j.logf("Failed: %s", j.rec.FailureReason)
		j.cleanup(ctx)
	} else {
		j.logf("Done.")
	}
	j.rec.State = state
	j.rec.Steps = append(j.rec.Steps, Step{Name: string(state), At: j.svc.deps.Now()})
	if err := j.svc.deps.Store.Save(ctx, j.rec); err != nil {
		log.Error().Err(err).Int64("transfer", j.rec.ID).Msg("transfer: cannot save the end of the Transfer")
	}
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

// cleanup removes the helper torrent and the files that qui wrote, and
// starts the source again when qui stopped it. Once the target has the real
// torrent, qui keeps its files.
func (j *job) cleanup(ctx context.Context) {
	if j.helper != "" {
		if err := j.removeHelper(ctx); err != nil {
			j.logf("qui could not remove its helper torrent %s from %s: %v", j.helper, j.target.Name, err)
		}
	}
	if j.stoppedSource {
		if err := j.svc.deps.Sync.BulkAction(ctx, j.source.ID, []string{j.hash()}, "resume"); err != nil {
			j.logf("qui could not start the torrent on %s again: %v", j.source.Name, err)
		}
	}
	if j.root == nil {
		return
	}
	folders := map[string]struct{}{}
	for _, name := range names(j.plan) {
		removed := j.remove(ctx, j.tempName(name), j.tempName(name)+incompleteExt)
		if !j.added && slices.Contains(j.placed, name) {
			removed = j.remove(ctx, name) || removed
		}
		for dir := path.Dir(name); removed && dir != "."; dir = path.Dir(dir) {
			folders[dir] = struct{}{}
		}
	}
	// The deepest folders go first, so that a parent is empty when its turn
	// comes. A folder that holds other files stays.
	for _, dir := range slices.SortedFunc(maps.Keys(folders), func(a, b string) int { return strings.Count(b, "/") - strings.Count(a, "/") }) {
		_ = j.root.Remove(ctx, dir)
	}
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
