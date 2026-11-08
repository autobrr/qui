// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

// fakeRemoteHost is an instance's own backend: its dialect, and every call it took.
// Any method it does not define panics on the nil embedded Backend.
type fakeRemoteHost struct {
	fsops.Backend
	d           fsops.PathDialect
	hardlinkErr error
	calls       int
	plans       []*hardlinktree.TreePlan
	removed     []*fsops.TreeCreateResult
}

func (h *fakeRemoteHost) Paths() fsops.PathDialect { return h.d }

func (h *fakeRemoteHost) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	h.calls++
	return &fsops.LstatInfo{Path: p, IsDir: true}, nil
}

func (h *fakeRemoteHost) MkdirAll(context.Context, string, fs.FileMode) error {
	h.calls++
	return nil
}

func (h *fakeRemoteHost) Mkdir(context.Context, string, fs.FileMode) error {
	h.calls++
	return nil
}

func (h *fakeRemoteHost) Remove(context.Context, string, fsops.RemoveOptions) error {
	h.calls++
	return nil
}

func (h *fakeRemoteHost) SameFilesystem(context.Context, string, string) (bool, error) {
	h.calls++
	return true, nil
}

func (h *fakeRemoteHost) HardlinkTree(_ context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	h.calls++
	if h.hardlinkErr != nil {
		return nil, h.hardlinkErr
	}
	h.plans = append(h.plans, plan)
	return &fsops.TreeCreateResult{Created: len(plan.Files), Dirs: []string{plan.RootDir}}, nil
}

func (h *fakeRemoteHost) RemoveTree(_ context.Context, created *fsops.TreeCreateResult) error {
	h.calls++
	h.removed = append(h.removed, created)
	return nil
}

// remoteInjectRequest is a full match of one file in a folder that keeps the torrent's own names.
func remoteInjectRequest(d fsops.PathDialect, scanInstance *models.Instance) *InjectRequest {
	const folder = "Synthetic.Film.2024.1080p.WEB.x264-GRP"
	searcheePath := dialectPath(d, "/data/lib/"+folder)
	source := &ScannedFile{Path: d.Join(searcheePath, "film.mkv"), RelPath: "film.mkv", Size: 4}
	torrentFile := TorrentFile{Path: folder + "/film.mkv", Size: 4}
	return &InjectRequest{
		InstanceID:    1,
		ScanInstance:  scanInstance,
		TorrentBytes:  []byte("x"),
		ParsedTorrent: &ParsedTorrent{Name: folder, InfoHash: "deadbeef", Files: []TorrentFile{torrentFile}, PieceLength: 16384},
		Searchee:      &Searchee{Name: folder, Path: searcheePath, Files: []*ScannedFile{source}},
		MatchResult: &MatchResult{
			MatchedFiles:   []MatchedFilePair{{SearcheeFile: source, TorrentFile: torrentFile}},
			IsMatch:        true,
			IsPerfectMatch: true,
		},
		SearchResult: &jackett.SearchResult{Indexer: "Test"},
	}
}

func remotePoolOver(row *models.Instance, host *fakeRemoteHost, onLocal *remoteSpy) *fsops.Pool {
	return fsops.NewPoolWithRemote(&fakeInstanceStore{instance: row}, onLocal, func(*models.Instance) fsops.Backend { return host })
}

func TestInjectRegularOnRemoteInstance(t *testing.T) {
	t.Parallel()

	for _, tt := range dialects() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			row := remoteInstance(models.SSHTierSFTP)
			host := &fakeRemoteHost{d: tt.d}
			onLocal := &remoteSpy{}
			manager := &recordingTorrentManager{}
			injector := NewInjector(nil, crossseed.NewAligner(manager), testAdder(manager, (&settleFake{}).idle(), nil), nil, nil, remotePoolOver(row, host, onLocal))

			result, err := inject(t.Context(), t, injector, remoteInjectRequest(tt.d, row))

			require.NoError(t, err)
			require.True(t, result.Success)
			require.Equal(t, injectModeRegular, result.Mode)
			require.Equal(t, dialectPath(tt.d, "/data/lib"), manager.addOptions["savepath"])
			require.Positive(t, host.calls)
			require.Zero(t, onLocal.calls.Load(), "the qui host must see none of the reads")
		})
	}
}

// Without the walked row nothing could tell whether this row is still the server the searchee's paths came from.
func TestInjectRefusesARequestWithNoScanInstance(t *testing.T) {
	t.Parallel()

	row := remoteInstance(models.SSHTierSFTP)
	host := &fakeRemoteHost{d: fsops.SlashPaths}
	manager := &recordingTorrentManager{}
	injector := NewInjector(nil, crossseed.NewAligner(manager), testAdder(manager, (&settleFake{}).idle(), nil), nil, nil, remotePoolOver(row, host, &remoteSpy{}))

	// Not the inject helper, which fills in a nil ScanInstance.
	result, err := injector.Inject(t.Context(), remoteInjectRequest(fsops.SlashPaths, nil))

	require.EqualError(t, err, "inject request missing scan instance")
	require.False(t, result.Success)
	require.Zero(t, host.calls, "the request is refused before any read")
	require.Nil(t, manager.addOptions)
}

func TestInjectHardlinkTreeOnRemoteInstance(t *testing.T) {
	t.Parallel()

	noExtension := errors.New("server does not advertise hardlink@openssh.com")
	const linkRoot = "/data/links/Synthetic.Film.2024.1080p.WEB.x264-GRP"
	for _, tt := range []struct {
		name        string
		fallback    bool
		hardlinkErr error
		addErr      error
		wantMode    string
		wantSave    string
		wantErr     string
		wantRemoved bool
		wantAdded   bool
	}{
		{name: "links through the instance's backend", wantMode: injectModeHardlink, wantSave: "/data/links", wantAdded: true},
		{name: "a refused add removes the tree", addErr: errors.New("add failed"), wantMode: injectModeHardlink, wantErr: "add failed", wantRemoved: true, wantAdded: true},
		{name: "no hardlink support falls back when set", fallback: true, hardlinkErr: noExtension, wantMode: injectModeRegular, wantSave: "/data/lib", wantAdded: true},
		{name: "no hardlink support fails when fallback is off", hardlinkErr: noExtension, wantErr: noExtension.Error()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			row := remoteInstance(models.SSHTierExec)
			row.UseHardlinks = true
			row.HardlinkBaseDir = "/data/links"
			row.FallbackToRegularMode = tt.fallback
			host := &fakeRemoteHost{d: fsops.SlashPaths, hardlinkErr: tt.hardlinkErr}
			onLocal := &remoteSpy{}
			manager := &failingTorrentAdder{err: tt.addErr}
			injector := NewInjector(nil, crossseed.NewAligner(manager), testAdder(manager, (&settleFake{}).idle(), nil), nil, nil, remotePoolOver(row, host, onLocal))

			result, err := inject(t.Context(), t, injector, remoteInjectRequest(fsops.SlashPaths, row))

			require.Zero(t, onLocal.calls.Load(), "the qui host must see none of the reads")
			require.Equal(t, tt.wantAdded, manager.called)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantSave, result.SavePath)
			}
			require.Equal(t, tt.wantMode, result.Mode)
			if tt.wantMode == injectModeHardlink {
				require.Len(t, host.plans, 1)
				require.Equal(t, linkRoot+"/film.mkv", host.plans[0].Files[0].TargetPath)
				require.Equal(t, "/data/lib/Synthetic.Film.2024.1080p.WEB.x264-GRP/film.mkv", host.plans[0].Files[0].SourcePath)
			}
			if tt.wantRemoved {
				require.Len(t, host.removed, 1)
				return
			}
			require.Empty(t, host.removed)
		})
	}
}

func TestInjectRefusesChangedScanTarget(t *testing.T) {
	t.Parallel()

	localRow := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	noneRow := &models.Instance{ID: 1}
	remoteRow := remoteInstance(models.SSHTierExec)
	remoteRow.SSHUsername = "seed"
	edited := func(edit func(*models.Instance)) *models.Instance {
		row := *remoteRow
		edit(&row)
		return &row
	}
	const changed = "get filesystem backend: instance 1: remote instance changed since the scan read it"
	for _, tt := range []struct {
		name    string
		walked  *models.Instance
		current *models.Instance
		wantErr string
	}{
		{name: "local run, now remote", walked: localRow, current: remoteRow, wantErr: "get filesystem backend: instance 1 is in remote filesystem mode: instance does not have local filesystem access"},
		{name: "local run, now none", walked: localRow, current: noneRow, wantErr: "get filesystem backend: instance 1 is in none filesystem mode: instance does not have local filesystem access"},
		{name: "remote run, now local", walked: remoteRow, current: localRow, wantErr: changed},
		{name: "remote run, now none", walked: remoteRow, current: noneRow, wantErr: changed},
		{name: "other host", walked: remoteRow, current: edited(func(r *models.Instance) { r.SSHHost = "other.example.invalid" }), wantErr: changed},
		{name: "other port", walked: remoteRow, current: edited(func(r *models.Instance) { r.SSHPort = 2222 }), wantErr: changed},
		{name: "other account", walked: remoteRow, current: edited(func(r *models.Instance) { r.SSHUsername = "alt" }), wantErr: changed},
		{name: "host key replaced", walked: remoteRow, current: edited(func(r *models.Instance) { r.SSHHostKeyEncrypted = "enc-hostkey-2" }), wantErr: changed},
		{name: "private key replaced", walked: remoteRow, current: edited(func(r *models.Instance) { r.SSHKeyEncrypted = "enc-key-2" }), wantErr: changed},
		{name: "the tier alone is the same server", walked: remoteRow, current: edited(func(r *models.Instance) { r.SSHTier = models.SSHTierSFTP })},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			host := &fakeRemoteHost{d: fsops.SlashPaths}
			onLocal := &remoteSpy{}
			manager := &failingTorrentAdder{}
			injector := NewInjector(nil, crossseed.NewAligner(manager), testAdder(manager, (&settleFake{}).idle(), nil), nil, nil, remotePoolOver(tt.current, host, onLocal))

			result, err := inject(t.Context(), t, injector, remoteInjectRequest(fsops.SlashPaths, tt.walked))

			require.Zero(t, onLocal.calls.Load())
			if tt.wantErr == "" {
				require.NoError(t, err)
				require.True(t, manager.called)
				require.Positive(t, host.calls)
				return
			}
			require.EqualError(t, err, tt.wantErr)
			require.Equal(t, tt.wantErr, result.ErrorMessage)
			require.False(t, manager.called, "nothing may be added over paths read on another server")
			require.Zero(t, host.calls, "the new server must see none of the reads")
		})
	}
}

func TestValidateLinkTreeInstance(t *testing.T) {
	t.Parallel()

	withLinks := func(row models.Instance) *models.Instance {
		row.UseHardlinks = true
		row.HardlinkBaseDir = "/data/links"
		return &row
	}
	for _, tt := range []struct {
		name     string
		instance *models.Instance
		wantErr  string
	}{
		{name: "local", instance: withLinks(models.Instance{HasLocalFilesystemAccess: true})},
		{name: "remote has Write", instance: withLinks(*remoteInstance(models.SSHTierSFTP))},
		{name: "no filesystem access has no Write", instance: withLinks(models.Instance{}), wantErr: "instance does not have local filesystem access enabled"},
		{name: "no base directory", instance: &models.Instance{HasLocalFilesystemAccess: true, UseHardlinks: true}, wantErr: "hardlink base directory is not configured"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateLinkTreeInstance(tt.instance)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

// statFailingHost answers every Stat and MkdirAll with err, as SSH does while
// qBittorrent stays reachable.
type statFailingHost struct {
	*fakeRemoteHost
	err     error
	linkErr error
}

func (h *statFailingHost) Stat(context.Context, string) (*fsops.LstatInfo, error) {
	return nil, h.err
}

func (h *statFailingHost) MkdirAll(context.Context, string, fs.FileMode) error {
	if h.linkErr != nil {
		return h.linkErr
	}
	return h.err
}

// regression: #2919
func TestInjectDoesNotGuessTheLayoutWhenTheHostStopsAnswering(t *testing.T) {
	t.Parallel()

	rootlessPartial := func(req *InjectRequest) {
		have, missing := TorrentFile{Path: "film.mkv", Size: 4}, TorrentFile{Path: "extra.mkv", Size: 4}
		req.ParsedTorrent.Files = []TorrentFile{have, missing}
		req.MatchResult.MatchedFiles = []MatchedFilePair{{SearcheeFile: req.Searchee.Files[0], TorrentFile: have}}
		req.MatchResult.UnmatchedTorrentFiles = []TorrentFile{missing}
		req.MatchResult.IsPerfectMatch, req.MatchResult.IsPartialMatch = false, true
		req.DownloadMissingFiles = true
	}
	folderedPartial := func(req *InjectRequest) {
		missing := TorrentFile{Path: req.ParsedTorrent.Name + "/extra.mkv", Size: 4}
		req.ParsedTorrent.Files = append(req.ParsedTorrent.Files, missing)
		req.MatchResult.UnmatchedTorrentFiles = []TorrentFile{missing}
		req.MatchResult.IsPerfectMatch, req.MatchResult.IsPartialMatch = false, true
		req.DownloadMissingFiles = true
	}
	hardlinksWithFallback := func(row *models.Instance) {
		row.UseHardlinks, row.FallbackToRegularMode, row.HardlinkBaseDir = true, true, "/data/links"
	}

	tests := []struct {
		name    string
		statErr error
		linkErr error
		row     func(*models.Instance)
		req     func(*InjectRequest)
		wantAdd bool
	}{
		{name: "lost connection, foldered full match", statErr: fsops.ErrConnectionLost},
		{name: "lost connection, rootless partial match", statErr: fsops.ErrConnectionLost, req: rootlessPartial},
		{name: "lost connection stays in link mode", statErr: fsops.ErrConnectionLost, row: hardlinksWithFallback, req: folderedPartial},
		{name: "fallback after a link failure still reads the layout", statErr: fsops.ErrConnectionLost, linkErr: errors.New("no hardlinks here"), row: hardlinksWithFallback, req: rootlessPartial},
		{name: "denied read, foldered full match", statErr: fs.ErrPermission},
		{name: "missing path is not a directory", statErr: fs.ErrNotExist, wantAdd: true},
		{name: "path under a file is not a directory", statErr: syscall.ENOTDIR, wantAdd: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			row := remoteInstance(models.SSHTierExec)
			if tt.row != nil {
				tt.row(row)
			}
			host := &statFailingHost{fakeRemoteHost: &fakeRemoteHost{d: fsops.SlashPaths}, err: fmt.Errorf("stat: %w", tt.statErr), linkErr: tt.linkErr}
			manager := &recordingTorrentManager{}
			pool := fsops.NewPoolWithRemote(&fakeInstanceStore{instance: row}, &remoteSpy{}, func(*models.Instance) fsops.Backend { return host })
			injector := NewInjector(nil, crossseed.NewAligner(manager), testAdder(manager, (&settleFake{}).idle(), nil), nil, nil, pool)
			req := remoteInjectRequest(fsops.SlashPaths, row)
			if tt.req != nil {
				tt.req(req)
			}

			result, err := inject(t.Context(), t, injector, req)

			if tt.wantAdd {
				require.NoError(t, err)
				require.True(t, result.Success)
				require.NotEmpty(t, manager.addOptions)
				return
			}
			require.ErrorIs(t, err, tt.statErr)
			require.False(t, result.Success)
			require.Empty(t, manager.addOptions)
		})
	}
}
