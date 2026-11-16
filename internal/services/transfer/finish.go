// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
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
	"github.com/autobrr/qui/internal/services/crossseed"
)

// prove proves each member before qui touches the source: by its own check
// of every piece with a confirmed flush, or else by a check-only helper
// torrent that checks every piece of the member on the target. qui's own
// check never covers the files that were on the target before the Transfer.
func (j *job) prove(ctx context.Context) error {
	checking := false
	for _, p := range j.parts {
		switch {
		case p.headStartProven:
			j.logf("Proof: every file%s was on %s, and the check passed all %d pieces.", j.of(p), j.target.Name, p.src.info.NumPieces())
			continue
		case !p.hasOnTarget() && j.rec.FlushState == FlushConfirmed && p.allProven():
			j.logf("Proof: qui checked every piece%s, and %s confirmed each flush.", j.of(p), j.target.Name)
			continue
		}
		if !checking {
			if err := j.step(ctx, StateChecking, "check on the target"); err != nil {
				return err
			}
			checking = true
		}
		states, err := j.checkOnTarget(ctx, p)
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
			return permanent("The check on %s failed %d of %d pieces%s: %s%s.", j.target.Name, len(failed), len(states), j.of(p), strings.Join(shown, ", "), more)
		}
		j.logf("Proof: the check on %s passed all %d pieces%s.", j.target.Name, len(states), j.of(p))
	}
	return nil
}

// checkOnTarget checks every piece of the member on the target with a
// check-only helper torrent and returns what the check found. The helper
// finds each file that qui writes under its temporary name, and each file
// that was on the target through a hardlink, so that qBittorrent renames no
// file that qui did not write. The links are gone when it returns.
func (j *job) checkOnTarget(ctx context.Context, p *part) ([]qbt.PieceState, error) {
	defer j.unlink(context.WithoutCancel(ctx), p)
	if err := j.link(ctx, p); err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	random := hex.EncodeToString(salt)
	checkNames := make([]string, 0, len(p.plan))
	for _, f := range p.plan {
		if !f.pad {
			checkNames = append(checkNames, j.checkName(f))
		}
	}
	helper, err := helpertorrent.Build(p.src.export, checkNames, random)
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
			SavePath:             p.Placement.SavePath,
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
	if len(states) != p.src.info.NumPieces() {
		return nil, temporary("The check on %s reported %d pieces of %d.", j.target.Name, len(states), p.src.info.NumPieces())
	}
	return states, nil
}

func (j *job) of(p *part) string {
	if len(j.parts) == 1 {
		return ""
	}
	return " of " + p.Name
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

// place renames the copy into place and adds each member: on the target,
// stopped and in seed mode, with the file names of the source, and on the
// Cross-seed instance of the target through the routed cross-seed add.
func (j *job) place(ctx context.Context) error {
	if err := j.step(ctx, StateFinishing, "place"); err != nil {
		return err
	}
	for _, p := range j.parts {
		inst := j.instanceOf(p)
		if onTarget, err := j.svc.has(ctx, inst.ID, p.Hash); err != nil {
			return temporary("qui cannot read the torrents of %s: %v", inst.Name, err)
		} else if onTarget {
			which := "this torrent"
			if len(j.parts) > 1 {
				which = p.Name
			}
			return permanent("Already on the target: %s has %s.", inst.Name, which)
		}
	}
	if err := j.putInPlace(ctx); err != nil {
		return err
	}

	var representative *part
	for _, p := range j.parts {
		if p.TargetInstanceID != j.target.ID {
			continue
		}
		if err := j.add(ctx, p); err != nil {
			return err
		}
		if representative == nil {
			representative = p
		}
	}
	for _, p := range j.parts {
		if p.TargetInstanceID != j.target.ID {
			if err := j.addCrossSeed(ctx, p, representative); err != nil {
				return err
			}
		}
	}
	return nil
}

// putInPlace renames each file that qui wrote from its temporary name to its
// name.
func (j *job) putInPlace(ctx context.Context) error {
	for _, f := range j.files {
		if f.onTarget {
			continue
		}
		temp := j.tempName(f.rel)
		if _, err := j.root.Lstat(ctx, temp); errors.Is(err, fs.ErrNotExist) {
			temp += incompleteExt
		}
		if err := j.root.RenameNoReplace(ctx, temp, f.rel); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return permanent("Conflict: a file is in the way on %s at %s.", j.target.Name, f.rel)
			}
			return temporary("qui cannot put %s in place on %s: %v", f.rel, j.target.Name, err)
		}
		j.placed = append(j.placed, f.rel)
	}
	return nil
}

// add adds the member on the target, stopped and in seed mode.
func (j *job) add(ctx context.Context, p *part) error {
	layout := qbt.ContentLayoutOriginal
	if p.src.info.IsDir() && commonRoot(names(p.plan)) == "" {
		layout = qbt.ContentLayoutSubfolderNone
	}
	j.added = true
	cfg := j.svc.deps.Config
	res := j.svc.adder.Add(ctx, torrentadd.Request{
		InstanceID: j.target.ID,
		Torrent:    p.src.export,
		Hash:       p.Hash,
		Settings: torrentadd.Settings{
			SavePath:        p.Placement.SavePath,
			Category:        p.Placement.Category,
			Tags:            p.Placement.Tags,
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
		return temporary("%s did not take %s: %s", j.target.Name, j.the(p, "the"), describe(res))
	}
	return j.rename(ctx, p)
}

// addCrossSeed adds the member on the Cross-seed instance of the target, on
// links to the files of the representative. The cross-seed add checks it
// and leaves it stopped.
func (j *job) addCrossSeed(ctx context.Context, p, representative *part) error {
	if representative == nil {
		return permanent("%s has no torrent on %s to link %s to.", j.crossSeed.Name, j.target.Name, p.Name)
	}
	j.added = true
	res, err := j.svc.deps.CrossSeeds.CrossSeed(ctx, &crossseed.CrossSeedRequest{
		TorrentData:       base64.StdEncoding.EncodeToString(p.src.export),
		TargetInstanceIDs: []int{j.target.ID},
		ManualTargetHash:  representative.Hash,
		SkipAutoResume:    true,
	})
	if err != nil {
		return temporary("%s did not take %s: %v", j.crossSeed.Name, p.Name, err)
	}
	why := "the cross-seed add reported no result"
	for _, r := range res.Results {
		if r.InstanceID != j.target.ID {
			continue
		}
		if r.Success && r.CrossSeedInstanceID == p.TargetInstanceID {
			return nil
		}
		why = r.Status
		if r.Message != "" {
			why += ": " + r.Message
		}
	}
	return temporary("%s did not take %s: %s", j.crossSeed.Name, p.Name, why)
}

func (j *job) instanceOf(p *part) *models.Instance {
	if p.TargetInstanceID == j.target.ID {
		return j.target
	}
	return j.crossSeed
}

func (j *job) the(p *part, article string) string {
	if len(j.parts) == 1 {
		return article + " torrent"
	}
	return p.Name
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

// rename gives each file of the member on the target the name that it has
// on the source.
func (j *job) rename(ctx context.Context, p *part) error {
	ctx = qbittorrent.WithPostAddBulkActionRetry(ctx)
	want := names(p.plan)
	for range 2 {
		files, err := j.svc.deps.Sync.GetTorrentFiles(qbittorrent.WithForceFilesRefresh(ctx), j.target.ID, p.Hash)
		if err != nil || files == nil || len(*files) != len(want) {
			return temporary("qui cannot read the files of the torrent on %s: %v", j.target.Name, err)
		}
		listed := slices.SortedFunc(slices.Values(*files), func(a, b qbt.TorrentFile) int { return cmp.Compare(a.Index, b.Index) })
		renamed := false
		for i, f := range listed {
			if f.Name == want[i] {
				continue
			}
			if err := j.svc.deps.Sync.RenameTorrentFile(ctx, j.target.ID, p.Hash, f.Name, want[i]); err != nil {
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
// torrents on the source as the user chose.
func (j *job) after(ctx context.Context) error {
	if err := j.step(ctx, StateFinishing, "after"); err != nil {
		return err
	}
	hashes := j.hashes()
	after := j.rec.Options.After
	if after != AfterKeepSeeding {
		if err := j.svc.deps.Sync.BulkAction(ctx, j.source.ID, hashes, "pause"); err != nil {
			return temporary("qui cannot stop %s on %s: %v", j.torrents("the"), j.source.Name, err)
		}
		j.stoppedSource = true
	}
	if !j.rec.Options.LeaveStopped {
		for _, p := range j.parts {
			if err := j.startTarget(ctx, p); err != nil {
				return err
			}
		}
	}
	switch after {
	case AfterKeepSeeding:
		j.logf("%s seeds on %s and on %s.", j.torrents("The"), j.source.Name, j.target.Name)
		return nil
	case AfterKeepFiles:
		if err := j.svc.deps.Sync.DeleteTorrents(ctx, j.source.ID, hashes, false, j.actor()); err != nil {
			return temporary("qui cannot remove %s from %s: %v", j.torrents("the"), j.source.Name, err)
		}
		j.stoppedSource = false
		j.logf("Removed %s from %s and kept %s files: %s stay in use.", j.torrents("the"), j.source.Name, j.its(), formatBytes(diskBytes(j.files)))
		return nil
	case AfterRemoveWithFiles:
	}

	keep := j.keepReason(ctx)
	if err := j.svc.deps.Sync.DeleteTorrents(ctx, j.source.ID, hashes, false, j.actor()); err != nil {
		return temporary("qui cannot remove %s from %s: %v", j.torrents("the"), j.source.Name, err)
	}
	j.stoppedSource = false
	if keep != "" {
		j.logf("Removed %s from %s and kept %s files, because %s: %s stay in use.", j.torrents("the"), j.source.Name, j.its(), keep, formatBytes(diskBytes(j.files)))
		return nil
	}
	j.removeSourceFiles(ctx)
	return nil
}

// startTarget starts the member where it landed and waits until it seeds.
func (j *job) startTarget(ctx context.Context, p *part) error {
	inst := j.instanceOf(p)
	if err := j.svc.deps.Sync.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), inst.ID, []string{p.Hash}, "resume"); err != nil {
		return temporary("qui cannot start %s on %s: %v", j.the(p, "the"), inst.Name, err)
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
		torrent, err := j.svc.torrent(ctx, inst.ID, p.Hash)
		if err != nil || torrent == nil {
			continue
		}
		last = torrent.State
		switch {
		case slices.Contains([]qbt.TorrentState{qbt.TorrentStateUploading, qbt.TorrentStateStalledUp, qbt.TorrentStateForcedUp, qbt.TorrentStateQueuedUp}, torrent.State):
			j.logf("%s seeds on %s.", j.the(p, "The"), inst.Name)
			return nil
		case torrent.State == qbt.TorrentStateMissingFiles || torrent.State == qbt.TorrentStateError:
			_ = j.svc.deps.Sync.BulkAction(ctx, inst.ID, []string{p.Hash}, "pause")
			return permanent("%s on %s does not find its files: qBittorrent shows %s.", j.the(p, "The"), inst.Name, torrent.State)
		}
	}
	return temporary("%s on %s did not start to seed: qBittorrent shows %q.", j.the(p, "The"), inst.Name, last)
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
	read, err := j.svc.othersRead(ctx, j.source.ID, j.hashes())
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
// the folders that this leaves empty below the save path of each member. A
// file whose size changed stays.
func (j *job) removeSourceFiles(ctx context.Context) {
	paths := j.srcFiles.Paths()
	folders := map[string]struct{}{}
	var kept, notCopied int64
	for _, f := range j.files {
		if f.onTarget {
			notCopied += f.size
			continue
		}
		saveRoot := paths.Clean(f.owner.src.torrent.SavePath)
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
	if notCopied > 0 {
		j.logf("%s stay on %s, because qui did not copy them: %s had them before the Transfer.", formatBytes(notCopied), j.source.Name, j.target.Name)
	}
	if kept > 0 {
		j.logf("Removed %s and %s files from %s. %s stay in use.", j.torrents("the"), j.its(), j.source.Name, formatBytes(kept))
		return
	}
	j.logf("Removed %s and %s files from %s.", j.torrents("the"), j.its(), j.source.Name)
}
