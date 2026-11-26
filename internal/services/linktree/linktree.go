// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package linktree

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/fsutil"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/pathutil"
)

type Request struct {
	Backend        fsops.Backend
	Config         Config
	TrackerFolder  string
	InstanceFolder string
	Torrent        Torrent
	Links          []Link
}

type Torrent struct {
	InfoHash string
	Name     string
	Files    []string
}

type Link struct {
	TorrentPath string
	SourcePath  string
}

type TreePlan struct {
	BaseDir      string
	PresetDir    string
	SavePath     string
	TorrentPaths []string

	req      Request
	baseDirs []string
	root     string
	planLost error
}

type Tree struct {
	Kind      LinkKind
	BaseDir   string
	PresetDir string
	SavePath  string
	Files     []LinkedFile

	backend fsops.Backend
	created *fsops.TreeCreateResult
}

type LinkedFile struct {
	TorrentPath string
	LinkPath    string
	Created     bool
}

func Plan(ctx context.Context, req Request) (*TreePlan, error) {
	if len(req.Links) == 0 {
		return nil, &Error{Kind: InvalidPlan, Err: errors.New("no files to link")}
	}
	paths := slices.Clone(req.Torrent.Files)
	for _, link := range req.Links {
		if link.SourcePath == "" {
			return nil, &Error{Kind: InvalidPlan, Err: errors.New("no source file for " + link.TorrentPath)}
		}
		paths = append(paths, link.TorrentPath)
	}
	for _, name := range paths {
		if err := hardlinktree.ValidateTorrentPath(req.Backend.Paths(), name); err != nil {
			return nil, &Error{Kind: InvalidPlan, Err: err}
		}
	}
	p := &TreePlan{req: req, root: commonRoot(req.Torrent.Files)}
	ranked := rankBaseDirs(req.Backend.Paths(), req.Config.BaseDirs, req.Links[0].SourcePath)
	if len(ranked) == 0 {
		return nil, &Error{Kind: NoBaseDir, Err: errors.New("no link base directory is configured")}
	}
	probe, err := p.sourceProbe(ctx)
	if err != nil {
		return nil, err
	}
	var lost, lastErr error
	for _, dir := range ranked {
		fits, err := p.onSourceFilesystem(ctx, probe, dir)
		switch {
		case errors.Is(err, fsops.ErrConnectionLost):
			lost = err
		case err != nil:
			lastErr = err
		case fits:
			p.baseDirs = append(p.baseDirs, dir)
		}
	}
	if len(p.baseDirs) == 0 {
		if lost != nil {
			return nil, &Error{Kind: ConnectionLost, Err: lost}
		}
		if lastErr == nil {
			lastErr = errors.New("no link base directory is on the filesystem of the source files")
		}
		return nil, &Error{Kind: NoBaseDir, Err: lastErr}
	}

	p.planLost = lost
	p.BaseDir = p.baseDirs[0]
	p.PresetDir = p.presetDir(p.BaseDir)
	p.SavePath = p.destination(p.PresetDir)
	for _, link := range req.Links {
		p.TorrentPaths = append(p.TorrentPaths, link.TorrentPath)
	}
	return p, nil
}

const sourceChecks = 8

func (p *TreePlan) sourceProbe(ctx context.Context) (string, error) {
	b := p.req.Backend
	d := b.Paths()
	probe := p.req.Links[0].SourcePath
	seen := map[string]struct{}{d.Dir(probe): {}}
	var others []string
	for _, link := range p.req.Links[1:] {
		folder := d.Dir(link.SourcePath)
		if _, ok := seen[folder]; !ok {
			seen[folder] = struct{}{}
			others = append(others, link.SourcePath)
		}
	}

	same := make([]bool, len(others))
	errs := make([]error, len(others))
	var g errgroup.Group
	g.SetLimit(sourceChecks)
	for i, source := range others {
		g.Go(func() error {
			same[i], errs[i] = b.SameFilesystem(ctx, probe, source)
			return nil
		})
	}
	_ = g.Wait()

	for _, err := range errs {
		if errors.Is(err, fsops.ErrConnectionLost) {
			return "", &Error{Kind: ConnectionLost, Err: err}
		}
	}
	for i, err := range errs {
		if err != nil {
			return "", &Error{Kind: NoBaseDir, Err: err}
		}
		if !same[i] {
			return "", &Error{Kind: NoBaseDir, Err: errors.New("the source files are on more than one filesystem")}
		}
	}
	return probe, nil
}

func (p *TreePlan) onSourceFilesystem(ctx context.Context, probe, dir string) (bool, error) {
	b := p.req.Backend
	d := b.Paths()
	for {
		same, err := b.SameFilesystem(ctx, probe, dir)
		if err == nil {
			return same, nil
		}
		if errors.Is(err, fsops.ErrConnectionLost) {
			return false, err
		}
		_, statErr := b.Stat(ctx, dir)
		if errors.Is(statErr, fsops.ErrConnectionLost) {
			return false, statErr
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return false, err
		}
		parent := d.Dir(dir)
		if parent == dir {
			return false, err
		}
		dir = parent
	}
}

func (p *TreePlan) presetDir(baseDir string) string {
	d := p.req.Backend.Paths()
	switch p.req.Config.Preset {
	case PresetByTracker:
		return d.Join(baseDir, pathutil.SanitizePathSegment(p.req.TrackerFolder))
	case PresetByInstance:
		return d.Join(baseDir, pathutil.SanitizePathSegment(p.req.InstanceFolder))
	default:
		return baseDir
	}
}

func (p *TreePlan) topFolder(presetDir string) string {
	d := p.req.Backend.Paths()
	if p.root != "" {
		return d.Join(presetDir, p.root)
	}
	return d.Join(presetDir, pathutil.IsolationFolder(p.req.Torrent.Name))
}

func (p *TreePlan) destination(presetDir string) string {
	if p.root != "" {
		return presetDir
	}
	return p.topFolder(presetDir)
}

func (p *TreePlan) linkPlan(rootDir string) (*hardlinktree.TreePlan, error) {
	files := make([]hardlinktree.ExistingFile, 0, len(p.req.Links))
	for _, link := range p.req.Links {
		files = append(files, hardlinktree.ExistingFile{AbsPath: link.SourcePath, RelPath: link.TorrentPath})
	}
	return hardlinktree.BuildLinkPlan(p.req.Backend.Paths(), rootDir, files)
}

func (p *TreePlan) Create(ctx context.Context) (*Tree, error) {
	lost := p.planLost
	var crossDevice, lastFailure *Error
	for _, baseDir := range p.baseDirs {
		tree, failure := p.createIn(ctx, baseDir)
		switch {
		case failure == nil:
			return tree, nil
		case failure.Kind == ConnectionLost && failure.skip:
			lost = failure.Err
		case failure.Kind == CrossDevice:
			crossDevice = failure
		case failure.skip:
			lastFailure = failure
		default:
			if lost != nil && failure.Kind != ConnectionLost {
				failure.Kind, failure.Err = ConnectionLost, errors.Join(lost, failure.Err)
			}
			return nil, failure
		}
	}
	switch {
	case lost != nil:
		return nil, &Error{Kind: ConnectionLost, Err: lost}
	case crossDevice != nil:
		return nil, crossDevice
	case lastFailure != nil:
		return nil, &Error{Kind: NoBaseDir, Err: lastFailure.Err}
	}
	return nil, &Error{Kind: NoBaseDir, Err: errors.New("no link base directory to create the tree in")}
}

func (p *TreePlan) createIn(ctx context.Context, baseDir string) (*Tree, *Error) {
	b := p.req.Backend
	d := b.Paths()
	presetDir := p.presetDir(baseDir)
	if err := b.MkdirAll(ctx, presetDir, fsutil.ContentDirMode); err != nil {
		failure := classify(err)
		failure.skip = true
		return nil, failure
	}
	if p.req.Config.Kind == Reflink {
		if failure := p.checkReflink(ctx, baseDir); failure != nil {
			return nil, failure
		}
	}

	topFolder := p.topFolder(presetDir)
	treeDir, savePath, reserved := topFolder, p.destination(presetDir), topFolder
	if err := b.Mkdir(ctx, topFolder, fsutil.ContentDirMode); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return nil, classify(err)
		}
		treeDir = d.Join(presetDir, pathutil.HashedIsolationFolder(p.req.Torrent.InfoHash, p.req.Torrent.Name))
		savePath, reserved = treeDir, ""
	}

	plan, err := p.linkPlan(savePath)
	if err != nil {
		return nil, p.release(ctx, reserved, &Error{Kind: InvalidPlan, Err: err})
	}
	create := b.HardlinkTree
	if p.req.Config.Kind == Reflink {
		create = b.ReflinkTree
	}
	created, err := create(ctx, plan)
	if err != nil {
		failure := classify(err)
		failure.Dir = treeDir
		failure.Partial = errors.Is(err, hardlinktree.ErrRollbackFailed)
		return nil, p.release(ctx, reserved, failure)
	}
	if reserved != "" {
		created.Dirs = append(created.Dirs, reserved)
	}
	return p.tree(baseDir, presetDir, savePath, plan, created), nil
}

func (p *TreePlan) checkReflink(ctx context.Context, baseDir string) *Error {
	supported, reason, err := p.req.Backend.SupportsReflink(ctx, baseDir)
	if errors.Is(err, fsops.ErrConnectionLost) {
		return &Error{Kind: ConnectionLost, Err: err}
	}
	if err != nil {
		reason = err.Error()
	}
	if err != nil || !supported {
		return &Error{Kind: NotSupported, Err: errors.New(reason)}
	}
	return nil
}

func (p *TreePlan) release(ctx context.Context, reserved string, failure *Error) *Error {
	if reserved != "" {
		if err := p.req.Backend.Remove(context.WithoutCancel(ctx), reserved, fsops.RemoveOptions{}); err != nil {
			failure.Err = errors.Join(failure.Err, err)
			failure.Partial, failure.Dir = true, reserved
			if errors.Is(err, fsops.ErrConnectionLost) {
				failure.Kind = ConnectionLost
			}
		}
	}
	if failure.Partial && failure.Kind == CrossDevice {
		failure.Kind = CreateFailed
	}
	return failure
}

func (p *TreePlan) tree(baseDir, presetDir, savePath string, plan *hardlinktree.TreePlan, created *fsops.TreeCreateResult) *Tree {
	d := p.req.Backend.Paths()
	made := make(map[string]struct{}, len(created.Files))
	for _, f := range created.Files {
		made[f] = struct{}{}
	}
	files := make([]LinkedFile, 0, len(p.req.Links))
	for _, link := range p.req.Links {
		linkPath := d.Join(savePath, d.FromSlash(link.TorrentPath))
		_, isNew := made[linkPath]
		files = append(files, LinkedFile{TorrentPath: link.TorrentPath, LinkPath: linkPath, Created: isNew})
	}
	return &Tree{
		Kind:      p.req.Config.Kind,
		BaseDir:   baseDir,
		PresetDir: presetDir,
		SavePath:  savePath,
		Files:     files,
		backend:   p.req.Backend,
		created:   created,
	}
}

func (t *Tree) Rollback(ctx context.Context) error {
	return t.backend.RemoveTree(context.WithoutCancel(ctx), t.created)
}

func commonRoot(files []string) string {
	var root string
	for i, name := range files {
		top, _, nested := strings.Cut(name, "/")
		if !nested || (i > 0 && top != root) {
			return ""
		}
		root = top
	}
	return root
}

func rankBaseDirs(d fsops.PathDialect, dirs []string, source string) []string {
	ranked := slices.Clone(dirs)
	slices.SortStableFunc(ranked, func(a, b string) int {
		return cmp.Compare(sharedPathComponents(d, b, source), sharedPathComponents(d, a, source))
	})
	return ranked
}

func sharedPathComponents(d fsops.PathDialect, a, b string) int {
	aParts := strings.Split(d.Clean(a), d.Separator())
	bParts := strings.Split(d.Clean(b), d.Separator())
	n := 0
	for n < len(aParts) && n < len(bParts) && aParts[n] == bParts[n] {
		n++
	}
	return n
}
