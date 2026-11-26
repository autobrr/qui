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

const (
	stepPrepare          = "prepare"
	stepCheck            = "check on the target"
	stepOwnCopy          = "copy a hardlink"
	stepNames            = "names"
	stepPlace            = "place"
	stepAdd              = "add"
	stepAdded            = "added"
	stepRename           = "rename"
	stepAfter            = "after"
	stepRemoveFromSource = "remove from source"
	stepCleanUp          = "clean up"
	stepRemovePlaced     = "remove the placed files"
)

type instanceOffError struct{ name string }

func (e *instanceOffError) Error() string { return e.name + " is turned off." }

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

func (j *job) reaches(inst *models.Instance) bool {
	return inst != nil && !j.away[inst.ID]
}

func (j *job) reached(name string) bool {
	return slices.ContainsFunc(j.rec.Steps, func(s Step) bool { return s.Name == name })
}

func (j *job) reachedFor(name, detail string) bool {
	return slices.ContainsFunc(j.rec.Steps, func(s Step) bool { return s.Name == name && s.Detail == detail })
}

func (j *job) added(p *part) bool {
	return j.reachedFor(stepAdded, p.Hash)
}

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
		case err != nil:
			return cannotLook(j.target.Name, err)
		case made || !info.Mode.IsRegular() || info.Size != f.size:
			return j.inTheWay(f.rel)
		default:
			f.onTarget = true
		}
	}
	j.relink(ctx)
	j.markOnTarget()
	j.keepProven()
	return nil
}

func (j *job) stoppedByQui() []string {
	var hashes []string
	for _, step := range j.rec.Steps {
		var running []string
		if step.Name != stepAfter || json.Unmarshal([]byte(step.Detail), &running) != nil {
			continue
		}
		for _, hash := range running {
			if !slices.Contains(hashes, hash) {
				hashes = append(hashes, hash)
			}
		}
	}
	return hashes
}

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

func (j *job) recordNames(ctx context.Context) error {
	if j.reached(stepNames) {
		return nil
	}
	recorded := make([][]string, len(j.parts))
	for i, p := range j.parts {
		recorded[i] = names(p.plan)
	}
	detail, _ := json.Marshal(recorded)
	return j.stepWith(ctx, StateCopying, stepNames, string(detail))
}

func (j *job) useRecordedNames(parts []*part) (renamed bool) {
	i := slices.IndexFunc(j.rec.Steps, func(s Step) bool { return s.Name == stepNames })
	var recorded [][]string
	if i < 0 || json.Unmarshal([]byte(j.rec.Steps[i].Detail), &recorded) != nil {
		return false
	}
	for _, p := range parts {
		old := recorded[p.position]
		for k := range p.plan {
			if !p.plan[k].pad && len(old) > 0 {
				renamed = renamed || p.plan[k].name != old[0]
				p.plan[k].name, old = old[0], old[1:]
			}
		}
	}
	return renamed
}

func (j *job) ownTemp(ctx context.Context, f *groupFile) (int64, bool, error) {
	temp := j.tempName(f.rel)
	info, err := j.root.Lstat(ctx, temp)
	marked, markedErr := j.root.Lstat(ctx, temp+incompleteExt)
	for _, e := range []error{err, markedErr} {
		if e != nil && !errors.Is(e, fs.ErrNotExist) {
			return 0, false, cannotLook(j.target.Name, e)
		}
	}
	switch {
	case err == nil && !errors.Is(markedErr, fs.ErrNotExist):
		return 0, false, j.inTheWay(temp + incompleteExt)
	case errors.Is(err, fs.ErrNotExist) && errors.Is(markedErr, fs.ErrNotExist):
		return 0, false, nil
	case errors.Is(err, fs.ErrNotExist):
		if err := j.root.RenameNoReplace(ctx, temp+incompleteExt, temp); err != nil {
			return 0, false, temporary("qui cannot rename %s on %s: %v", temp+incompleteExt, j.target.Name, err)
		}
		info = marked
	}
	if !info.Mode.IsRegular() || info.Size > f.size {
		return 0, false, j.inTheWay(temp)
	}
	return info.Size, true, nil
}

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
	if j.reaches(j.source) {
		j.startSource(ctx)
	}
}

func (j *job) cleanTarget(ctx context.Context) {
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
	placed, err := j.placedFiles(ctx)
	if err != nil {
		j.logf("qui cannot tell which files it placed on %s, so it keeps the files that it wrote there: %v", j.target.Name, err)
		return
	}
	folders := map[string]struct{}{}
	for _, f := range j.files {
		j.remove(ctx, j.tempName(f.rel), j.tempName(f.rel)+incompleteExt)
		j.remove(ctx, j.linkName(f.rel), j.linkName(f.rel)+incompleteExt)
		if !keepPlaced && slices.Contains(placed, f.rel) {
			j.remove(ctx, f.rel)
		}
		for dir := path.Dir(f.rel); dir != "."; dir = path.Dir(dir) {
			folders[dir] = struct{}{}
		}
	}
	for _, dir := range slices.SortedFunc(maps.Keys(folders), func(a, b string) int { return strings.Count(b, "/") - strings.Count(a, "/") }) {
		_ = j.root.Remove(ctx, dir)
	}
}

func (j *job) placedFiles(ctx context.Context) ([]string, error) {
	if !j.reached(stepPlace) {
		return nil, nil
	}
	if i := slices.IndexFunc(j.rec.Steps, func(s Step) bool { return s.Name == stepRemovePlaced }); i >= 0 {
		var placed []string
		err := json.Unmarshal([]byte(j.rec.Steps[i].Detail), &placed)
		return placed, err
	}
	headStart, _ := j.headStartFiles()
	placed := []string{}
	for _, f := range j.files {
		if !slices.Contains(headStart, f.rel) && j.absent(ctx, j.tempName(f.rel), j.tempName(f.rel)+incompleteExt) {
			placed = append(placed, f.rel)
		}
	}
	detail, _ := json.Marshal(placed)
	j.rec.Steps = append(j.rec.Steps, Step{Name: stepRemovePlaced, At: j.svc.deps.Now(), Detail: string(detail)})
	if err := j.svc.deps.Store.Save(ctx, j.rec); err != nil {
		return nil, err
	}
	return placed, nil
}

func (j *job) absent(ctx context.Context, names ...string) bool {
	for _, name := range names {
		if _, err := j.root.Lstat(ctx, name); !errors.Is(err, fs.ErrNotExist) {
			return false
		}
	}
	return true
}

func (j *job) removeMember(ctx context.Context, m *Member) (keep bool, err error) {
	inst := j.target
	if m.TargetInstanceID != j.target.ID {
		if j.crossSeed, err = j.svc.instance(ctx, m.TargetInstanceID); err != nil {
			return false, err
		}
		inst = j.crossSeed
		if m.Placement.Endpoint.Changed(inst) {
			j.away[inst.ID] = true
		}
		if !j.reaches(inst) {
			return true, nil
		}
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

func (j *job) readGroup(ctx context.Context) bool {
	if j.parts != nil {
		return true
	}
	targetFiles, err := j.svc.deps.Files.GetBackend(ctx, j.target.ID)
	if err != nil {
		return false
	}
	d := pathsOf(j.target, targetFiles, j.rec.Members[0].Placement.SavePath)
	saves := make([]string, len(j.rec.Members))
	for i := range j.rec.Members {
		saves[i] = j.rec.Members[i].Placement.SavePath
	}
	j.rootDir, j.rootPaths = topFolder(d, saves), d
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
	j.useRecordedNames(j.parts)
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
