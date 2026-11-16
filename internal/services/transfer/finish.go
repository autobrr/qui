// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/helpertorrent"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/torrentadd"
)

// prove proves the copy before qui touches the source: by its own check of
// every piece with a confirmed flush, or else by a check-only helper torrent
// that checks every piece on the target. qui's own check never covers the
// files that were on the target before the Transfer.
func (j *job) prove(ctx context.Context) error {
	onTarget := slices.ContainsFunc(j.plan, func(f file) bool { return f.onTarget })
	switch {
	case j.headStartProven:
		j.logf("Proof: every file was on %s, and the check passed all %d pieces.", j.target.Name, j.src.info.NumPieces())
		return nil
	case !onTarget && j.rec.FlushState == FlushConfirmed && j.allProven():
		j.logf("Proof: qui checked every piece, and %s confirmed each flush.", j.target.Name)
		return nil
	}
	if err := j.step(ctx, StateChecking, "check on the target"); err != nil {
		return err
	}
	states, err := j.checkOnTarget(ctx)
	if err != nil {
		return err
	}
	var failed []string
	for piece, state := range states {
		if state != qbt.PieceStateAlreadyDownloaded {
			failed = append(failed, strconv.Itoa(piece))
		}
	}
	if len(failed) > 0 {
		shown := failed[:min(len(failed), 20)]
		more := ""
		if len(failed) > len(shown) {
			more = fmt.Sprintf(" and %d more", len(failed)-len(shown))
		}
		return permanent("The check on %s failed %d of %d pieces: %s%s.", j.target.Name, len(failed), len(states), strings.Join(shown, ", "), more)
	}
	j.logf("Proof: the check on %s passed all %d pieces.", j.target.Name, len(states))
	return nil
}

// checkOnTarget checks every piece on the target with a check-only helper
// torrent and returns what the check found. The helper finds each file that
// qui writes under its temporary name, and each file that was on the target
// through a hardlink, so that qBittorrent renames no file that qui did not
// write. The links are gone when it returns.
func (j *job) checkOnTarget(ctx context.Context) ([]qbt.PieceState, error) {
	defer j.unlink(context.WithoutCancel(ctx))
	if err := j.link(ctx); err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	random := hex.EncodeToString(salt)
	checkNames := make([]string, 0, len(j.plan))
	for _, f := range j.plan {
		if !f.pad {
			checkNames = append(checkNames, j.checkName(f))
		}
	}
	helper, err := helpertorrent.Build(j.src.export, checkNames, random)
	if err != nil {
		return nil, permanent("qui cannot build the helper torrent: %v", err)
	}
	if err := j.svc.deps.Store.RegisterHelper(ctx, Helper{
		InstanceID: j.target.ID, Hash: helper.InfoHash, Random: []byte(random), Owner: OwnerTransfer, OwnerID: j.rec.ID,
	}); err != nil {
		return nil, err
	}
	j.helper = helper.InfoHash
	cfg := j.svc.deps.Config
	res := j.svc.adder.Add(ctx, torrentadd.Request{
		InstanceID: j.target.ID,
		Torrent:    helper.TorrentFile,
		Hash:       helper.InfoHash,
		Settings: torrentadd.Settings{
			SavePath:             j.savePath(),
			ContentLayout:        qbt.ContentLayout(helper.ContentLayout),
			Stopped:              new(false),
			Forced:               true,
			AutoTMM:              new(false),
			UseDownloadPath:      new(false),
			StopWhenFilesChecked: true,
		},
		Polls:        cfg.CheckPolls,
		PollInterval: cfg.PollInterval,
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if res.Outcome == torrentadd.Failed || res.Outcome == torrentadd.Checking {
		return nil, temporary("The check on %s did not end: %s", j.target.Name, describe(res))
	}
	states, err := j.svc.deps.Sync.GetTorrentPieceStates(ctx, j.target.ID, helper.InfoHash)
	if err != nil {
		return nil, temporary("qui cannot read the check on %s: %v", j.target.Name, err)
	}
	if err := j.removeHelper(ctx); err != nil {
		return nil, temporary("qui cannot remove its helper torrent from %s: %v", j.target.Name, err)
	}
	if len(states) != j.src.info.NumPieces() {
		return nil, temporary("The check on %s reported %d pieces of %d.", j.target.Name, len(states), j.src.info.NumPieces())
	}
	return states, nil
}

func describe(res torrentadd.Result) string {
	switch {
	case res.Err != nil:
		return res.Err.Error()
	case res.Torrent != nil:
		return "qBittorrent shows " + string(res.Torrent.State)
	}
	return "qBittorrent does not list the torrent"
}

func (j *job) removeHelper(ctx context.Context) error {
	if err := j.svc.deps.Sync.DeleteTorrents(ctx, j.target.ID, []string{j.helper}, false, j.actor()); err != nil {
		return err
	}
	j.helper = ""
	return nil
}

// place renames the copy into place and adds the real torrent on the
// target, stopped and in seed mode, with the file names of the source.
func (j *job) place(ctx context.Context) error {
	again := j.reached(stepPlace)
	if err := j.stepWith(ctx, StateFinishing, stepPlace, headStartDetail(j.plan)); err != nil {
		return err
	}
	owned := j.reached(stepAdded)
	if !owned {
		onTarget, err := j.svc.has(ctx, j.target.ID, j.hash())
		switch {
		case err != nil:
			return temporary("qui cannot read the torrents of %s: %v", j.target.Name, err)
		case onTarget && j.reached(stepAdd):
			return j.ownershipUnknown()
		case onTarget:
			return permanent("Already on the target: %s has this torrent.", j.target.Name)
		}
	}
	for _, f := range j.plan {
		if f.pad || f.onTarget {
			continue
		}
		if err := j.placeFile(ctx, f.name, again); err != nil {
			return err
		}
	}

	if !owned {
		if err := j.add(ctx); err != nil {
			return err
		}
	}
	return j.rename(ctx)
}

// add adds the real torrent on the target. qui records that the target did
// not have it before the add, and that the target took it after the answer.
// Without an answer, a torrent on the target may be qui's or the user's.
func (j *job) add(ctx context.Context) error {
	if err := j.stepWith(ctx, StateFinishing, stepAdd, j.target.Name+" did not have the torrent."); err != nil {
		return err
	}
	placement := j.member().Placement
	layout := qbt.ContentLayoutOriginal
	if j.src.info.IsDir() && commonRoot(names(j.plan)) == "" {
		layout = qbt.ContentLayoutSubfolderNone
	}
	cfg := j.svc.deps.Config
	res := j.svc.adder.Add(ctx, torrentadd.Request{
		InstanceID: j.target.ID,
		Torrent:    j.src.export,
		Hash:       j.hash(),
		Settings: torrentadd.Settings{
			SavePath:        j.savePath(),
			Category:        placement.Category,
			Tags:            placement.Tags,
			ContentLayout:   layout,
			Stopped:         new(true),
			AutoTMM:         new(false),
			UseDownloadPath: new(false),
			SeedMode:        true,
		},
		Polls:        cfg.Polls,
		PollInterval: cfg.PollInterval,
	})
	if res.Failure == torrentadd.AddRefused {
		if onTarget, err := j.svc.has(ctx, j.target.ID, j.hash()); err == nil && onTarget {
			return j.ownershipUnknown()
		}
		return temporary("%s did not take the torrent: %s", j.target.Name, describe(res))
	}
	if err := j.step(ctx, StateFinishing, stepAdded); err != nil {
		return err
	}
	if res.Outcome != torrentadd.Verified {
		return temporary("%s did not take the torrent: %s", j.target.Name, describe(res))
	}
	return nil
}

// ownershipUnknown stops the Transfer when the target has the torrent after
// an add that did not answer. qui keeps the torrent and its files there, and
// leaves the source as it is.
func (j *job) ownershipUnknown() error {
	return permanent("Ownership unknown: %s has the torrent, but its add did not answer, so qui cannot tell who added it. "+
		"qui keeps the torrent and its files on %s, and the torrent on %s.", j.target.Name, j.target.Name, j.source.Name)
}

// placeFile renames the temporary file of a file into place. When the
// Transfer resumes after the step place, the file can be in place already.
func (j *job) placeFile(ctx context.Context, name string, again bool) error {
	temp := j.tempName(name)
	_, err := j.root.Lstat(ctx, temp)
	if errors.Is(err, fs.ErrNotExist) {
		temp += incompleteExt
		_, err = j.root.Lstat(ctx, temp)
	}
	if errors.Is(err, fs.ErrNotExist) {
		if _, err := j.root.Lstat(ctx, name); again && err == nil {
			return nil
		}
		return permanent("The copy of %s is missing on %s.", name, j.target.Name)
	}
	err = j.root.RenameNoReplace(ctx, temp, name)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrExist) && again && j.sameFile(ctx, temp, name):
		// A stop came between the link and the remove of the rename.
		if err := j.root.Remove(ctx, temp); err != nil {
			return temporary("qui cannot remove %s on %s: %v", temp, j.target.Name, err)
		}
		return nil
	case errors.Is(err, fs.ErrExist):
		return permanent("Conflict: a file is in the way on %s at %s.", j.target.Name, name)
	}
	return temporary("qui cannot put %s in place on %s: %v", name, j.target.Name, err)
}

func (j *job) sameFile(ctx context.Context, a, b string) bool {
	ai, err := j.root.Lstat(ctx, a)
	if err != nil || ai.FileIDErr != nil || ai.FileID.IsZero() {
		return false
	}
	bi, err := j.root.Lstat(ctx, b)
	return err == nil && bi.FileIDErr == nil && ai.FileID == bi.FileID
}

func commonRoot(names []string) string {
	root := ""
	for _, name := range names {
		first, _, nested := strings.Cut(name, "/")
		if !nested || (root != "" && first != root) {
			return ""
		}
		root = first
	}
	return root
}

// rename gives each file on the target the name that it has on the source.
func (j *job) rename(ctx context.Context) error {
	ctx = qbittorrent.WithPostAddBulkActionRetry(ctx)
	want := names(j.plan)
	for range 2 {
		files, err := j.svc.deps.Sync.GetTorrentFiles(qbittorrent.WithForceFilesRefresh(ctx), j.target.ID, j.hash())
		if err != nil || files == nil || len(*files) != len(want) {
			return temporary("qui cannot read the files of the torrent on %s: %v", j.target.Name, err)
		}
		listed := slices.SortedFunc(slices.Values(*files), func(a, b qbt.TorrentFile) int { return cmp.Compare(a.Index, b.Index) })
		renamed := false
		for i, f := range listed {
			if f.Name == want[i] {
				continue
			}
			if err := j.svc.deps.Sync.RenameTorrentFile(ctx, j.target.ID, j.hash(), f.Name, want[i]); err != nil {
				return temporary("qui cannot rename %s to %s on %s: %v", f.Name, want[i], j.target.Name, err)
			}
			renamed = true
		}
		if !renamed {
			return nil
		}
	}
	return temporary("The file names of the torrent on %s differ from the source.", j.target.Name)
}

// after stops the source, starts the target, and then removes or keeps the
// torrent on the source as the user chose. A cancel rolls back until the
// removal from the source starts.
func (j *job) after(ctx context.Context) error {
	if err := j.step(ctx, StateFinishing, stepAfter); err != nil {
		return err
	}
	after := j.rec.Options.After
	if after != AfterKeepSeeding {
		if err := j.svc.deps.Sync.BulkAction(ctx, j.source.ID, []string{j.hash()}, "pause"); err != nil {
			return temporary("qui cannot stop the torrent on %s: %v", j.source.Name, err)
		}
		j.stoppedSource = true
	}
	if !j.rec.Options.LeaveStopped {
		if err := j.startTarget(ctx); err != nil {
			return err
		}
	}
	if !j.svc.commit(j.rec.ID) {
		return errCancelled
	}
	if after == AfterKeepSeeding {
		j.logf("The torrent seeds on %s and on %s.", j.source.Name, j.target.Name)
		return nil
	}
	keep := ""
	if after == AfterRemoveWithFiles {
		keep = j.keepReason(ctx)
	}
	if err := j.stepWith(ctx, StateFinishing, stepRemoveFromSource, keep); err != nil {
		return err
	}
	return j.removeFromSource(ctx, keep)
}

// finishRemoval goes on with the removal from the source after a stop. It
// keeps the files when the recorded step says why.
func (j *job) finishRemoval(ctx context.Context) error {
	if !j.svc.commit(j.rec.ID) {
		return errCancelled
	}
	var keep string
	for _, step := range j.rec.Steps {
		if step.Name == stepRemoveFromSource {
			keep = step.Detail
		}
	}
	if err := j.stepWith(ctx, StateFinishing, stepRemoveFromSource, keep); err != nil {
		return err
	}
	var err error
	if j.srcFiles, _, err = j.svc.deps.Files.Require(ctx, j.source.ID, models.CapabilityReadBytes); err != nil {
		return temporary("qui cannot read the files of the Transfer source: %v", err)
	}
	if !j.readPlan(ctx) {
		return permanent("qui cannot read the names of the files of the torrent, so it cannot finish the removal from %s.", j.source.Name)
	}
	return j.removeFromSource(ctx, keep)
}

// removeFromSource removes the torrent from the source, and its files
// unless keep says why they stay or the user chose to keep them.
func (j *job) removeFromSource(ctx context.Context, keep string) error {
	onSource, err := j.svc.has(ctx, j.source.ID, j.hash())
	if err != nil {
		return temporary("qui cannot read the torrents of %s: %v", j.source.Name, err)
	}
	if onSource {
		if err := j.svc.deps.Sync.DeleteTorrents(ctx, j.source.ID, []string{j.hash()}, false, j.actor()); err != nil {
			return temporary("qui cannot remove the torrent from %s: %v", j.source.Name, err)
		}
	}
	j.stoppedSource = false
	switch {
	case j.rec.Options.After == AfterKeepFiles:
		j.logf("Removed the torrent from %s and kept its files: %s stay in use.", j.source.Name, formatBytes(diskBytes(j.plan)))
	case keep != "":
		j.logf("Removed the torrent from %s and kept its files, because %s: %s stay in use.", j.source.Name, keep, formatBytes(diskBytes(j.plan)))
	default:
		j.removeSourceFiles(ctx)
	}
	return nil
}

// startTarget starts the real torrent and waits until it seeds.
func (j *job) startTarget(ctx context.Context) error {
	if err := j.svc.deps.Sync.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), j.target.ID, []string{j.hash()}, "resume"); err != nil {
		return temporary("qui cannot start the torrent on %s: %v", j.target.Name, err)
	}
	cfg := j.svc.deps.Config
	var last qbt.TorrentState
	for poll := range cfg.Polls {
		if poll > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(cfg.PollInterval):
			}
		}
		torrent, err := j.svc.torrent(ctx, j.target.ID, j.hash())
		if err != nil || torrent == nil {
			continue
		}
		last = torrent.State
		switch {
		case slices.Contains([]qbt.TorrentState{qbt.TorrentStateUploading, qbt.TorrentStateStalledUp, qbt.TorrentStateForcedUp, qbt.TorrentStateQueuedUp}, torrent.State):
			j.logf("The torrent seeds on %s.", j.target.Name)
			return nil
		case torrent.State == qbt.TorrentStateMissingFiles || torrent.State == qbt.TorrentStateError:
			_ = j.svc.deps.Sync.BulkAction(ctx, j.target.ID, []string{j.hash()}, "pause")
			return permanent("The torrent on %s does not find its files: qBittorrent shows %s.", j.target.Name, torrent.State)
		}
	}
	return temporary("The torrent on %s did not start to seed: qBittorrent shows %q.", j.target.Name, last)
}

// keepReason says why qui keeps the files on the source, empty when it may
// remove them.
func (j *job) keepReason(ctx context.Context) string {
	instances, err := j.svc.deps.Instances.List(ctx)
	if err != nil {
		return "qui cannot list the instances that use the same SSH server"
	}
	pin := j.hostKeyPin(j.source)
	for _, inst := range instances {
		if inst.ID != j.source.ID && fsops.SameSSHServer(j.source, pin, inst, j.hostKeyPin) {
			return inst.Name + " uses the same SSH server"
		}
	}
	read, err := j.svc.othersRead(ctx, j.source.ID, j.hash(), true)
	switch {
	case err != nil:
		return "qui cannot check which torrents read them"
	case read:
		return "another torrent reads them"
	}
	return ""
}

func (j *job) hostKeyPin(inst *models.Instance) []byte {
	pin, err := j.svc.deps.Instances.GetHostKeyPin(inst)
	if err != nil && !errors.Is(err, models.ErrSSHHostKeyNotPinned) {
		log.Warn().Err(err).Int("instance", inst.ID).Msg("transfer: cannot read the host key pin, pairing instances by host and port only")
	}
	return pin
}

// removeSourceFiles removes the files that qui copied from the source, and
// the folders that this leaves empty. A file whose size changed stays.
func (j *job) removeSourceFiles(ctx context.Context) {
	paths := j.srcFiles.Paths()
	saveRoot := paths.Clean(j.member().SourceSavePath)
	folders := map[string]struct{}{}
	var kept, notCopied int64
	for _, f := range j.plan {
		if f.pad {
			continue
		}
		if f.onTarget {
			notCopied += f.size
			continue
		}
		p := paths.Join(saveRoot, paths.FromSlash(f.name))
		info, err := j.srcFiles.Lstat(ctx, p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || info.IsDir || info.Size != f.size {
			kept += f.size
			continue
		}
		if err := j.srcFiles.Remove(ctx, p, fsops.RemoveOptions{}); err != nil {
			j.logf("qui could not remove %s on %s: %v", f.name, j.source.Name, err)
			kept += f.size
			continue
		}
		for dir := paths.Dir(p); dir != saveRoot && len(dir) > len(saveRoot); dir = paths.Dir(dir) {
			folders[dir] = struct{}{}
		}
	}
	for _, dir := range slices.SortedFunc(maps.Keys(folders), func(a, b string) int { return len(b) - len(a) }) {
		_ = j.srcFiles.Remove(ctx, dir, fsops.RemoveOptions{})
	}
	if notCopied > 0 {
		j.logf("%s stay on %s, because qui did not copy them: %s had them before the Transfer.", formatBytes(notCopied), j.source.Name, j.target.Name)
	}
	if kept > 0 {
		j.logf("Removed the torrent and its files from %s. %s stay in use.", j.source.Name, formatBytes(kept))
		return
	}
	j.logf("Removed the torrent and its files from %s.", j.source.Name)
}
