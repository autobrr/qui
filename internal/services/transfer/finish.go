// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
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

func (j *job) prove(ctx context.Context) error {
	checking := false
	for _, p := range j.parts {
		switch {
		case p.headStartProven:
			j.logf("Proof: every file%s was on %s, and the check passed all %d pieces.", j.of(p), j.target.Name, p.src.info.NumPieces())
			continue
		case p.passed:
			j.logf("Proof: BitTorrent checked every piece%s on %s.", j.of(p), j.target.Name)
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
			Tags:                 []string{TransferHelperTag},
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

func (j *job) place(ctx context.Context) error {
	again := j.reached(stepPlace)
	if err := j.stepWith(ctx, StateFinishing, stepPlace, headStartDetail(j.files)); err != nil {
		return err
	}
	for _, p := range j.parts {
		if j.added(p) {
			continue
		}
		inst := j.instanceOf(p)
		onTarget, err := j.svc.has(ctx, inst.ID, p.Hash)
		switch {
		case err != nil:
			return temporary("qui cannot read the torrents of %s: %v", inst.Name, err)
		case onTarget && j.reachedFor(stepAdd, p.Hash):
			return j.ownershipUnknown(p)
		case onTarget:
			which := "this torrent"
			if len(j.parts) > 1 {
				which = p.Name
			}
			return permanent("Already on the target: %s has %s.", inst.Name, which)
		}
	}
	if err := j.putInPlace(ctx, again); err != nil {
		return err
	}
	return j.addMembers(ctx)
}

func (j *job) addMembers(ctx context.Context) error {
	var representative *part
	for _, p := range j.parts {
		if p.TargetInstanceID != j.target.ID {
			continue
		}
		if err := j.addOnTarget(ctx, p); err != nil {
			return err
		}
		if representative == nil {
			representative = p
		}
	}
	for _, p := range j.parts {
		if p.TargetInstanceID == j.target.ID {
			continue
		}
		if !j.added(p) {
			if err := j.addCrossSeed(ctx, p, representative); err != nil {
				return err
			}
		}
		if err := j.crossSeedChecked(ctx, p); err != nil {
			return err
		}
	}
	return j.manage(ctx)
}

func (j *job) addOnTarget(ctx context.Context, p *part) error {
	complete := false
	if !j.added(p) {
		var err error
		if complete, err = j.add(ctx, p); err != nil {
			return err
		}
	}
	if err := j.rename(ctx, p); err != nil {
		return err
	}
	switch {
	case j.reachedFor(stepRename, p.Hash):
		j.logf("%s checks the files of %s after their rename.", j.target.Name, j.the(p, "the"))
		return j.recheck(ctx, p)
	case !complete:
		return j.check(ctx, p)
	}
	return nil
}

func (j *job) putInPlace(ctx context.Context, again bool) error {
	if j.blind {
		return nil
	}
	for _, f := range j.files {
		if !f.onTarget {
			if err := j.placeFile(ctx, f.rel, again); err != nil {
				return err
			}
		}
	}
	return nil
}

func (j *job) placeFile(ctx context.Context, name string, again bool) error {
	temp := j.tempName(name)
	_, err := j.root.Lstat(ctx, temp)
	if errors.Is(err, fs.ErrNotExist) {
		temp += incompleteExt
		_, err = j.root.Lstat(ctx, temp)
	}
	if errors.Is(err, fs.ErrNotExist) {
		if again {
			_, err := j.root.Lstat(ctx, name)
			switch {
			case err == nil:
				return nil
			case !errors.Is(err, fs.ErrNotExist):
				return temporary("qui cannot look for the copy of %s on %s: %w", name, j.target.Name, err)
			}
		}
		return permanent("The copy of %s is missing on %s.", name, j.target.Name)
	}
	err = j.root.RenameNoReplace(ctx, temp, name)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrExist) && again:
		same, err := j.sameFile(ctx, temp, name)
		switch {
		case err != nil:
			return cannotLook(j.target.Name, err)
		case !same:
			return permanent("Conflict: a file is in the way on %s at %s.", j.target.Name, name)
		}
		if err := j.root.Remove(ctx, temp); err != nil {
			return temporary("qui cannot remove %s on %s: %v", temp, j.target.Name, err)
		}
		return nil
	case errors.Is(err, fs.ErrExist):
		return permanent("Conflict: a file is in the way on %s at %s.", j.target.Name, name)
	}
	return temporary("qui cannot put %s in place on %s: %v", name, j.target.Name, err)
}

func (j *job) sameFile(ctx context.Context, a, b string) (bool, error) {
	ai, err := j.root.Lstat(ctx, a)
	if err != nil {
		return false, err
	}
	bi, err := j.root.Lstat(ctx, b)
	if err != nil {
		return false, err
	}
	return ai.FileIDErr == nil && !ai.FileID.IsZero() && bi.FileIDErr == nil && ai.FileID == bi.FileID, nil
}

func (j *job) add(ctx context.Context, p *part) (complete bool, err error) {
	if err := j.stepWith(ctx, StateFinishing, stepAdd, p.Hash); err != nil {
		return false, err
	}
	cfg := j.svc.deps.Config
	res := j.svc.adder.Add(ctx, torrentadd.Request{
		InstanceID: j.target.ID,
		Torrent:    p.src.export,
		Hash:       p.Hash,
		Settings: torrentadd.Settings{
			SavePath:        p.Placement.SavePath,
			Category:        p.Placement.Category,
			Tags:            p.Placement.Tags,
			ContentLayout:   addLayout(p.src.info, p.plan),
			Stopped:         new(true),
			AutoTMM:         new(false),
			UseDownloadPath: new(false),
			SeedMode:        true,
		},
		Polls:        cfg.Polls,
		PollInterval: cfg.PollInterval,
	})
	if res.Failure == torrentadd.AddRefused {
		if onTarget, err := j.svc.has(ctx, j.target.ID, p.Hash); err == nil && onTarget {
			return false, j.ownershipUnknown(p)
		}
		return false, temporary("%s did not take %s: %s", j.target.Name, j.the(p, "the"), describe(res))
	}
	if err := j.stepWith(ctx, StateFinishing, stepAdded, p.Hash); err != nil {
		return false, err
	}
	if res.Outcome != torrentadd.Verified && res.Failure != torrentadd.MissingFiles {
		return false, temporary("%s did not take %s: %s", j.target.Name, j.the(p, "the"), describe(res))
	}
	return res.Outcome == torrentadd.Verified, nil
}

func (j *job) check(ctx context.Context, p *part) error {
	req := torrentadd.Request{InstanceID: j.target.ID, Hash: p.Hash, Polls: 1, PollInterval: j.svc.deps.Config.PollInterval}
	if res := j.svc.adder.Confirm(ctx, req); res.Outcome == torrentadd.Verified {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	j.logf("%s does not show %s complete after the add, so it checks the files.", j.target.Name, j.the(p, "the"))
	return j.recheck(ctx, p)
}

func (j *job) recheck(ctx context.Context, p *part) error {
	cfg := j.svc.deps.Config
	res := j.svc.adder.Recheck(ctx, torrentadd.Request{InstanceID: j.target.ID, Hash: p.Hash, Polls: cfg.CheckPolls, PollInterval: cfg.PollInterval})
	if err := ctx.Err(); err != nil {
		return err
	}
	if res.Outcome != torrentadd.Verified {
		return temporary("The check of %s on %s did not pass: %s", j.the(p, "the"), j.target.Name, describe(res))
	}
	return nil
}

func (j *job) manage(ctx context.Context) error {
	var hashes []string
	for _, p := range j.parts {
		if p.Placement.AutoTMM {
			hashes = append(hashes, p.Hash)
		}
	}
	if len(hashes) == 0 {
		return nil
	}
	err := j.svc.deps.Sync.SetAutoTMM(qbittorrent.WithPostAddBulkActionRetry(ctx), j.target.ID, hashes, true)
	if err != nil {
		return temporary("qui cannot turn on Automatic Torrent Management for %s on %s: %v", j.torrents("the"), j.target.Name, err)
	}
	return nil
}

func (j *job) addCrossSeed(ctx context.Context, p, representative *part) error {
	if representative == nil {
		return permanent("%s has no torrent on %s to link %s to.", j.crossSeed.Name, j.target.Name, p.Name)
	}
	route, err := j.svc.deps.Routes.Get(ctx, j.target.ID)
	if err != nil {
		return temporary("qui cannot read the cross-seed route of %s: %v", j.target.Name, err)
	}
	if route == nil || route.CrossSeedInstanceID != p.TargetInstanceID {
		return permanent("%s no longer sends its cross-seeds to %s, so the Transfer stopped.", j.target.Name, j.crossSeed.Name)
	}
	if err := j.stepWith(ctx, StateFinishing, stepAdd, p.Hash); err != nil {
		return err
	}
	res, err := j.svc.deps.CrossSeeds.CrossSeed(ctx, &crossseed.CrossSeedRequest{
		TorrentData:       base64.StdEncoding.EncodeToString(p.src.export),
		TargetInstanceIDs: []int{j.target.ID},
		ManualTargetHash:  representative.Hash,
		SkipAutoResume:    true,
	})
	if err != nil {
		if onTarget, hasErr := j.svc.has(ctx, j.crossSeed.ID, p.Hash); hasErr == nil && onTarget {
			return j.ownershipUnknown(p)
		}
		return temporary("%s did not take %s: %v", j.crossSeed.Name, p.Name, err)
	}
	why := "the cross-seed add reported no result"
	stranded := false
	for _, r := range res.Results {
		if r.InstanceID != j.target.ID {
			continue
		}
		landed := r.CrossSeedInstanceID == p.TargetInstanceID
		if r.Success && landed {
			return j.stepWith(ctx, StateFinishing, stepAdded, p.Hash)
		}
		stranded = stranded || landed && crossseed.ClassOf(r.Status) == crossseed.ClassStranded
		why = r.Status
		if r.Message != "" {
			why += ": " + r.Message
		}
	}
	if onTarget, err := j.svc.has(ctx, j.crossSeed.ID, p.Hash); err == nil && onTarget {
		if !stranded {
			return j.ownershipUnknown(p)
		}
		if err := j.stepWith(ctx, StateFinishing, stepAdded, p.Hash); err != nil {
			return err
		}
	}
	return temporary("%s did not take %s: %s", j.crossSeed.Name, p.Name, why)
}

func (j *job) crossSeedChecked(ctx context.Context, p *part) error {
	cfg := j.svc.deps.Config
	res := j.svc.adder.ConfirmRecheck(ctx, torrentadd.Request{
		InstanceID:   j.crossSeed.ID,
		Hash:         p.Hash,
		Polls:        cfg.CheckPolls,
		PollInterval: cfg.PollInterval,
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	if res.Outcome != torrentadd.Verified {
		return temporary("The check of %s on %s did not pass: %s", p.Name, j.crossSeed.Name, describe(res))
	}
	return nil
}

func (j *job) ownershipUnknown(p *part) error {
	inst := j.instanceOf(p)
	return permanent("Ownership unknown: %s has %s, but its add did not answer, so qui cannot tell who added it. "+
		"qui keeps the torrents and their files on %s, and %s on %s.", inst.Name, j.the(p, "the"), j.target.Name, j.torrents("the"), j.source.Name)
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

func (j *job) rename(ctx context.Context, p *part) error {
	ctx = qbittorrent.WithPostAddBulkActionRetry(ctx)
	want := names(p.plan)
	sent := false
	for poll := range j.svc.deps.Config.Polls {
		if poll > 1 {
			if err := j.pause(ctx, poll); err != nil {
				return err
			}
		}
		files, err := j.svc.deps.Sync.GetTorrentFiles(qbittorrent.WithForceFilesRefresh(ctx), j.target.ID, p.Hash)
		if err != nil || files == nil || len(*files) != len(want) {
			return temporary("qui cannot read the files of the torrent on %s: %v", j.target.Name, err)
		}
		listed := slices.SortedFunc(slices.Values(*files), func(a, b qbt.TorrentFile) int { return cmp.Compare(a.Index, b.Index) })
		if slices.EqualFunc(listed, want, func(f qbt.TorrentFile, name string) bool { return f.Name == name }) {
			return nil
		}
		if sent {
			continue
		}
		if err := j.sendRenames(ctx, p, listed, want); err != nil {
			return err
		}
		sent = true
	}
	return temporary("The file names of the torrent on %s differ from the source.", j.target.Name)
}

func (j *job) sendRenames(ctx context.Context, p *part, listed []qbt.TorrentFile, want []string) error {
	if !j.reachedFor(stepRename, p.Hash) {
		if err := j.stepWith(ctx, StateFinishing, stepRename, p.Hash); err != nil {
			return err
		}
	}
	for i, f := range listed {
		if f.Name == want[i] {
			continue
		}
		if !j.blind {
			if err := j.noFileAtOldName(ctx, path.Join(p.dir, f.Name), path.Join(p.dir, want[i])); err != nil {
				return err
			}
		}
		if err := j.svc.deps.Sync.RenameTorrentFile(ctx, j.target.ID, p.Hash, f.Name, want[i]); err != nil {
			return temporary("qui cannot rename %s to %s on %s: %v", f.Name, want[i], j.target.Name, err)
		}
	}
	return nil
}

func (j *job) noFileAtOldName(ctx context.Context, old, name string) error {
	_, err := j.root.Lstat(ctx, old)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return cannotLook(j.target.Name, err)
	}
	one, err := j.oneFile(ctx, old, name)
	switch {
	case err != nil:
		return cannotLook(j.target.Name, err)
	case one:
		return nil
	}
	return j.inTheWay(old)
}

func (j *job) oneFile(ctx context.Context, a, b string) (bool, error) {
	if !strings.EqualFold(a, b) {
		return false, nil
	}
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for k := range as {
		if as[k] == bs[k] {
			continue
		}
		entries, err := j.dstFiles.ReadDir(ctx, j.targetPath(path.Join(bs[:k]...)))
		if err != nil {
			return false, err
		}
		has := func(name string) bool {
			return slices.ContainsFunc(entries, func(e fsops.DirEntry) bool { return e.Name == name })
		}
		if has(as[k]) && has(bs[k]) {
			return false, nil
		}
	}
	return true, nil
}

func (j *job) after(ctx context.Context) error {
	if err := j.checkInstances(ctx); err != nil {
		return err
	}
	after := j.rec.Options.After
	var detail []byte
	if after != AfterKeepSeeding {
		running, err := j.running(ctx)
		if err != nil {
			return err
		}
		detail, _ = json.Marshal(running)
	}
	if err := j.stepWith(ctx, StateFinishing, stepAfter, string(detail)); err != nil {
		return err
	}
	if after != AfterKeepSeeding {
		if err := j.svc.deps.Sync.BulkAction(ctx, j.source.ID, j.hashes(), "pause"); err != nil {
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
	if !j.svc.commit(j.rec.ID) {
		return errCancelled
	}
	if after == AfterKeepSeeding {
		j.logf("%s seeds on %s and on %s.", j.torrents("The"), j.source.Name, j.target.Name)
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

func (j *job) running(ctx context.Context) ([]string, error) {
	torrents, err := j.svc.deps.Sync.GetTorrentRefsFresh(ctx, j.source.ID, qbittorrent.TorrentRefFilter{Hashes: j.hashes()})
	if err != nil {
		return nil, temporary("qui cannot read the torrents on %s: %v", j.source.Name, err)
	}
	running := []string{}
	for _, t := range torrents {
		if !slices.Contains([]qbt.TorrentState{qbt.TorrentStateStoppedUp, qbt.TorrentStatePausedUp, qbt.TorrentStateStoppedDl, qbt.TorrentStatePausedDl}, t.State) {
			running = append(running, t.Hash)
		}
	}
	return running, nil
}

func (j *job) finishRemoval(ctx context.Context) error {
	if !j.svc.commit(j.rec.ID) {
		return errCancelled
	}
	if err := j.checkInstances(ctx); err != nil {
		return err
	}
	if models.FilesystemAccessMode(j.source) != models.FilesystemModeNone {
		var err error
		if j.srcFiles, _, err = j.svc.deps.Files.Require(ctx, j.source.ID, models.CapabilityReadBytes); err != nil {
			return temporary("qui cannot read the files of the Transfer source: %v", err)
		}
	}
	if !j.readGroup(ctx) {
		return permanent("qui cannot read the names of the files of %s, so it cannot finish the removal from %s.", j.torrents("the"), j.source.Name)
	}
	var keep string
	for _, step := range j.rec.Steps {
		if step.Name == stepRemoveFromSource {
			keep = step.Detail
		}
	}
	if keep == "" && j.rec.Options.After == AfterRemoveWithFiles {
		keep = j.keepReason(ctx)
	}
	if err := j.stepWith(ctx, StateFinishing, stepRemoveFromSource, keep); err != nil {
		return err
	}
	return j.removeFromSource(ctx, keep)
}

func (j *job) removeFromSource(ctx context.Context, keep string) error {
	present, err := j.svc.deps.Sync.GetTorrentRefsFresh(ctx, j.source.ID, qbittorrent.TorrentRefFilter{Hashes: j.hashes()})
	if err != nil {
		return temporary("qui cannot read the torrents of %s: %v", j.source.Name, err)
	}
	withFiles := j.srcFiles == nil && j.rec.Options.After == AfterRemoveWithFiles && keep == ""
	if len(present) > 0 {
		hashes := make([]string, 0, len(present))
		for _, t := range present {
			hashes = append(hashes, t.Hash)
		}
		if err := j.svc.deps.Sync.DeleteTorrents(ctx, j.source.ID, hashes, withFiles, j.actor()); err != nil {
			return temporary("qui cannot remove %s from %s: %v", j.torrents("the"), j.source.Name, err)
		}
	}
	j.stoppedSource = false
	switch {
	case j.rec.Options.After == AfterKeepFiles:
		j.logf("Removed %s from %s and kept %s files: %s stay in use.", j.torrents("the"), j.source.Name, j.its(), formatBytes(diskBytes(j.files)))
	case keep != "":
		j.logf("Removed %s from %s and kept %s files, because %s: %s stay in use.", j.torrents("the"), j.source.Name, j.its(), keep, formatBytes(diskBytes(j.files)))
	case withFiles:
		j.logf("Removed %s and %s files from %s.", j.torrents("the"), j.its(), j.source.Name)
	default:
		j.removeSourceFiles(ctx)
	}
	return nil
}

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

func (j *job) keepReason(ctx context.Context) string {
	if j.rec.Way == WayNone {
		return j.target.Name + " reads the same files"
	}
	if j.blind {
		var size int64
		for _, f := range j.files {
			size += f.size
		}
		switch got := j.downloaded(); {
		case got == 0:
			return j.target.Name + " had the files before the Transfer"
		case got < size:
			return fmt.Sprintf("qui saw only %s of %s arrive on %s by BitTorrent, and %s may read the rest on %s",
				formatBytes(got), formatBytes(size), j.target.Name, j.target.Name, j.source.Name)
		}
	}
	if j.srcFiles == nil && slices.ContainsFunc(j.files, func(f *groupFile) bool { return f.onTarget }) {
		return "qui cannot reach the files on " + j.source.Name + " to remove only the files that it copied"
	}
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
	if j.srcFiles != nil {
		return j.changedOnSource(ctx)
	}
	return ""
}

func (j *job) changedOnSource(ctx context.Context) string {
	present, err := j.svc.deps.Sync.GetTorrentRefsFresh(ctx, j.source.ID, qbittorrent.TorrentRefFilter{Hashes: j.hashes()})
	if err != nil {
		return "qui cannot read the torrents on " + j.source.Name
	}
	for _, p := range j.parts {
		i := slices.IndexFunc(present, func(t *qbt.Torrent) bool { return strings.EqualFold(t.Hash, p.Hash) })
		if i < 0 {
			continue
		}
		if present[i].SavePath != p.SourceSavePath {
			return "a torrent moved on " + j.source.Name
		}
		files, err := j.svc.deps.Sync.GetTorrentFiles(qbittorrent.WithForceFilesRefresh(ctx), j.source.ID, p.Hash)
		if err != nil || files == nil {
			return "qui cannot read the names of the files on " + j.source.Name
		}
		plan, err := planFiles(p.src.info, *files)
		if err != nil || !slices.Equal(names(plan), names(p.plan)) {
			return "a file was renamed on " + j.source.Name
		}
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

func (j *job) removeSourceFiles(ctx context.Context) {
	paths := j.srcFiles.Paths()
	folders := map[string]struct{}{}
	var kept, notCopied int64
	for _, f := range j.files {
		if f.onTarget {
			notCopied += f.size
			continue
		}
		saveRoot := paths.Clean(f.owner.SourceSavePath)
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
		j.logf("Removed %s and %s files from %s. %s stay in use.", j.torrents("the"), j.its(), j.source.Name, formatBytes(kept))
		return
	}
	j.logf("Removed %s and %s files from %s.", j.torrents("the"), j.its(), j.source.Name)
}
