// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
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
	// stepPlace holds the paths below the root of the files that were on
	// the target before the Transfer, as JSON.
	stepPlace = "place"
	// stepAdd comes before the add of a member, when its instance did not
	// have it, and stepAdded once the instance answered the add. Both hold
	// the hash of the member.
	stepAdd   = "add"
	stepAdded = "added"
	stepAfter = "after"
	// stepRemoveFromSource holds why qui keeps the files on the source,
	// empty when it removes them.
	stepRemoveFromSource = "remove from source"
	stepCleanUp          = "clean up"
)

// instanceOffError makes a Transfer wait until its instance is on again.
type instanceOffError struct{ name string }

func (e *instanceOffError) Error() string { return e.name + " is turned off." }

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
			wait = cmp.Or(wait, error(&instanceOffError{name: inst.Name}))
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

func (j *job) reachedFor(name, detail string) bool {
	return slices.ContainsFunc(j.rec.Steps, func(s Step) bool { return s.Name == name && s.Detail == detail })
}

// added reports whether the instance of the member answered its add.
func (j *job) added(p *part) bool {
	return j.reachedFor(stepAdded, p.Hash)
}

// findOnTargetAgain is findOnTarget for a Transfer that resumes. The
// temporary files are qui's own: qui writes the missing pieces into them.
// The links of an earlier check go. A file at a final name is a head start
// as in findOnTarget, but not next to a temporary file of its own.
func (j *job) findOnTargetAgain(ctx context.Context) error {
	j.made = map[*groupFile]int64{}
	headStart, placed := j.headStartFiles()
	for _, f := range j.files {
		j.remove(ctx, j.linkName(f.rel), j.linkName(f.rel)+incompleteExt)
		if placed {
			f.onTarget = slices.Contains(headStart, f.rel)
			continue
		}
		size, made, err := j.ownTemp(ctx, f)
		if err != nil {
			return err
		}
		info, err := j.root.Lstat(ctx, f.rel)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if made {
				j.made[f] = size
			}
		case made || err != nil || !info.Mode.IsRegular() || info.Size != f.size:
			return j.inTheWay(f.rel)
		default:
			f.onTarget = true
		}
	}
	j.markOnTarget()
	j.keepProven()
	return nil
}

// headStartFiles returns the files that were on the target before the
// Transfer, once qui recorded them at the step place.
func (j *job) headStartFiles() ([]string, bool) {
	var detail string
	for _, step := range j.rec.Steps {
		if step.Name == stepPlace {
			detail = step.Detail
		}
	}
	if detail == "" {
		return nil, false
	}
	var files []string
	if err := json.Unmarshal([]byte(detail), &files); err != nil {
		return nil, true
	}
	return files, true
}

func headStartDetail(files []*groupFile) string {
	onTarget := []string{}
	for _, f := range files {
		if f.onTarget {
			onTarget = append(onTarget, f.rel)
		}
	}
	detail, _ := json.Marshal(onTarget)
	return string(detail)
}

// ownTemp finds the temporary file of f, also under the .!qB name that a
// check gave it, and returns its size.
func (j *job) ownTemp(ctx context.Context, f *groupFile) (int64, bool, error) {
	temp := j.tempName(f.rel)
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

// keepProven keeps each Proven piece of a member whose bytes in the files
// that the member writes are all in their temporary files.
func (j *job) keepProven() {
	for _, p := range j.parts {
		info := p.src.info
		proven := make([]byte, (info.NumPieces()+7)/8)
		copy(proven, p.ProvenPieces)
		for piece := range info.NumPieces() {
			start := int64(piece) * info.PieceLength
			for _, s := range p.segments(start, min(info.PieceLength, info.TotalLength()-start)) {
				if !j.writes(p, s) {
					continue
				}
				if size, made := j.made[j.byRel[p.rel(p.plan[s.file])]]; !made || size < s.offset+s.n {
					proven[piece/8] &^= 0x80 >> (piece % 8)
				}
			}
		}
		p.ProvenPieces = proven
	}
}

// removeHelpers removes the helper torrents of the Transfer that are still
// on the target or the source, also those that a stop left there. It keeps
// their files.
func (j *job) removeHelpers(ctx context.Context) error {
	helpers, err := j.svc.deps.Store.HelpersOf(ctx, OwnerTransfer, j.rec.ID)
	if err != nil || len(helpers) == 0 {
		return err
	}
	for _, inst := range []*models.Instance{j.target, j.source} {
		if !j.reaches(inst) {
			continue
		}
		var hashes []string
		for _, h := range helpers {
			if h.InstanceID == inst.ID {
				hashes = append(hashes, h.Hash)
			}
		}
		if len(hashes) == 0 {
			continue
		}
		present, err := j.svc.deps.Sync.GetTorrentRefsFresh(ctx, inst.ID, qbittorrent.TorrentRefFilter{Hashes: hashes})
		if err != nil {
			return fmt.Errorf("%s: %w", inst.Name, err)
		}
		if len(present) == 0 {
			continue
		}
		gone := make([]string, 0, len(present))
		for _, t := range present {
			gone = append(gone, t.Hash)
		}
		if err := j.svc.deps.Sync.DeleteTorrents(ctx, inst.ID, gone, false, j.actor()); err != nil {
			return fmt.Errorf("%s: %w", inst.Name, err)
		}
	}
	j.helper = ""
	return nil
}

// cleanup takes back what the Transfer did, from its recorded steps: it
// removes the helper torrents, the members that qui added, and the files
// that qui wrote, and starts the source again when qui stopped it. Once the
// removal from the source started, it takes back nothing. It skips an
// instance that the run could not reach.
func (j *job) cleanup(ctx context.Context) {
	if j.reached(stepRemoveFromSource) {
		j.logf("The removal from the source started, so qui takes nothing back.")
		return
	}
	if err := j.removeHelpers(ctx); err != nil {
		j.logf("qui could not remove its helper torrents: %v", err)
	}
	if j.reaches(j.target) {
		j.cleanTarget(ctx)
	}
	if j.reaches(j.source) && (j.stoppedSource || j.reached(stepAfter) && j.rec.Options.After != AfterKeepSeeding) {
		j.startSource(ctx)
	}
}

func (j *job) cleanTarget(ctx context.Context) {
	// keepPlaced keeps the files in place for a member that qui may not have
	// added.
	keepPlaced := false
	if j.reached(stepPlace) {
		for i := range j.rec.Members {
			m := &j.rec.Members[i]
			keep, err := j.removeMember(ctx, m)
			if err != nil {
				j.logf("qui cannot remove %s, so it keeps the files on %s: %v", m.Name, j.target.Name, err)
				return
			}
			keepPlaced = keepPlaced || keep
		}
	}
	if j.rec.Way == WayBitTorrent && models.FilesystemAccessMode(j.target) == models.FilesystemModeNone {
		j.logf("qui cannot reach the files on %s, so the files of the Transfer torrent stay there.", j.target.Name)
		return
	}
	if !j.readGroup(ctx) {
		j.logf("qui cannot read the names of the files of %s, so it leaves the files that it wrote on %s.", j.torrents("the"), j.target.Name)
		return
	}
	if !j.cleanupRoot(ctx) {
		return
	}
	folders := map[string]struct{}{}
	for _, f := range j.files {
		temp := j.remove(ctx, j.tempName(f.rel), j.tempName(f.rel)+incompleteExt)
		removed := j.remove(ctx, j.linkName(f.rel), j.linkName(f.rel)+incompleteExt) || temp
		// A file whose temporary file was still there never moved into place.
		if j.reached(stepPlace) && !f.onTarget && !keepPlaced && !temp {
			removed = j.remove(ctx, f.rel) || removed
		}
		for dir := path.Dir(f.rel); removed && dir != "."; dir = path.Dir(dir) {
			folders[dir] = struct{}{}
		}
	}
	// The deepest folders go first, so that a parent is empty when its turn
	// comes. A folder that holds other files stays.
	for _, dir := range slices.SortedFunc(maps.Keys(folders), func(a, b string) int { return strings.Count(b, "/") - strings.Count(a, "/") }) {
		_ = j.root.Remove(ctx, dir)
	}
}

// removeMember removes a member that qui added from the instance where it
// landed. It reports keep for a member there that qui may not have added.
func (j *job) removeMember(ctx context.Context, m *Member) (keep bool, err error) {
	inst := j.target
	if m.TargetInstanceID != j.target.ID {
		if j.crossSeed == nil {
			if j.crossSeed, err = j.svc.instance(ctx, m.TargetInstanceID); err != nil {
				return false, err
			}
		}
		inst = j.crossSeed
	}
	present, err := j.svc.has(ctx, inst.ID, m.Hash)
	switch {
	case err != nil:
		return false, err
	case present && j.reachedFor(stepAdded, m.Hash):
		return false, j.svc.deps.Sync.DeleteTorrents(ctx, inst.ID, []string{m.Hash}, false, j.actor())
	}
	return present, nil
}

// readGroup reads the files of the members when the run did not read them:
// on the source, or else on the target, where a member on the Cross-seed
// instance holds the files of another member.
func (j *job) readGroup(ctx context.Context) bool {
	if j.parts != nil {
		return true
	}
	targetFiles, err := j.svc.deps.Files.GetBackend(ctx, j.target.ID)
	if err != nil {
		return false
	}
	d := targetFiles.Paths()
	saves := make([]string, len(j.rec.Members))
	for i := range j.rec.Members {
		saves[i] = j.rec.Members[i].Placement.SavePath
	}
	j.rootDir = topFolder(d, saves)
	for _, inst := range []*models.Instance{j.source, j.target} {
		if !j.reaches(inst) {
			continue
		}
		if parts := j.partsOn(ctx, inst, d); parts != nil {
			j.parts = parts
			break
		}
	}
	if j.parts == nil {
		return false
	}
	if j.files, j.byRel, err = groupFiles(j.parts); err != nil {
		j.parts = nil
		return false
	}
	headStart, _ := j.headStartFiles()
	for _, f := range j.files {
		f.onTarget = slices.Contains(headStart, f.rel)
	}
	j.markOnTarget()
	return true
}

func (j *job) partsOn(ctx context.Context, inst *models.Instance, d fsops.PathDialect) []*part {
	var parts []*part
	for i := range j.rec.Members {
		m := &j.rec.Members[i]
		src, err := j.svc.readSource(ctx, inst.ID, m.Hash)
		if err != nil {
			return nil
		}
		if src == nil {
			if inst == j.target && m.TargetInstanceID != j.target.ID {
				continue
			}
			return nil
		}
		p := &part{Member: m, position: i, src: src}
		if p.plan, err = planFiles(src.info, src.files); err != nil {
			return nil
		}
		rel, err := d.Rel(j.rootDir, d.Clean(m.Placement.SavePath))
		if err != nil {
			return nil
		}
		if p.dir = d.ToSlash(rel); p.dir == "." {
			p.dir = ""
		}
		parts = append(parts, p)
	}
	return parts
}

// cleanupRoot opens the root on the target when the run did not, and
// reports false when the root is not there.
func (j *job) cleanupRoot(ctx context.Context) bool {
	if j.root != nil {
		return true
	}
	files, _, err := j.svc.deps.Files.Require(ctx, j.target.ID, models.CapabilityWriteBytes)
	if err != nil {
		j.logf("qui cannot reach the files on %s, so it leaves the files that it wrote there: %v", j.target.Name, err)
		return false
	}
	if _, err := files.Lstat(ctx, j.rootDir); err != nil {
		return false
	}
	opener, ok := files.(fsops.RootOpener)
	if !ok {
		return false
	}
	if j.root, err = opener.OpenRoot(ctx, j.rootDir); err != nil {
		j.logf("qui cannot open the save path %s on %s, so it leaves the files that it wrote there: %v", j.rootDir, j.target.Name, err)
		return false
	}
	return true
}
