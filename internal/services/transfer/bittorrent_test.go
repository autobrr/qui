// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

var (
	sourcePeer = netip.MustParseAddrPort("198.51.100.1:6881")
	targetPeer = netip.MustParseAddrPort("198.51.100.2:6881")
)

func (r *rig) connect() {
	swarm := qbtstub.NewSwarm()
	swarm.Join(r.src, sourcePeer)
	swarm.Join(r.dst, targetPeer)
	r.dst.FreeSpace = 1 << 30
}

func (r *rig) requireNoHelpers(record *transfer.Record) {
	r.t.Helper()
	helpers, err := r.store.HelpersOf(r.t.Context(), transfer.OwnerTransfer, record.ID)
	require.NoError(r.t, err)
	for _, h := range helpers {
		require.False(r.t, r.src.Has(h.Hash), "the Transfer torrent left the source")
		require.False(r.t, r.dst.Has(h.Hash), "the Transfer torrent left the target")
	}
}

func (r *rig) requireHelperTag(record *transfer.Record) {
	r.t.Helper()
	helpers, err := r.store.HelpersOf(r.t.Context(), transfer.OwnerTransfer, record.ID)
	require.NoError(r.t, err)
	require.NotEmpty(r.t, helpers)
	for _, h := range helpers {
		client := r.dst
		if h.InstanceID == r.source.ID {
			client = r.src
		}
		require.Equal(r.t, []string{"qui-transfer"}, client.AddedTags(h.Hash))
	}
}

func TestTransferTorrentConnectsTheOtherWay(t *testing.T) {
	r := newRigOn(t, overSFTP, noAccess)
	r.connect()
	r.src.Unreachable = true
	s := newShow(t, showName)
	r.seed(s)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, transfer.WayBitTorrent, record.Way)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.Contains(t, logOf(record), "Passing 70 B by a Transfer torrent from seedbox to homelab.")
	require.Contains(t, logOf(record), "qui cannot see the files on homelab, so a file with other content at the same path can be replaced.")
	require.Contains(t, logOf(record), "No byte arrived on homelab from seedbox. qui asks seedbox to connect to homelab.")
	require.Contains(t, logOf(record), "Proof: BitTorrent checked every piece on homelab.")
	require.Equal(t, 1, r.dst.Calls("torrents/addPeers"))
	require.Equal(t, 1, r.src.Calls("torrents/addPeers"))
	r.requireNoHelpers(record)
}

func TestTransferTorrentWaitReadsTheSyncSnapshot(t *testing.T) {
	syncs := func(checkPolls int) int {
		r := newRigOn(t, overSFTP, noAccess, func(c *transfer.Config) { c.CheckPolls, c.SnapshotAge = checkPolls, time.Hour })
		r.connect()
		r.dst.CheckSyncs = 1000
		s := newShow(t, showName)
		r.seed(s)

		record := r.run(r.request(s))

		require.Equal(t, transfer.StateFailed, record.State)
		require.Contains(t, record.FailureReason, "The check of the Transfer torrent on homelab did not end.")
		return r.dst.Syncs()
	}

	short, long := syncs(20), syncs(60)

	require.Less(t, long-short, 10, "a poll that asked qBittorrent each time would send 80 more syncs")
}

func TestTransferTorrentWaitEndsOnAFreshSync(t *testing.T) {
	r := newRigOn(t, overSFTP, noAccess, func(c *transfer.Config) { c.SnapshotAge = time.Hour })
	r.connect()
	r.dst.CheckSyncs = 12
	s := newShow(t, showName)
	r.seed(s)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, transfer.WayBitTorrent, record.Way)
}

func TestTransferTorrentFallsBackToSFTP(t *testing.T) {
	for _, tc := range []struct {
		name    string
		connect func(r *rig)
		reason  string
	}{
		{name: "no public peer address", connect: func(*rig) {},
			reason: "The clients did not connect: seedbox has no public peer address, and homelab has no public peer address."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, overSFTP, overSFTP)
			tc.connect(r)
			s := newShow(t, showName)
			r.seed(s)

			record := r.run(r.request(s))

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, transfer.WaySFTP, record.Way)
			require.Equal(t, contents(s, showName), tree(t, r.dstDir), "the copy over SFTP left no temporary file")
			require.Contains(t, logOf(record), tc.reason+" qui copies the files over SFTP instead.")
			require.Empty(t, tree(t, r.srcDir))
			r.requireNoHelpers(record)
		})
	}
}

func TestTransferTorrentFailsWithoutAWayBack(t *testing.T) {
	for _, tc := range []struct {
		name           string
		source, target side
		forced         transfer.Way
	}{
		{name: "a source without file access", source: noAccess, target: overSFTP},
		{name: "forced BitTorrent", source: overSFTP, target: overSFTP, forced: transfer.WayBitTorrent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, tc.source, tc.target)
			r.connect()
			r.src.Unreachable, r.dst.Unreachable = true, true
			s := newShow(t, showName)
			r.seed(s)
			req := r.request(s)
			req.Options.ForcedWay = tc.forced

			record := r.run(req)

			require.Equal(t, transfer.StateFailed, record.State)
			require.Equal(t, transfer.FailureTemporary, record.FailureClass)
			require.Equal(t, "The clients did not connect: homelab did not reach seedbox at 198.51.100.1:6881, "+
				"and seedbox did not reach homelab at 198.51.100.2:6881.", record.FailureReason)
			require.Equal(t, transfer.WayBitTorrent, record.Way)
			r.requireNoHelpers(record)
			require.True(t, r.seeding(r.source.ID, s.hash), "the torrent seeds on the source")
			require.False(t, r.dst.Has(s.hash))
		})
	}
}

func TestTransferTorrentWritesTheTemporaryNamesOfTheCopy(t *testing.T) {
	r := newRigOn(t, overSFTP, onDisk)
	r.connect()
	s := newShow(t, showName)
	r.seed(s)
	req := r.request(s)
	req.Options.ForcedWay = transfer.WayBitTorrent

	record := r.run(req)

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, transfer.WayBitTorrent, record.Way)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir), "no temporary name is left")
	require.Equal(t, 2, r.src.Writes("torrents/renameFile"), "the source renames both files of the Transfer torrent")
	require.True(t, r.seeding(r.target.ID, s.hash))
	require.Empty(t, tree(t, r.srcDir))
	r.requireNoHelpers(record)
}

func TestTransferTorrentConnectsAfterTheCheckOnTheSource(t *testing.T) {
	r := newRigOn(t, overSFTP, onDisk)
	r.connect()
	r.src.CheckSyncs = 3
	s := newShow(t, showName)
	r.seed(s)
	req := r.request(s)
	req.Options.ForcedWay = transfer.WayBitTorrent

	record := r.run(req)

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, transfer.WayBitTorrent, record.Way)
	require.Zero(t, r.src.Calls("torrents/addPeers"), "the target reached the source at its first try")
}

func TestTransferTorrentWaitsForACheckOnTheSourceLongerThanAWait(t *testing.T) {
	r := newRigOn(t, overSFTP, onDisk, func(c *transfer.Config) { c.CheckPolls = 100 })
	r.connect()
	r.src.CheckSyncs = 60
	s := newShow(t, showName)
	r.seed(s)
	req := r.request(s)
	req.Options.ForcedWay = transfer.WayBitTorrent

	record := r.run(req)

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, transfer.WayBitTorrent, record.Way)
}

func TestTransferTorrentStopsAtAFileInTheWayOnTheSource(t *testing.T) {
	r := newRigOn(t, overSFTP, onDisk)
	r.connect()
	s := newShow(t, showName)
	r.seed(s)
	theirs := filepath.Join(r.srcDir, showName, "Show.S01E01.mkv.qui-transfer-1")
	require.NoError(t, os.WriteFile(theirs, []byte("theirs"), 0o600))
	req := r.request(s)
	req.Options.ForcedWay = transfer.WayBitTorrent

	record := r.run(req)

	want := contents(s, showName)
	want[showName+"/Show.S01E01.mkv.qui-transfer-1"] = "theirs"
	require.Equal(t, want, tree(t, r.srcDir), "the source keeps its files")
	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, transfer.FailurePermanent, record.FailureClass)
	require.Equal(t, "Conflict: a file is in the way on seedbox at "+pathOn(overSFTP, theirs)+".", record.FailureReason)
	require.Zero(t, r.src.Writes("torrents/renameFile"))
	require.True(t, r.seeding(r.source.ID, s.hash))
	require.Empty(t, tree(t, r.dstDir))
	r.requireNoHelpers(record)
}

func TestTransferPicksTheWay(t *testing.T) {
	for _, tc := range []struct {
		name           string
		source, target side
		forced         transfer.Way
		want           transfer.Way
	}{
		{name: "no file access", source: noAccess, target: noAccess, want: transfer.WayBitTorrent},
		{name: "a forced copy on qui's own disk", source: onDisk, target: onDisk, forced: transfer.WaySFTP, want: transfer.WayLocal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, tc.source, tc.target)
			r.connect()
			s := newShow(t, showName)
			r.seed(s)
			req := r.request(s)
			req.Options.ForcedWay = tc.forced

			record := r.run(req)

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, tc.want, record.Way)
			require.Equal(t, contents(s, showName), tree(t, r.dstDir))
			require.True(t, r.seeding(r.target.ID, s.hash))
			require.Empty(t, tree(t, r.srcDir))
		})
	}
}

func TestTransferTorrentHintsAtTCPOnly(t *testing.T) {
	r := newRigOn(t, overSFTP, noAccess)
	r.connect()
	r.src.BittorrentProtocol = 1
	s := newShow(t, showName)
	r.seed(s)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	var hints []transfer.LogLine
	for _, line := range record.Log {
		if line.Hint != nil {
			hints = append(hints, line)
		}
	}
	require.Len(t, hints, 1)
	require.Equal(t, transfer.Hint{Kind: transfer.HintTCPAndUTP, InstanceID: r.target.ID}, *hints[0].Hint)
	require.Equal(t, `homelab uses qBittorrent's default "TCP and μTP". In qui's tests, "TCP only" was about three times faster. `+
		"qui does not change this setting.", hints[0].Message)
	require.Equal(t, 1, r.src.BittorrentProtocol, "qui leaves the setting alone")
	require.Zero(t, r.dst.Writes("app/setPreferences"))
	require.Zero(t, r.src.Writes("app/setPreferences"))
}

func TestTransferTorrentReadsTheFreeSpaceInQBittorrent(t *testing.T) {
	const (
		full  = "Not enough free space on homelab: not enough free space on the target: the Transfer needs 70 B, and 69 B is free"
		noAdd = "homelab did not take the Transfer torrent: could not add torrent | conflicts detected: torrent(s) failed to be added"
	)
	for _, tc := range []struct {
		name                      string
		webAPI                    string
		free                      int64
		elsewhere                 bool
		savePath, defaultSavePath string
		placed                    string
		reason                    string
		log                       string
	}{
		{name: "5.3 refuses a full disk", webAPI: "2.15.2", free: 69, reason: full},
		{name: "5.3 at another save path", webAPI: "2.15.2", free: 70, elsewhere: true},
		{name: "5.1 elsewhere", free: 69, elsewhere: true,
			log: "Free space unknown on homelab: before qBittorrent 5.3, qBittorrent reports it only at its default save path."},
		{name: "5.3 at a Windows save path", webAPI: "2.15.2", free: 69, savePath: "C:/Downloads", placed: `C:\Downloads`, reason: noAdd},
		{name: "5.3 at a UNC save path", webAPI: "2.15.2", free: 69, savePath: `\\nas\media\Downloads`,
			placed: `\\nas\media\Downloads`, reason: noAdd},
		{name: "5.1 at a Windows default save path", free: 69, defaultSavePath: "C:/Downloads", placed: `C:\Downloads`, reason: noAdd},
		{name: "5.3 at a slash save path", webAPI: "2.15.2", free: 69, savePath: "/srv/downloads", placed: "/srv/downloads", reason: noAdd},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, overSFTP, noAccess)
			r.connect()
			r.dst.WebAPIVersion = tc.webAPI
			r.dst.FreeSpace = tc.free
			s := newShow(t, showName)
			r.seed(s)
			req := r.request(s)
			saveDir := r.dstDir
			if tc.elsewhere {
				saveDir = filepath.Join(r.dstDir, "elsewhere")
				req.Placement.SavePath = saveDir
			}
			req.Placement.SavePath = cmp.Or(tc.savePath, req.Placement.SavePath)
			if tc.defaultSavePath != "" {
				r.dst.SetDefaultSavePath(tc.defaultSavePath)
			}
			if tc.placed != "" {
				r.dst.OnWrite = func(endpoint string) {
					if endpoint == "torrents/add" {
						r.dst.Reject = http.StatusConflict
					}
				}
			}

			record := r.run(req)

			if tc.placed != "" {
				require.Equal(t, tc.placed, record.Members[0].Placement.SavePath)
				require.NotContains(t, logOf(record), "Free space unknown")
			}
			if tc.placed != "" && tc.webAPI != "" {
				require.Equal(t, tc.placed, r.dst.FreeSpacePath())
			}
			if tc.reason != "" {
				require.Equal(t, transfer.StateFailed, record.State)
				require.Equal(t, tc.reason, record.FailureReason)
				r.requireNoHelpers(record)
				require.Zero(t, r.src.Writes("torrents/add"), "the source never seeds the Transfer torrent")
				return
			}
			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, contents(s, showName), tree(t, saveDir))
			if tc.log != "" {
				require.Contains(t, logOf(record), tc.log)
			} else {
				require.NotContains(t, logOf(record), "Free space unknown")
			}
		})
	}
}

func TestTransferTorrentKeepsTheFilesThatTheTargetHad(t *testing.T) {
	for _, tc := range []struct {
		name   string
		place  func(t *testing.T, r *rig, s show)
		seeds  int
		reason string
	}{
		{name: "every file", place: func(t *testing.T, r *rig, s show) { write(t, filepath.Join(r.dstDir, showName), s.files) },
			reason: "homelab had the files before the Transfer"},
		{name: "a link to a file of the source", place: func(t *testing.T, r *rig, _ show) {
			require.NoError(t, os.MkdirAll(filepath.Join(r.dstDir, showName), 0o755))
			err := os.Symlink(filepath.Join(r.srcDir, showName, "Show.S01E01.mkv"), filepath.Join(r.dstDir, showName, "Show.S01E01.mkv"))
			if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
				t.Skip("this Windows user cannot make a symlink")
			}
			require.NoError(t, err)
		}, seeds: 1, reason: "qui saw only 30 B of 70 B arrive on homelab by BitTorrent, and homelab may read the rest on seedbox"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, overSFTP, noAccess)
			r.connect()
			s := newShow(t, showName)
			r.seed(s)
			tc.place(t, r, s)

			record := r.run(r.request(s))

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Contains(t, logOf(record), "Removed the torrent from seedbox and kept its files, because "+tc.reason)
			require.Equal(t, contents(s, showName), tree(t, r.srcDir))
			require.Equal(t, contents(s, showName), tree(t, r.dstDir))
			require.Equal(t, tc.seeds, r.src.Writes("torrents/add"))
			r.requireNoHelpers(record)
		})
	}
}

func TestTransferTorrentFromASourceWithoutFileAccess(t *testing.T) {
	t.Run("removes the files", func(t *testing.T) {
		r := newRigOn(t, noAccess, onDisk)
		r.connect()
		s := newShow(t, showName)
		r.seed(s)

		record := r.run(r.request(s))

		require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
		require.Equal(t, transfer.WayBitTorrent, record.Way)
		require.Equal(t, "true", r.src.LastParams("torrents/delete").Get("deleteFiles"))
		require.Empty(t, tree(t, r.srcDir))
		require.Contains(t, logOf(record), "Removed the torrent and its files from seedbox.")
	})
	t.Run("keeps them after a head start", func(t *testing.T) {
		r := newRigOn(t, noAccess, onDisk)
		r.connect()
		s := packOf(t, qbtstub.ContentFile{Path: episode1, Data: numbered("E01", 32)}, qbtstub.ContentFile{Path: episode2, Data: numbered("E02", 30)})
		r.seed(s)
		before := r.placeOnTarget(map[string][]byte{episode1: s.files[episode1]})

		record := r.run(r.request(s))

		require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
		r.requireUntouched(before, s.files)
		require.Equal(t, "false", r.src.LastParams("torrents/delete").Get("deleteFiles"))
		require.Equal(t, contents(s, showName), tree(t, r.srcDir))
		require.Contains(t, logOf(record), "because qui cannot reach the files on seedbox to remove only the files that it copied")
	})
}

func TestTransferTorrentStartsAfterTheHeadStart(t *testing.T) {
	t.Run("passes the check", func(t *testing.T) {
		r := newRigOn(t, overSFTP, overSFTP)
		r.connect()
		s := newShow(t, showName)
		r.seed(s)
		before := r.placeOnTarget(map[string][]byte{"Show.S01E01.mkv": s.files["Show.S01E01.mkv"]})

		record := r.run(r.request(s))

		require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
		require.Equal(t, transfer.WayBitTorrent, record.Way)
		r.requireUntouched(before, s.files)
		require.Equal(t, contents(s, showName), tree(t, r.dstDir))
		require.Equal(t, map[string]string{showName + "/Show.S01E01.mkv": string(s.files["Show.S01E01.mkv"])}, tree(t, r.srcDir),
			"the file that the target had stays on the source")
		r.requireHelperTag(record)
	})
	t.Run("fails the check", func(t *testing.T) {
		r := newRigOn(t, overSFTP, overSFTP)
		r.connect()
		s := newShow(t, showName)
		r.seed(s)
		other := map[string][]byte{"Show.S01E01.mkv": bytes.Repeat([]byte("x"), len(s.files["Show.S01E01.mkv"]))}
		before := r.placeOnTarget(other)

		record := r.run(r.request(s))

		require.Equal(t, transfer.StateFailed, record.State)
		require.Contains(t, record.FailureReason, "Conflict: the file at")
		r.requireUntouched(before, other)
		require.Zero(t, r.src.Writes("torrents/add"), "no Transfer torrent")
		require.Zero(t, r.dst.Calls("torrents/addPeers"))
	})
}

func TestTransferTorrentNeverWritesIntoAFileOnTheTarget(t *testing.T) {
	const nfo = "Show.S01.nfo"
	edges := numbered("E02", 36)
	copy(edges[:8], "XXXXXXXX")
	copy(edges[24:], "YYYYYYYYYYYY")
	for _, tc := range []struct {
		name   string
		source side
		middle qbtstub.ContentFile
		theirs []byte
		reason string
	}{
		{name: "edges that differ", source: overSFTP, middle: qbtstub.ContentFile{Path: episode2, Data: numbered("E02", 36)},
			theirs: edges, reason: "Conflict: the file at %s on homelab differs from the torrent."},
		{name: "no complete piece of its own", source: overSFTP, middle: qbtstub.ContentFile{Path: nfo, Data: numbered("nfo", 10)},
			theirs: numbered("NFO", 10), reason: "Conflict: the file at %s on homelab differs from the torrent."},
		{name: "a source without file access", source: noAccess, middle: qbtstub.ContentFile{Path: episode2, Data: numbered("E02", 36)},
			theirs: numbered("E02", 36),
			reason: "Conflict: qui cannot check a part of the file at %s on homelab, because it cannot read the files on seedbox."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, tc.source, overSFTP)
			r.connect()
			s := packOf(t, qbtstub.ContentFile{Path: episode1, Data: numbered("E01", 40)}, tc.middle,
				qbtstub.ContentFile{Path: episode3, Data: numbered("E03", 30)})
			r.seed(s)
			theirs := map[string][]byte{tc.middle.Path: tc.theirs}
			before := r.placeOnTarget(theirs)

			record := r.run(r.request(s))

			r.requireUntouched(before, theirs)
			require.Equal(t, transfer.WayBitTorrent, record.Way)
			require.Equal(t, transfer.StateFailed, record.State)
			require.Equal(t, transfer.FailurePermanent, record.FailureClass)
			require.Equal(t, fmt.Sprintf(tc.reason, pathOn(overSFTP, filepath.Join(r.dstDir, showName, tc.middle.Path))), record.FailureReason)
			require.Zero(t, r.src.Writes("torrents/add"), "no Transfer torrent")
		})
	}
}

func TestTransferTorrentRetries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before func(r *rig, s show)
		source func(s show) map[string]string
	}{
		{name: "nothing on the target", before: func(*rig, show) {},
			source: func(show) map[string]string { return map[string]string{} }},
		{name: "a download that stopped", before: func(r *rig, s show) {
			write(t, filepath.Join(r.dstDir, showName), map[string][]byte{episode1: s.files[episode1]})
			r.dst.FreeSpace = 38
		}, source: func(s show) map[string]string { return contents(s, showName) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, overSFTP, noAccess)
			r.connect()
			r.dst.WebAPIVersion = "2.15.2"
			r.src.Unreachable, r.dst.Unreachable = true, true
			s := newShow(t, showName)
			r.seed(s)
			first := r.run(r.request(s))
			require.Equal(t, transfer.StateFailed, first.State)
			require.Equal(t, transfer.FailureTemporary, first.FailureClass)

			tc.before(r, s)
			r.src.Unreachable = false
			r.setClock(clock.Add(time.Hour))
			r.svc.Wake()
			r.svc.Wait()

			record := r.get(first.ID)
			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, contents(s, showName), tree(t, r.dstDir))
			require.Equal(t, tc.source(s), tree(t, r.srcDir))
			r.requireNoHelpers(record)
			helpers, err := r.store.HelpersOf(t.Context(), transfer.OwnerTransfer, record.ID)
			require.NoError(t, err)
			require.Len(t, helpers, 4, "each run adds a Transfer torrent with a new infohash on both clients")
		})
	}
}

func TestTransferTorrentLeavesAHardlinkedCrossSeed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target side
		forced transfer.Way
		line   string
	}{
		{name: "a target without file access", target: noAccess,
			line: "1 cross-seed stays on seedbox. It reads these files through hardlinks, and qui cannot make hardlinks on homelab without file access. " +
				"To move it too, give qui file access to homelab, on qui's own disk or over SSH, or cross-seed again on homelab."},
		{name: "a forced Transfer torrent", target: overSFTP, forced: transfer.WayBitTorrent,
			line: "1 cross-seed stays on seedbox. It reads these files through hardlinks, and a Transfer torrent cannot make hardlinks. " +
				`To move it too, select the way "Auto" or "Copy through qui".`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, onDisk, tc.target)
			r.connect()
			r.dst.FreeSpace = 70
			s := newShow(t, showName)
			r.seed(s)
			links, crossSeedHash := r.seedHardlinkedCrossSeed(s)
			sourceFiles := tree(t, r.srcDir)
			req := r.request(s)
			req.Options.ForcedWay = tc.forced

			summary, err := r.svc.Summary(t.Context(), req)
			require.NoError(t, err)
			require.Equal(t, 1, summary.Stay)
			require.Len(t, summary.Members, 1)

			record := r.run(req)

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Len(t, record.Members, 1)
			require.Contains(t, logOf(record), tc.line)
			require.Equal(t, contents(s, showName), tree(t, r.dstDir))
			require.True(t, r.seeding(r.target.ID, s.hash))
			require.False(t, r.dst.Has(crossSeedHash))
			require.True(t, r.seeding(r.source.ID, crossSeedHash))
			require.Equal(t, contents(s, showName), tree(t, links))
			require.Equal(t, sourceFiles, tree(t, r.srcDir), "the cross-seed reads the files, so they stay on the source")
			r.requireNoHelpers(record)
		})
	}
}

func TestTransferTorrentPlacesAGroupBelowAWindowsSavePath(t *testing.T) {
	r := newRigOn(t, onDisk, noAccess)
	r.connect()
	r.dst.FreeSpace = 140
	r.dst.WebAPIVersion = "2.15.2"
	r.dst.OnWrite = func(endpoint string) {
		if endpoint == "torrents/add" {
			r.dst.Reject = http.StatusConflict
		}
	}
	s := newShow(t, showName)
	r.seed(s)
	r.seedEpisode(s)
	req := r.request(s)
	req.Placement.SavePath = "C:/Downloads"

	record := r.run(req)

	require.Equal(t, []string{`C:\Downloads`, `C:\Downloads\` + showName},
		[]string{record.Members[0].Placement.SavePath, record.Members[1].Placement.SavePath})
	require.Equal(t, `C:\Downloads`, r.dst.FreeSpacePath())
	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, "homelab did not take the Transfer torrent: could not add torrent | conflicts detected: torrent(s) failed to be added",
		record.FailureReason, "the stub cannot write at a save path on another host")
}

func TestTransferTorrentWaitsForARecheckOnCompletionOnTheTarget(t *testing.T) {
	r := newRigOn(t, overSFTP, noAccess)
	r.connect()
	r.dst.RecheckSyncs = 30
	s := newShow(t, showName)
	r.seed(s)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, transfer.WayBitTorrent, record.Way)
}
