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
// that checks every piece on the target.
func (j *job) prove(ctx context.Context) error {
	if j.rec.FlushState == FlushConfirmed && j.allProven() {
		j.logf("Proof: qui checked every piece, and %s confirmed each flush.", j.target.Name)
		return nil
	}
	if err := j.step(ctx, StateChecking, "check on the target"); err != nil {
		return err
	}
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	random := hex.EncodeToString(salt)
	temps := make([]string, 0, len(j.plan))
	for _, name := range names(j.plan) {
		temps = append(temps, j.tempName(name))
	}
	helper, err := helpertorrent.Build(j.src.export, temps, random)
	if err != nil {
		return permanent("qui cannot build the helper torrent: %v", err)
	}
	if err := j.svc.deps.Store.RegisterHelper(ctx, Helper{
		InstanceID: j.target.ID, Hash: helper.InfoHash, Random: []byte(random), Owner: OwnerTransfer, OwnerID: j.rec.ID,
	}); err != nil {
		return err
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
		return err
	}
	if res.Outcome == torrentadd.Failed || res.Outcome == torrentadd.Checking {
		return temporary("The check on %s did not end: %s", j.target.Name, describe(res))
	}
	states, err := j.svc.deps.Sync.GetTorrentPieceStates(ctx, j.target.ID, helper.InfoHash)
	if err != nil {
		return temporary("qui cannot read the check on %s: %v", j.target.Name, err)
	}
	if err := j.removeHelper(ctx); err != nil {
		return temporary("qui cannot remove its helper torrent from %s: %v", j.target.Name, err)
	}
	if len(states) != j.src.info.NumPieces() {
		return temporary("The check on %s reported %d pieces of %d.", j.target.Name, len(states), j.src.info.NumPieces())
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
	if err := j.step(ctx, StateFinishing, "place"); err != nil {
		return err
	}
	if onTarget, err := j.svc.has(ctx, j.target.ID, j.hash()); err != nil {
		return temporary("qui cannot read the torrents of %s: %v", j.target.Name, err)
	} else if onTarget {
		return permanent("Already on the target: %s has this torrent.", j.target.Name)
	}
	for _, name := range names(j.plan) {
		temp := j.tempName(name)
		if _, err := j.root.Lstat(ctx, temp); errors.Is(err, fs.ErrNotExist) {
			temp += incompleteExt
		}
		if err := j.root.RenameNoReplace(ctx, temp, name); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return permanent("Conflict: a file is in the way on %s at %s.", j.target.Name, name)
			}
			return temporary("qui cannot put %s in place on %s: %v", name, j.target.Name, err)
		}
		j.placed = append(j.placed, name)
	}

	placement := j.member().Placement
	layout := qbt.ContentLayoutOriginal
	if j.src.info.IsDir() && commonRoot(names(j.plan)) == "" {
		layout = qbt.ContentLayoutSubfolderNone
	}
	j.added = true
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
	if res.Outcome != torrentadd.Verified {
		return temporary("%s did not take the torrent: %s", j.target.Name, describe(res))
	}
	return j.rename(ctx)
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
// torrent on the source as the user chose.
func (j *job) after(ctx context.Context) error {
	if err := j.step(ctx, StateFinishing, "after"); err != nil {
		return err
	}
	hashes := []string{j.hash()}
	after := j.rec.Options.After
	if after != AfterKeepSeeding {
		if err := j.svc.deps.Sync.BulkAction(ctx, j.source.ID, hashes, "pause"); err != nil {
			return temporary("qui cannot stop the torrent on %s: %v", j.source.Name, err)
		}
		j.stoppedSource = true
	}
	if !j.rec.Options.LeaveStopped {
		if err := j.startTarget(ctx); err != nil {
			return err
		}
	}
	switch after {
	case AfterKeepSeeding:
		j.logf("The torrent seeds on %s and on %s.", j.source.Name, j.target.Name)
		return nil
	case AfterKeepFiles:
		if err := j.svc.deps.Sync.DeleteTorrents(ctx, j.source.ID, hashes, false, j.actor()); err != nil {
			return temporary("qui cannot remove the torrent from %s: %v", j.source.Name, err)
		}
		j.stoppedSource = false
		j.logf("Removed the torrent from %s and kept its files: %s stay in use.", j.source.Name, formatBytes(diskBytes(j.plan)))
		return nil
	case AfterRemoveWithFiles:
	}

	keep := j.keepReason(ctx)
	if err := j.svc.deps.Sync.DeleteTorrents(ctx, j.source.ID, hashes, false, j.actor()); err != nil {
		return temporary("qui cannot remove the torrent from %s: %v", j.source.Name, err)
	}
	j.stoppedSource = false
	if keep != "" {
		j.logf("Removed the torrent from %s and kept its files, because %s: %s stay in use.", j.source.Name, keep, formatBytes(diskBytes(j.plan)))
		return nil
	}
	j.removeSourceFiles(ctx)
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
	saveRoot := paths.Clean(j.src.torrent.SavePath)
	folders := map[string]struct{}{}
	var kept int64
	for _, f := range j.plan {
		if f.pad {
			continue
		}
		p := paths.Join(saveRoot, paths.FromSlash(f.name))
		info, err := j.srcFiles.Lstat(ctx, p)
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
	if kept > 0 {
		j.logf("Removed the torrent and its files from %s. %s stay in use.", j.source.Name, formatBytes(kept))
		return
	}
	j.logf("Removed the torrent and its files from %s.", j.source.Name)
}
