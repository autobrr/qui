// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// The steps that qui records before it takes them. After a stop, they say
// where the Transfer resumes, and what its cleanup takes back.
const (
	stepPrepare = "prepare"
	stepCheck   = "check on the target"
	// stepPlace holds the indexes of the files that were on the target
	// before the Transfer, as JSON.
	stepPlace = "place"
	// stepAdd comes before the add of the real torrent, when the target did
	// not have it, and stepAdded once the target answered the add.
	stepAdd   = "add"
	stepAdded = "added"
	stepAfter = "after"
	// stepRemoveFromSource holds why qui keeps the files on the source,
	// empty when it removes them.
	stepRemoveFromSource = "remove from source"
	stepCleanUp          = "clean up"
)

// instanceOff makes a Transfer wait until its instance is on again.
type instanceOff struct{ name string }

func (e *instanceOff) Error() string { return e.name + " is turned off." }

// checkInstances reads both instances of the Transfer. A deleted instance or
// a changed endpoint stops the Transfer for the user, and qui touches that
// instance no more. A turned-off instance makes the Transfer wait.
func (j *job) checkInstances(ctx context.Context) error {
	var stop, wait error
	for _, side := range []struct {
		id       int
		endpoint fsops.Endpoint
		into     **models.Instance
	}{
		{j.rec.SourceInstanceID, j.rec.SourceEndpoint, &j.source},
		{j.rec.TargetInstanceID, j.rec.TargetEndpoint, &j.target},
	} {
		inst, err := j.svc.deps.Instances.Get(ctx, side.id)
		switch {
		case errors.Is(err, models.ErrInstanceNotFound):
			j.away[side.id] = true
			stop = cmp.Or(stop, permanent("Instance %d of the Transfer was deleted. qui cleans up what it can still reach.", side.id))
			continue
		case err != nil:
			return temporary("qui cannot read instance %d: %v", side.id, err)
		case side.endpoint.Changed(inst):
			j.away[side.id] = true
			stop = cmp.Or(stop, permanent("%s changed its address, its SSH server, or its file access, so the Transfer stopped. qui cleans up nothing on %s.", inst.Name, inst.Name))
		case !inst.IsActive:
			wait = cmp.Or(wait, error(&instanceOff{name: inst.Name}))
		}
		*side.into = inst
	}
	return cmp.Or(stop, wait)
}

// reaches reports whether the cleanup may touch the instance.
func (j *job) reaches(inst *models.Instance) bool {
	return inst != nil && !j.away[inst.ID]
}

// reached reports whether the Transfer recorded the step.
func (j *job) reached(name string) bool {
	return slices.ContainsFunc(j.rec.Steps, func(s Step) bool { return s.Name == name })
}

// findOnTargetAgain is findOnTarget for a Transfer that resumes. The
// temporary files are qui's own: qui writes the missing pieces into them.
// The links of an earlier check go. A file at a final name is a head start
// as in findOnTarget, but not next to a temporary file of its own.
func (j *job) findOnTargetAgain(ctx context.Context) error {
	j.made = map[int]int64{}
	headStart, placed := j.headStartFiles()
	for i := range j.plan {
		f := &j.plan[i]
		if f.pad {
			continue
		}
		j.remove(ctx, j.linkName(f.name), j.linkName(f.name)+incompleteExt)
		if placed {
			f.onTarget = slices.Contains(headStart, i)
			continue
		}
		size, made, err := j.ownTemp(ctx, f)
		if err != nil {
			return err
		}
		info, err := j.root.Lstat(ctx, f.name)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if made {
				j.made[i] = size
			}
		case made || err != nil || !info.Mode.IsRegular() || info.Size != f.size:
			return j.inTheWay(f.name)
		default:
			f.onTarget = true
		}
	}
	j.keepProven()
	return nil
}

// headStartFiles returns the indexes of the files that were on the target
// before the Transfer, once qui recorded them at the step place.
func (j *job) headStartFiles() ([]int, bool) {
	var detail string
	for _, step := range j.rec.Steps {
		if step.Name == stepPlace {
			detail = step.Detail
		}
	}
	if detail == "" {
		return nil, false
	}
	var files []int
	if err := json.Unmarshal([]byte(detail), &files); err != nil {
		return nil, true
	}
	return files, true
}

func headStartDetail(plan []file) string {
	files := []int{}
	for i, f := range plan {
		if f.onTarget {
			files = append(files, i)
		}
	}
	detail, _ := json.Marshal(files)
	return string(detail)
}

// ownTemp finds the temporary file of f, also under the .!qB name that a
// check gave it, and returns its size.
func (j *job) ownTemp(ctx context.Context, f *file) (int64, bool, error) {
	temp := j.tempName(f.name)
	info, err := j.root.Lstat(ctx, temp)
	marked, markedErr := j.root.Lstat(ctx, temp+incompleteExt)
	switch {
	case err == nil && !errors.Is(markedErr, fs.ErrNotExist):
		return 0, false, j.inTheWay(temp + incompleteExt)
	case errors.Is(err, fs.ErrNotExist) && errors.Is(markedErr, fs.ErrNotExist):
		return 0, false, nil
	case errors.Is(err, fs.ErrNotExist):
		if markedErr != nil {
			return 0, false, j.inTheWay(temp + incompleteExt)
		}
		if err := j.root.RenameNoReplace(ctx, temp+incompleteExt, temp); err != nil {
			return 0, false, temporary("qui cannot rename %s on %s: %v", temp+incompleteExt, j.target.Name, err)
		}
		info, err = marked, nil
	}
	if err != nil || !info.Mode.IsRegular() || info.Size > f.size {
		return 0, false, j.inTheWay(temp)
	}
	return info.Size, true, nil
}

// keepProven keeps each Proven piece whose bytes in the files that qui
// writes are all in its temporary files.
func (j *job) keepProven() {
	info := j.src.info
	proven := make([]byte, (info.NumPieces()+7)/8)
	copy(proven, j.member().ProvenPieces)
	for piece := range info.NumPieces() {
		start := int64(piece) * info.PieceLength
		for _, s := range j.segments(start, min(info.PieceLength, info.TotalLength()-start)) {
			if size, made := j.made[s.file]; j.writes(s.file) && (!made || size < s.offset+s.n) {
				proven[piece/8] &^= 0x80 >> (piece % 8)
			}
		}
	}
	j.member().ProvenPieces = proven
}

// removeHelpers removes the helper torrents of the Transfer that are still
// on the target, also those that a stop left there.
func (j *job) removeHelpers(ctx context.Context) error {
	helpers, err := j.svc.deps.Store.HelpersOf(ctx, OwnerTransfer, j.rec.ID)
	if err != nil || len(helpers) == 0 {
		return err
	}
	hashes := make([]string, 0, len(helpers))
	for _, h := range helpers {
		if h.InstanceID == j.target.ID {
			hashes = append(hashes, h.Hash)
		}
	}
	present, err := j.svc.deps.Sync.GetTorrentRefsFresh(ctx, j.target.ID, qbittorrent.TorrentRefFilter{Hashes: hashes})
	if err != nil || len(present) == 0 {
		return err
	}
	gone := make([]string, 0, len(present))
	for _, t := range present {
		gone = append(gone, t.Hash)
	}
	return j.svc.deps.Sync.DeleteTorrents(ctx, j.target.ID, gone, false, j.actor())
}

// cleanup takes back what the Transfer did, from its recorded steps: it
// removes the helper torrents, the real torrent when qui added it, and the
// files that qui wrote, and starts the source again when qui stopped it.
// Once the removal from the source started, it takes back nothing. It skips
// an instance that the run could not reach.
func (j *job) cleanup(ctx context.Context) {
	if j.reached(stepRemoveFromSource) {
		j.logf("The removal of the torrent from the source started, so qui takes nothing back.")
		return
	}
	if j.reaches(j.target) {
		j.cleanTarget(ctx)
	}
	if j.reaches(j.source) && (j.stoppedSource || j.reached(stepAfter) && j.rec.Options.After != AfterKeepSeeding) {
		j.startSource(ctx)
	}
}

func (j *job) cleanTarget(ctx context.Context) {
	if err := j.removeHelpers(ctx); err != nil {
		j.logf("qui could not remove its helper torrents from %s: %v", j.target.Name, err)
	}
	planned := j.readPlan(ctx)
	if !planned {
		j.logf("qui cannot read the names of the files of the torrent, so it leaves the files that it wrote on %s.", j.target.Name)
	}
	// keepPlaced keeps the files in place for a torrent on the target that
	// qui may not have added.
	keepPlaced := false
	if j.reached(stepPlace) {
		present, err := j.svc.has(ctx, j.target.ID, j.hash())
		switch {
		case err != nil:
			j.logf("qui cannot read the torrents of %s, so it keeps the files there: %v", j.target.Name, err)
			return
		case present && j.reached(stepAdded):
			if err := j.svc.deps.Sync.DeleteTorrents(ctx, j.target.ID, []string{j.hash()}, false, j.actor()); err != nil {
				j.logf("qui could not remove the torrent from %s, so it keeps its files: %v", j.target.Name, err)
				return
			}
		case present:
			keepPlaced = true
		}
	}
	if !planned || !j.cleanupRoot(ctx) {
		return
	}
	folders := map[string]struct{}{}
	for _, f := range j.plan {
		if f.pad {
			continue
		}
		temp := j.remove(ctx, j.tempName(f.name), j.tempName(f.name)+incompleteExt)
		removed := j.remove(ctx, j.linkName(f.name), j.linkName(f.name)+incompleteExt) || temp
		// A file whose temporary file was still there never moved into place.
		if j.reached(stepPlace) && !f.onTarget && !keepPlaced && !temp {
			removed = j.remove(ctx, f.name) || removed
		}
		for dir := path.Dir(f.name); removed && dir != "."; dir = path.Dir(dir) {
			folders[dir] = struct{}{}
		}
	}
	// The deepest folders go first, so that a parent is empty when its turn
	// comes. A folder that holds other files stays.
	for _, dir := range slices.SortedFunc(maps.Keys(folders), func(a, b string) int { return strings.Count(b, "/") - strings.Count(a, "/") }) {
		_ = j.root.Remove(ctx, dir)
	}
}

// readPlan reads the names of the files when the run did not read them: on
// the source, or else on the target.
func (j *job) readPlan(ctx context.Context) bool {
	if j.plan != nil {
		return true
	}
	for _, inst := range []*models.Instance{j.source, j.target} {
		if !j.reaches(inst) {
			continue
		}
		src, err := j.svc.readSource(ctx, inst.ID, j.hash())
		if err != nil || src == nil {
			continue
		}
		plan, err := planFiles(src.info, src.files)
		if err != nil {
			continue
		}
		headStart, _ := j.headStartFiles()
		for _, i := range headStart {
			if i < len(plan) {
				plan[i].onTarget = true
			}
		}
		j.plan = plan
		return true
	}
	return false
}

// cleanupRoot opens the save path on the target when the run did not, and
// reports false when the save path is not there.
func (j *job) cleanupRoot(ctx context.Context) bool {
	if j.root != nil {
		return true
	}
	files, _, err := j.svc.deps.Files.Require(ctx, j.target.ID, models.CapabilityWriteBytes)
	if err != nil {
		j.logf("qui cannot reach the files on %s, so it leaves the files that it wrote there: %v", j.target.Name, err)
		return false
	}
	if _, err := files.Lstat(ctx, j.savePath()); err != nil {
		return false
	}
	opener, ok := files.(fsops.RootOpener)
	if !ok {
		return false
	}
	if j.root, err = opener.OpenRoot(ctx, j.savePath()); err != nil {
		j.logf("qui cannot open the save path %s on %s, so it leaves the files that it wrote there: %v", j.savePath(), j.target.Name, err)
		return false
	}
	return true
}
