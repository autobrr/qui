// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/remote"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

// crossSeedOf builds a torrent of the same files as s with another piece
// size, as a cross-seed from another tracker has, and so another infohash.
func crossSeedOf(t *testing.T, s show) (data []byte, hash string) {
	t.Helper()
	return qbtstub.NewContentTorrent(t, showName, 32,
		qbtstub.ContentFile{Path: "Show.S01E01.mkv", Data: s.files["Show.S01E01.mkv"]},
		qbtstub.ContentFile{Path: "Show.S01E02.mkv", Data: s.files["Show.S01E02.mkv"]})
}

// seeding reports whether the instance seeds the torrent.
func (r *rig) seeding(instanceID int, hash string) bool {
	r.t.Helper()
	torrents, err := r.sync.GetTorrentRefsFresh(r.t.Context(), instanceID, qbittorrent.TorrentRefFilter{Hashes: []string{hash}})
	require.NoError(r.t, err)
	return len(torrents) == 1 && string(torrents[0].State) == "stalledUP"
}

type routes map[int]*models.CrossSeedRoute

func (r routes) Get(_ context.Context, sourceInstanceID int) (*models.CrossSeedRoute, error) {
	return r[sourceInstanceID], nil
}

// crossSeeds stands in for the routed cross-seed add: it links the files of
// the matched torrent into the folder of the Cross-seed instance, and adds
// the torrent there stopped, as the add leaves it after its check.
type crossSeeds struct {
	r        *rig
	inst     *models.Instance
	stub     *qbtstub.Server
	dir      string
	requests []crossseed.CrossSeedRequest
	// refuse is the status of a refused add.
	refuse string
}

// withCrossSeedInstance gives the target a Cross-seed instance.
func (r *rig) withCrossSeedInstance() *crossSeeds {
	r.t.Helper()
	c := &crossSeeds{r: r, dir: r.t.TempDir()}
	c.stub = qbtstub.New(r.t, c.dir)
	inst, err := r.instances.Create(r.t.Context(), "homelab cross-seed", c.stub.URL, "", "", nil, nil, false, new(true))
	require.NoError(r.t, err)
	c.inst = inst
	r.deps.Routes = routes{r.target.ID: {SourceInstanceID: r.target.ID, CrossSeedInstanceID: inst.ID}}
	r.deps.CrossSeeds = c
	closeService(r.t, r.svc)
	svc := transfer.NewService(r.t.Context(), r.deps)
	r.svc = svc
	r.t.Cleanup(func() { closeService(r.t, svc) })
	return c
}

func (c *crossSeeds) CrossSeed(ctx context.Context, req *crossseed.CrossSeedRequest) (*crossseed.CrossSeedResponse, error) {
	c.requests = append(c.requests, *req)
	matchedOn := req.TargetInstanceIDs[0]
	if c.refuse != "" {
		return &crossseed.CrossSeedResponse{Results: []crossseed.InstanceCrossSeedResult{{InstanceID: matchedOn, Status: c.refuse}}}, nil
	}
	data, err := base64.StdEncoding.DecodeString(req.TorrentData)
	if err != nil {
		return nil, err
	}
	matched, err := c.r.sync.GetTorrentRefsFresh(ctx, matchedOn, qbittorrent.TorrentRefFilter{Hashes: []string{req.ManualTargetHash}})
	if err != nil {
		return nil, err
	}
	if len(matched) != 1 {
		return nil, fmt.Errorf("no matched torrent %s", req.ManualTargetHash)
	}
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, err
	}
	for _, f := range info.UpvertedFiles() {
		rel := filepath.Join(append([]string{info.Name}, f.Path...)...)
		if err := os.MkdirAll(filepath.Dir(filepath.Join(c.dir, rel)), 0o755); err != nil {
			return nil, err
		}
		if err := os.Link(filepath.Join(matched[0].SavePath, rel), filepath.Join(c.dir, rel)); err != nil {
			return nil, err
		}
	}
	hash := c.stub.Seed(data, c.dir)
	if err := c.r.sync.BulkAction(ctx, c.inst.ID, []string{hash}, "pause"); err != nil {
		return nil, err
	}
	return &crossseed.CrossSeedResponse{Success: true, Results: []crossseed.InstanceCrossSeedResult{
		{InstanceID: matchedOn, Success: true, Status: "added", CrossSeedInstanceID: c.inst.ID},
	}}, nil
}

// At a target with a Cross-seed instance, a group of the same files splits:
// the Group representative lands on the target, and the other torrent on the
// Cross-seed instance, through the routed cross-seed add.
func TestTransferSendsTheCrossSeedToTheCrossSeedInstance(t *testing.T) {
	r := newRig(t)
	cs := r.withCrossSeedInstance()
	s := newShow(t, showName)
	r.seed(s)
	crossSeed, crossSeedHash := crossSeedOf(t, s)
	r.src.Seed(crossSeed, r.srcPath())
	r.src.Edit(s.hash, func(t *qbtstub.Torrent) { t.AddedOn = 200 })
	r.src.Edit(crossSeedHash, func(t *qbtstub.Torrent) { t.AddedOn = 100 })

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Contains(t, logOf(record), "Proof: the check on homelab passed all 3 pieces of "+showName+".", "qui read no piece of the representative")
	require.True(t, r.seeding(r.target.ID, crossSeedHash), "the older torrent is the Group representative")
	require.False(t, r.dst.Has(s.hash))
	require.True(t, r.seeding(cs.inst.ID, s.hash))
	require.Len(t, cs.requests, 1)
	require.Equal(t, crossSeedHash, cs.requests[0].ManualTargetHash)
	require.Equal(t, []int{r.target.ID}, cs.requests[0].TargetInstanceIDs)
	require.True(t, cs.requests[0].SkipAutoResume)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.Equal(t, contents(s, showName), tree(t, cs.dir))
	require.False(t, r.src.Has(s.hash))
	require.False(t, r.src.Has(crossSeedHash))
	require.Empty(t, tree(t, r.srcDir))
}

// checkingCrossSeeds is the routed cross-seed add as crossseed runs it with
// SkipAutoResume: the add sends the recheck and returns while qBittorrent
// checks the links.
type checkingCrossSeeds struct {
	*crossSeeds
	hash string
}

func (c checkingCrossSeeds) CrossSeed(ctx context.Context, req *crossseed.CrossSeedRequest) (*crossseed.CrossSeedResponse, error) {
	res, err := c.crossSeeds.CrossSeed(ctx, req)
	if err != nil {
		return nil, err
	}
	return res, c.r.sync.BulkAction(ctx, c.inst.ID, []string{c.hash}, "recheck")
}

// The routed cross-seed add returns while qBittorrent checks the cross-seed.
// qui waits for that check before it starts the cross-seed or ends the
// Transfer, also when the check takes longer than a start.
//
// regression: #4231
func TestTransferWaitsForTheCheckOfARoutedCrossSeed(t *testing.T) {
	for _, tc := range []struct {
		name         string
		leaveStopped bool
		state        string
	}{
		{name: "start on the target", state: "stalledUP"},
		{name: "leave stopped on the target", leaveStopped: true, state: "stoppedUP"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, func(c *transfer.Config) { c.CheckPolls = 100 })
			cs := r.withCrossSeedInstance()
			s := newShow(t, showName)
			r.seed(s)
			crossSeed, crossSeedHash := crossSeedOf(t, s)
			r.src.Seed(crossSeed, r.srcPath())
			r.src.Edit(s.hash, func(t *qbtstub.Torrent) { t.AddedOn = 200 })
			r.src.Edit(crossSeedHash, func(t *qbtstub.Torrent) { t.AddedOn = 100 })
			cs.stub.CheckSyncs = 40
			r.deps.CrossSeeds = checkingCrossSeeds{crossSeeds: cs, hash: s.hash}
			r.restart()
			req := r.request(s)
			req.Options.LeaveStopped = tc.leaveStopped

			record := r.run(req)

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			torrents, err := r.sync.GetTorrentRefsFresh(t.Context(), cs.inst.ID, qbittorrent.TorrentRefFilter{Hashes: []string{s.hash}})
			require.NoError(t, err)
			require.Len(t, torrents, 1)
			require.Equal(t, tc.state, string(torrents[0].State), "the check ended before the Transfer")
			require.False(t, r.src.Has(s.hash))
		})
	}
}

// A refused cross-seed add fails the Transfer. The source keeps both
// torrents and their files.
func TestTransferFailsWhenTheCrossSeedAddFails(t *testing.T) {
	r := newRig(t)
	cs := r.withCrossSeedInstance()
	cs.refuse = "no_match"
	s := newShow(t, showName)
	r.seed(s)
	crossSeed, crossSeedHash := crossSeedOf(t, s)
	r.src.Seed(crossSeed, r.srcPath())
	r.src.Edit(crossSeedHash, func(t *qbtstub.Torrent) { t.AddedOn = 100 })
	r.src.Edit(s.hash, func(t *qbtstub.Torrent) { t.AddedOn = 200 })

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, "homelab cross-seed did not take "+showName+": no_match", record.FailureReason)
	require.True(t, r.seeding(r.source.ID, s.hash))
	require.True(t, r.seeding(r.source.ID, crossSeedHash))
	require.Equal(t, contents(s, showName), tree(t, r.srcDir))
}

// A group whose torrents hold other files goes whole to the target, also
// when the target has a Cross-seed instance. The request is the middle
// episode of a pack: the pack copies only its other files, and the check on
// the target proves the pieces that qui did not read.
func TestTransferMovesAMixedGroupWholeToTheTarget(t *testing.T) {
	r := newRig(t)
	cs := r.withCrossSeedInstance()
	pack := newPack(t)
	r.seed(pack)
	episode, episodeHash := singleFileTorrent(t, episode2, pack.files[episode2])
	r.src.Seed(episode, r.srcPath(showName))
	r.disk.free = 106

	record := r.run(transfer.Request{SourceInstanceID: r.source.ID, Hash: episodeHash, TargetInstanceID: r.target.ID})

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Len(t, record.Members, 2)
	require.Empty(t, cs.requests)
	require.Contains(t, logOf(record), "Proof: the check on homelab passed all 7 pieces of "+showName+".")
	require.True(t, r.seeding(r.target.ID, episodeHash))
	require.True(t, r.seeding(r.target.ID, pack.hash))
	require.Equal(t, contents(pack, showName), tree(t, r.dstDir))
	require.False(t, r.src.Has(pack.hash))
	require.False(t, r.src.Has(episodeHash))
	require.Empty(t, tree(t, r.srcDir))
}

// seedHardlinkedCrossSeed seeds a cross-seed of s on the source that reads
// hardlinks to the files of s in the folder links.
func (r *rig) seedHardlinkedCrossSeed(s show) (links, hash string) {
	r.t.Helper()
	links = filepath.Join(r.srcDir, "links")
	require.NoError(r.t, os.MkdirAll(filepath.Join(links, showName), 0o755))
	for name := range s.files {
		require.NoError(r.t, os.Link(filepath.Join(r.srcDir, showName, name), filepath.Join(links, showName, name)))
	}
	crossSeed, hash := crossSeedOf(r.t, s)
	r.src.Seed(crossSeed, r.srcPath("links"))
	return links, hash
}

// Over SSH qui does not see a hardlink, so a cross-seed on links to the
// files stays on the source and keeps its data.
func TestTransferLeavesAHardlinkedCrossSeedOverSSH(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	links, crossSeedHash := r.seedHardlinkedCrossSeed(s)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Len(t, record.Members, 1)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.True(t, r.seeding(r.source.ID, crossSeedHash))
	require.Equal(t, contents(s, showName), tree(t, links))
}

// On qui's own disk, qui sees that a cross-seed reads the files through
// hardlinks, so the cross-seed moves with the torrent. qui copies each file
// once and links the path of the cross-seed to the copy where the target can
// link, so the free space for one copy is enough. On one filesystem, both
// paths link to the source. A target that cannot link gets a copy of each
// path.
func TestTransferMovesAHardlinkedCrossSeed(t *testing.T) {
	const linkedLog = "2 files (70 B) are hardlinks of other files on seedbox, so qui copies them once and links them on homelab."
	for _, tc := range []struct {
		name            string
		target          side
		oneFilesystem   bool
		noLinks         bool
		free            int64
		after           transfer.After
		log             string
		writtenFolders  []string
		oneFileOnTarget bool
	}{
		{name: "a copy linked over SFTP", target: overSFTP, log: linkedLog,
			writtenFolders: []string{showName}, oneFileOnTarget: true},
		{name: "links to the source on one filesystem", target: onDisk, oneFilesystem: true,
			log: "Linking 4 files (140 B) on qui's own disk from seedbox to homelab.", oneFileOnTarget: true},
		{name: "a copy of each path where the target cannot link", target: onDisk, noLinks: true, free: 140,
			log:            "qui cannot link links/" + showName + "/Show.S01E01.mkv on homelab, so it copies the file",
			writtenFolders: []string{showName, "links/" + showName}},
		{name: "the kept files count once", target: onDisk, free: 70, after: transfer.AfterKeepFiles,
			log: "Removed the 2 torrents from seedbox and kept their files: 70 B stay in use.", writtenFolders: []string{showName}, oneFileOnTarget: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, onDisk, tc.target)
			r.disk.otherFilesystem = !tc.oneFilesystem
			r.disk.noLinks = tc.noLinks
			r.disk.free = tc.free
			s := newShow(t, showName)
			r.seed(s)
			_, crossSeedHash := r.seedHardlinkedCrossSeed(s)
			sourceFiles := tree(t, r.srcDir)
			req := r.request(s)
			req.Options.After = tc.after

			record := r.run(req)

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Len(t, record.Members, 2)
			require.Contains(t, logOf(record), tc.log)
			written := map[string]bool{}
			for _, folder := range tc.writtenFolders {
				written[folder] = true
			}
			require.Equal(t, written, r.writtenFolders())
			require.Equal(t, sourceFiles, tree(t, r.dstDir))
			for name := range s.files {
				require.Equal(t, tc.oneFileOnTarget, sameFile(t, filepath.Join(r.dstDir, showName, name), filepath.Join(r.dstDir, "links", showName, name)), name)
			}
			require.True(t, r.seeding(r.target.ID, s.hash))
			require.True(t, r.seeding(r.target.ID, crossSeedHash))
			require.False(t, r.src.Has(s.hash))
			require.False(t, r.src.Has(crossSeedHash))
			if tc.after == transfer.AfterKeepFiles {
				require.Equal(t, sourceFiles, tree(t, r.srcDir))
			} else {
				require.Empty(t, tree(t, r.srcDir))
			}
		})
	}
}

// A target that cannot link needs the free space for a second copy of a
// hardlinked file. Without it, the Transfer stops, and qui removes what it
// wrote.
func TestTransferNeedsSpaceForACopyThatTheTargetCannotLink(t *testing.T) {
	r := newRigOn(t, onDisk, onDisk)
	r.disk.otherFilesystem = true
	r.disk.noLinks = true
	r.disk.free = 100
	s := newShow(t, showName)
	r.seed(s)
	_, crossSeedHash := r.seedHardlinkedCrossSeed(s)
	sourceFiles := tree(t, r.srcDir)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, transfer.FailurePermanent, record.FailureClass)
	require.Equal(t, "Not enough free space on homelab: not enough free space on the target: the Transfer needs 40 B, and 30 B is free", record.FailureReason)
	require.Empty(t, tree(t, r.dstDir))
	// regression: #4231
	entries, err := os.ReadDir(r.dstDir)
	require.NoError(t, err)
	require.Empty(t, entries, "the folders of the link that failed stay")
	require.Equal(t, sourceFiles, tree(t, r.srcDir))
	require.True(t, r.seeding(r.source.ID, s.hash))
	require.True(t, r.seeding(r.source.ID, crossSeedHash))
}

// A Transfer that resumes keeps the temporary files that it made for a
// hardlinked cross-seed: the links, or the copies where the target cannot
// link. It creates each temporary file once.
func TestTransferKeepsTheFilesOfAHardlinkedCrossSeedOverARestart(t *testing.T) {
	for _, tc := range []struct {
		name    string
		noLinks bool
		free    int64
		// stopAt is the start of the name of the first write that stops qui.
		stopAt          string
		writtenFolders  map[string]bool
		oneFileOnTarget bool
	}{
		{name: "the links", free: 70, stopAt: showName + "/",
			writtenFolders: map[string]bool{showName: true}, oneFileOnTarget: true},
		// regression: #4231
		{name: "a copy where the target cannot link", noLinks: true, free: 140, stopAt: "links/" + showName + "/Show.S01E02.mkv",
			writtenFolders: map[string]bool{showName: true, "links/" + showName: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, onDisk, onDisk)
			r.disk.otherFilesystem = true
			r.disk.noLinks = tc.noLinks
			r.disk.free = tc.free
			s := newShow(t, showName)
			r.seed(s)
			_, crossSeedHash := r.seedHardlinkedCrossSeed(s)
			stop := sync.OnceFunc(r.cancel)
			r.disk.onWrite = func(name string) {
				if strings.HasPrefix(filepath.ToSlash(name), tc.stopAt) {
					stop()
				}
			}

			id, err := r.svc.Start(t.Context(), r.request(s))
			require.NoError(t, err)
			r.svc.Wait()
			require.True(t, r.get(id).FinishedAt.IsZero(), "the stop keeps the Transfer unfinished")
			r.restart()
			r.svc.Wait()

			record := r.get(id)
			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, tc.writtenFolders, r.writtenFolders())
			_, creates := r.disk.log()
			for name, n := range creates {
				require.Equal(t, 1, n, "qui creates %s once", name)
			}
			for name := range s.files {
				require.Equal(t, tc.oneFileOnTarget, sameFile(t, filepath.Join(r.dstDir, showName, name), filepath.Join(r.dstDir, "links", showName, name)), name)
			}
			require.True(t, r.seeding(r.target.ID, crossSeedHash))
			require.Empty(t, tree(t, r.srcDir))
		})
	}
}

// A connection that drops while qui links a hardlink on the target, or
// creates a temporary file there, is no sign that the target cannot link. qui
// reconnects, makes again a name that the target may have made before the
// drop, and links the path of the cross-seed to the copy. It writes no second
// copy.
// regression: #4231
func TestTransferLinksAHardlinkedCrossSeedAfterTheTargetDrops(t *testing.T) {
	for _, tc := range []struct {
		name string
		drop targetDrop
	}{
		{name: "before a link", drop: targetDrop{op: "link"}},
		{name: "after a link", drop: targetDrop{op: "link", served: true}},
		{name: "after a create", drop: targetDrop{op: "create", served: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, onDisk, overSFTP)
			r.disk.otherFilesystem = true
			s := newShow(t, showName)
			r.seed(s)
			_, crossSeedHash := r.seedHardlinkedCrossSeed(s)
			r.dropAtFirst(tc.drop)

			record := r.run(r.request(s))

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Contains(t, logOf(record), "The connection to homelab dropped.")
			require.NotContains(t, logOf(record), "so it copies the file")
			require.Equal(t, map[string]bool{showName: true}, r.writtenFolders(), "qui writes no byte of the cross-seed")
			for name := range s.files {
				require.True(t, sameFile(t, filepath.Join(r.dstDir, showName, name), filepath.Join(r.dstDir, "links", showName, name)), name)
			}
			require.True(t, r.seeding(r.target.ID, crossSeedHash))
		})
	}
}

// targetDrop drops the connections to an SFTP target once, at the first
// request op of qui there: "link" or "create". With served, the target takes
// the request, and only its answer is lost.
type targetDrop struct {
	op     string
	served bool
}

// dropAtFirst starts qui again on an SFTP target whose connections drop as
// d says.
func (r *rig) dropAtFirst(d targetDrop) {
	r.t.Helper()
	ssh := sshpool.NewPool(sshpool.NewDialer(r.instances))
	r.t.Cleanup(ssh.Close)
	drops := &drops{targetDrop: d, server: r.dstSSH}
	r.deps.Files = fsops.NewPoolWithRemote(r.instances, r.disk, func(inst *models.Instance) fsops.Backend {
		return &dropFiles{sftpFiles: &sftpFiles{Backend: remote.New(ssh, inst), d: r.disk}, drops: drops}
	})
	r.restart()
}

type drops struct {
	targetDrop
	server  *sshtest.Server
	dropped atomic.Bool
}

// request runs a request op on name, and fails it as the remote backend
// fails on a dropped connection when it is the request that drops.
func (d *drops) request(op, name string, request func() error) error {
	if op != d.op || !d.dropped.CompareAndSwap(false, true) {
		return request()
	}
	if d.served {
		if err := request(); err != nil {
			return err
		}
	}
	d.server.DropConnections()
	return fmt.Errorf("%s %s: %w", op, name, fsops.ErrConnectionLost)
}

type dropFiles struct {
	*sftpFiles
	drops *drops
}

func (f *dropFiles) OpenRoot(ctx context.Context, dir string) (fsops.Root, error) {
	root, err := f.sftpFiles.OpenRoot(ctx, dir)
	if err != nil {
		return nil, err
	}
	return &dropRoot{Root: root, drops: f.drops}, nil
}

type dropRoot struct {
	fsops.Root
	drops *drops
}

func (r *dropRoot) Link(ctx context.Context, oldName, newName string) error {
	return r.drops.request("link", newName, func() error { return r.Root.Link(ctx, oldName, newName) })
}

func (r *dropRoot) Create(ctx context.Context, name string) (fsops.FileWriter, error) {
	var w fsops.FileWriter
	err := r.drops.request("create", name, func() (err error) {
		w, err = r.Root.Create(ctx, name)
		return err
	})
	if err != nil {
		if w != nil {
			_ = w.Close()
		}
		return nil, err
	}
	return w, nil
}

// writtenFolders returns the folders below the root of the Transfer of the
// files that the copy wrote.
func (r *rig) writtenFolders() map[string]bool {
	writes, _ := r.disk.log()
	out := map[string]bool{}
	for _, w := range writes {
		out[path.Dir(filepath.ToSlash(w.name))] = true
	}
	return out
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	ai, err := os.Stat(a)
	require.NoError(t, err)
	bi, err := os.Stat(b)
	require.NoError(t, err)
	return os.SameFile(ai, bi)
}

// Each running Transfer to a target holds its bytes of the free space there,
// so a Transfer from another source cannot count them again.
func TestTransferReservesFreeSpaceOnTheTarget(t *testing.T) {
	r := newRig(t)
	first := newShow(t, showName)
	r.seed(first)
	otherDir := t.TempDir()
	otherPath := pathOn(overSFTP, otherDir)
	other := qbtstub.New(t, otherPath)
	otherSource := r.sshInstance("seedbox two", other.URL)
	second := newShow(t, "Other.Show.S02.720p-GRP")
	write(t, filepath.Join(otherDir, "Other.Show.S02.720p-GRP"), second.files)
	other.Seed(second.data, otherPath)
	small, smallHash := singleFileTorrent(t, "Small.mkv", []byte("ten bytes!"))
	write(t, otherDir, map[string][]byte{"Small.mkv": []byte("ten bytes!")})
	other.Seed(small, otherPath)
	r.disk.free = 100

	// The first Transfer stops at its first write. The third Transfer waits
	// behind the second on the same source, so its first write tells that the
	// second has ended.
	copying, release := make(chan struct{}), make(chan struct{})
	var writes atomic.Int32
	r.disk.onWrite = func(string) {
		switch writes.Add(1) {
		case 1:
			close(copying)
			<-release
		case 2:
			close(release)
		}
	}
	firstID, err := r.svc.Start(t.Context(), r.request(first))
	require.NoError(t, err)
	<-copying
	secondID, err := r.svc.Start(t.Context(), transfer.Request{SourceInstanceID: otherSource.ID, Hash: second.hash, TargetInstanceID: r.target.ID})
	require.NoError(t, err)
	thirdID, err := r.svc.Start(t.Context(), transfer.Request{SourceInstanceID: otherSource.ID, Hash: smallHash, TargetInstanceID: r.target.ID})
	require.NoError(t, err)
	r.svc.Wait()

	records := map[int64]*transfer.Record{}
	for _, id := range []int64{firstID, secondID, thirdID} {
		records[id], err = r.store.Get(t.Context(), id)
		require.NoError(t, err)
	}
	require.Equal(t, transfer.StateDone, records[firstID].State, records[firstID].FailureReason)
	require.Equal(t, transfer.StateFailed, records[secondID].State)
	require.Contains(t, records[secondID].FailureReason, "the Transfer needs 70 B, and 30 B is free")
	require.Equal(t, transfer.StateDone, records[thirdID].State, records[thirdID].FailureReason)
}

// A file of the group that is on the target before the Transfer is a head
// start for each torrent of the group: qui writes only the missing file, and
// once.
func TestTransferUsesAFileOnTheTargetForTheWholeGroup(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	crossSeed, crossSeedHash := crossSeedOf(t, s)
	r.src.Seed(crossSeed, r.srcPath())
	theirs := map[string][]byte{"Show.S01E02.mkv": s.files["Show.S01E02.mkv"]}
	before := r.placeOnTarget(theirs)
	r.disk.free = 40
	written := map[string]struct{}{}
	r.disk.onWrite = func(name string) {
		final, _, _ := strings.Cut(path.Base(name), ".qui-transfer-")
		written[final] = struct{}{}
	}

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, map[string]struct{}{"Show.S01E01.mkv": {}}, written)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	r.requireUntouched(before, theirs)
	require.True(t, r.seeding(r.target.ID, s.hash))
	require.True(t, r.seeding(r.target.ID, crossSeedHash))
	require.Equal(t, map[string]string{showName + "/Show.S01E02.mkv": string(theirs["Show.S01E02.mkv"])}, tree(t, r.srcDir),
		"the source keeps the file that qui did not copy")
}
