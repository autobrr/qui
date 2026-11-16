// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
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

// A cross-seed that reads the same files moves with the torrent in one
// Transfer. qui copies each file once, so the free space for one copy is
// enough.
func TestTransferMovesTheGroupWithItsCrossSeed(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	crossSeed, crossSeedHash := crossSeedOf(t, s)
	r.src.Seed(crossSeed, r.srcDir)
	r.disk.free = 70

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Len(t, record.Members, 2)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir), "the target has each file once")
	require.True(t, r.seeding(r.target.ID, s.hash))
	require.True(t, r.seeding(r.target.ID, crossSeedHash))
	require.False(t, r.src.Has(s.hash))
	require.False(t, r.src.Has(crossSeedHash))
	require.Empty(t, tree(t, r.srcDir))
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
	r.svc.Close()
	r.svc = transfer.NewService(r.t.Context(), r.deps)
	r.t.Cleanup(r.svc.Close)
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
	if err != nil || len(matched) != 1 {
		return nil, fmt.Errorf("no matched torrent %s: %v", req.ManualTargetHash, err)
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
	r.src.Seed(crossSeed, r.srcDir)
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

// A refused cross-seed add fails the Transfer. The source keeps both
// torrents and their files.
func TestTransferFailsWhenTheCrossSeedAddFails(t *testing.T) {
	r := newRig(t)
	cs := r.withCrossSeedInstance()
	cs.refuse = "no_match"
	s := newShow(t, showName)
	r.seed(s)
	crossSeed, crossSeedHash := crossSeedOf(t, s)
	r.src.Seed(crossSeed, r.srcDir)
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
	files := map[string][]byte{
		"Show.S01E01.mkv": bytes.Repeat([]byte("one|"), 10),
		"Show.S01E02.mkv": []byte("E02-abcdefghijklmnopqrstuvwxyz"),
		"Show.S01E03.mkv": bytes.Repeat([]byte("six|"), 5),
	}
	pack, packHash := qbtstub.NewContentTorrent(t, showName, 16,
		qbtstub.ContentFile{Path: "Show.S01E01.mkv", Data: files["Show.S01E01.mkv"]},
		qbtstub.ContentFile{Path: "Show.S01E02.mkv", Data: files["Show.S01E02.mkv"]},
		qbtstub.ContentFile{Path: "Show.S01E03.mkv", Data: files["Show.S01E03.mkv"]})
	write(t, filepath.Join(r.srcDir, showName), files)
	r.src.Seed(pack, r.srcDir)
	episode, episodeHash := singleFileTorrent(t, "Show.S01E02.mkv", files["Show.S01E02.mkv"])
	r.src.Seed(episode, filepath.Join(r.srcDir, showName))
	r.disk.free = 90

	record := r.run(transfer.Request{SourceInstanceID: r.source.ID, Hash: episodeHash, TargetInstanceID: r.target.ID})

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Len(t, record.Members, 2)
	require.Empty(t, cs.requests)
	require.Contains(t, logOf(record), "Proof: the check on homelab passed all 6 pieces of "+showName+".")
	require.True(t, r.seeding(r.target.ID, episodeHash))
	require.True(t, r.seeding(r.target.ID, packHash))
	require.Equal(t, contents(show{files: files}, showName), tree(t, r.dstDir))
	require.False(t, r.src.Has(packHash))
	require.False(t, r.src.Has(episodeHash))
	require.Empty(t, tree(t, r.srcDir))
}

// Over SSH qui does not see a hardlink, so a cross-seed on links to the
// files stays on the source and keeps its data.
func TestTransferLeavesAHardlinkedCrossSeedOverSSH(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	links := filepath.Join(r.srcDir, "links")
	require.NoError(t, os.MkdirAll(filepath.Join(links, showName), 0o755))
	for name := range s.files {
		require.NoError(t, os.Link(filepath.Join(r.srcDir, showName, name), filepath.Join(links, showName, name)))
	}
	crossSeed, crossSeedHash := crossSeedOf(t, s)
	r.src.Seed(crossSeed, links)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Len(t, record.Members, 1)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.True(t, r.seeding(r.source.ID, crossSeedHash))
	require.Equal(t, contents(s, showName), tree(t, links))
}

// Each running Transfer to a target holds its bytes of the free space there,
// so a Transfer from another source cannot count them again.
func TestTransferReservesFreeSpaceOnTheTarget(t *testing.T) {
	r := newRig(t)
	first := newShow(t, showName)
	r.seed(first)
	otherDir := t.TempDir()
	other := qbtstub.New(t, otherDir)
	otherSource := r.sshInstance("seedbox two", other.URL)
	second := newShow(t, "Other.Show.S02.720p-GRP")
	write(t, filepath.Join(otherDir, "Other.Show.S02.720p-GRP"), second.files)
	other.Seed(second.data, otherDir)
	small, smallHash := singleFileTorrent(t, "Small.mkv", []byte("ten bytes!"))
	write(t, otherDir, map[string][]byte{"Small.mkv": []byte("ten bytes!")})
	other.Seed(small, otherDir)
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
