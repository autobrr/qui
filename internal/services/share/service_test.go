// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package share_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/rollout"
	"github.com/autobrr/qui/internal/services/peeraddress"
	"github.com/autobrr/qui/internal/services/share"
	"github.com/autobrr/qui/internal/services/sharetracker"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const showName = "Show.S01.1080p-GRP"

// ownerAddress is the public peer address of the user's client: the
// external address and the listen port that the stub reports.
var ownerAddress = netip.MustParseAddrPort("203.0.113.10:51413")

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// rig is one instance with a stub qBittorrent that seeds the torrent of
// Show.S01.1080p-GRP from dir, and a Share service with qui's own tracker on
// 127.0.0.1.
type rig struct {
	t               *testing.T
	db              dbinterface.Querier
	dir             string
	stub            *qbtstub.Server
	instances       *models.InstanceStore
	instance        *models.Instance
	sync            *qbittorrent.SyncManager
	helpers         *transfer.Store
	trackerSettings *sharetracker.SettingsStore
	clock           *clock
	port            int
	hash            string
	svc             *share.Service
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, dir: t.TempDir(), db: testdb.NewMigratedSQLite(t, "share-service"), clock: &clock{now: storeClock}}
	r.stub = qbtstub.New(t, r.dir)
	r.stub.ListenPort = int(ownerAddress.Port())
	r.stub.SetExternalAddress(ownerAddress.Addr().String(), "")

	r.hash = r.seed(showName)

	var err error
	r.instances, err = models.NewInstanceStore(r.db, bytes.Repeat([]byte{7}, 32))
	require.NoError(t, err)
	r.instance, err = r.instances.Create(t.Context(), "seedbox", r.stub.URL, "", "", nil, nil, false, new(true))
	require.NoError(t, err)
	clientPool, err := qbittorrent.NewClientPool(r.instances, models.NewInstanceErrorStore(r.db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })
	r.sync = qbittorrent.NewSyncManager(clientPool, nil)
	r.helpers, err = transfer.NewStore(t.Context(), r.db)
	require.NoError(t, err)
	r.sync.SetHelperTorrents(r.helpers)

	r.port = freePort(t)
	r.trackerSettings = sharetracker.NewSettingsStore(r.db)
	require.NoError(t, r.trackerSettings.SaveSettings(t.Context(), sharetracker.Settings{
		Port: r.port, PublicAddress: "http://127.0.0.1:" + strconv.Itoa(r.port),
	}))
	r.svc = r.newService(rollout.Switches{TransfersAndShares: true})
	return r
}

// seed writes the files of a complete torrent under name, and seeds the
// torrent in the stub.
func (r *rig) seed(name string) string {
	r.t.Helper()
	files := map[string][]byte{
		"Show.S01E01.mkv": bytes.Repeat([]byte("episode one|"), 4)[:40],
		"Show.S01E02.mkv": bytes.Repeat([]byte("episode two|"), 3)[:30],
	}
	for file, data := range files {
		path := filepath.Join(r.dir, name, file)
		require.NoError(r.t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(r.t, os.WriteFile(path, data, 0o600))
	}
	data, _ := qbtstub.NewContentTorrent(r.t, name, 16,
		qbtstub.ContentFile{Path: "Show.S01E01.mkv", Data: files["Show.S01E01.mkv"]},
		qbtstub.ContentFile{Path: "Show.S01E02.mkv", Data: files["Show.S01E02.mkv"]})
	return r.stub.Seed(data, r.dir)
}

// newService builds a Share service on the database of the rig, as qui does
// at each start.
func (r *rig) newService(switches rollout.Switches) *share.Service {
	svc := share.NewService(share.Deps{
		Store:           share.NewStore(r.db),
		Settings:        share.NewSettingsStore(r.db),
		TrackerSettings: r.trackerSettings,
		Helpers:         r.helpers,
		Instances:       r.instances,
		Sync:            r.sync,
		Peers:           peeraddress.New(r.instances, r.sync),
		Switches:        switches,
		TrackerHost:     "127.0.0.1",
		Now:             r.clock.Now,
		Config:          share.Config{Polls: 20, PollInterval: time.Millisecond, Tick: time.Hour},
	})
	r.sync.SetTorrentChangeGuard(svc)
	r.t.Cleanup(svc.Close)
	return svc
}

func (r *rig) start(req share.Request) *share.Share {
	r.t.Helper()
	if req.InstanceID == 0 {
		req.InstanceID = r.instance.ID
	}
	if req.Hash == "" {
		req.Hash = r.hash
	}
	sh, err := r.svc.Start(r.t.Context(), req)
	require.NoError(r.t, err)
	return sh
}

func (r *rig) get(id int64) share.Share {
	r.t.Helper()
	list, err := r.svc.List(r.t.Context())
	require.NoError(r.t, err)
	for _, sh := range list {
		if sh.ID == id {
			return sh
		}
	}
	r.t.Fatalf("no Share %d", id)
	return share.Share{}
}

func (r *rig) check() {
	r.t.Helper()
	require.NoError(r.t, r.svc.Check(r.t.Context()))
}

// requireEnded checks that the Share ended for reason, that the Share torrent
// left the client, and that the files of the real torrent stayed.
func (r *rig) requireEnded(id int64, reason string) {
	r.t.Helper()
	sh := r.get(id)
	require.Equal(r.t, share.StateEnded, sh.State)
	require.Equal(r.t, reason, sh.EndReason)
	require.Equal(r.t, r.clock.Now(), sh.EndedAt)
	require.False(r.t, r.stub.Has(sh.HelperHash), "the Share torrent stays in the client")
	for _, name := range []string{"Show.S01E01.mkv", "Show.S01E02.mkv"} {
		require.FileExists(r.t, filepath.Join(r.dir, showName, name))
	}
}

// announce sends one announce to the Share tracker, from 127.0.0.1 with the
// port and the bytes left of a client, and returns the peers of the answer.
func (r *rig) announce(announceURL, infoHash string, port, left int, event string) []netip.AddrPort {
	r.t.Helper()
	peers, failure := r.sendAnnounce("", announceURL, infoHash, port, left, event)
	require.Empty(r.t, failure)
	return peers
}

// sendAnnounce sends one announce and returns the peers and the failure
// reason of the answer. A from address goes in X-Forwarded-For, for a
// tracker that trusts 127.0.0.1 as a proxy.
func (r *rig) sendAnnounce(from, announceURL, infoHash string, port, left int, event string) ([]netip.AddrPort, string) {
	r.t.Helper()
	hash, err := metainfo.NewHashFromHex(infoHash)
	require.NoError(r.t, err)
	q := url.Values{}
	q.Set("info_hash", string(hash.Bytes()))
	q.Set("port", strconv.Itoa(port))
	q.Set("uploaded", "0")
	q.Set("downloaded", "0")
	q.Set("left", strconv.Itoa(left))
	q.Set("compact", "1")
	if event != "" {
		q.Set("event", event)
	}
	req, err := http.NewRequestWithContext(r.t.Context(), http.MethodGet, announceURL+"?"+q.Encode(), nil)
	require.NoError(r.t, err)
	if from != "" {
		req.Header.Set("X-Forwarded-For", from)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(r.t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(r.t, err)
	require.Equal(r.t, http.StatusOK, resp.StatusCode, string(body))
	var answer struct {
		Failure string `bencode:"failure reason"`
		Peers   []byte `bencode:"peers"`
	}
	require.NoError(r.t, bencode.Unmarshal(body, &answer))
	var peers []netip.AddrPort
	for i := 0; i+6 <= len(answer.Peers); i += 6 {
		addr := netip.AddrFrom4([4]byte(answer.Peers[i : i+4]))
		peers = append(peers, netip.AddrPortFrom(addr, uint16(answer.Peers[i+4])<<8|uint16(answer.Peers[i+5])))
	}
	return peers, answer.Failure
}

// torrentOf decodes a .torrent and returns its announce URL and infohash.
func torrentOf(t *testing.T, data []byte) (announce, infoHash string, private bool) {
	t.Helper()
	mi, err := metainfo.Load(bytes.NewReader(data))
	require.NoError(t, err)
	info, err := mi.UnmarshalInfo()
	require.NoError(t, err)
	return mi.Announce, mi.HashInfoBytes().HexString(), info.Private != nil && *info.Private
}

// clientTorrent returns the .torrent of the Share torrent in the user's client.
func (r *rig) clientTorrent(hash string) []byte {
	r.t.Helper()
	data, _, _, err := r.sync.ExportTorrent(r.t.Context(), r.instance.ID, hash)
	require.NoError(r.t, err)
	return data
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

func listening(port int) bool {
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(context.Background(), "tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func TestAShareSeedsAShareTorrentThatQuisTrackerAnnounces(t *testing.T) {
	r := newRig(t)
	var registeredBeforeAdd bool
	r.stub.OnAdd = func(hash string) { registeredBeforeAdd = r.helpers.IsHelper(r.instance.ID, hash) }

	sh := r.start(share.Request{})

	require.Equal(t, share.StateActive, sh.State)
	require.NotEqual(t, r.hash, sh.HelperHash)
	require.True(t, registeredBeforeAdd, "qui writes the registry row before the add")
	require.True(t, r.stub.Has(sh.HelperHash))
	add := r.stub.LastParams("torrents/add")
	require.Equal(t, "true", add.Get("seedMode"))
	require.Equal(t, "true", add.Get("forced"))
	require.Equal(t, "false", add.Get("autoTMM"))
	require.Equal(t, r.dir, add.Get("savepath"))
	require.Equal(t, r.clock.Now().Add(7*24*time.Hour), sh.ExpiresAt)
	require.True(t, listening(r.port), "the Share tracker listens while a Share runs")

	base := "http://127.0.0.1:" + strconv.Itoa(r.port) + "/announce/"
	recipientURL, recipientHash, private := torrentOf(t, sh.TorrentFile)
	require.Equal(t, base+sh.RecipientSecret, recipientURL)
	require.Equal(t, sh.HelperHash, recipientHash)
	require.True(t, private)
	ownerURL, ownerHash, _ := torrentOf(t, r.clientTorrent(sh.HelperHash))
	require.Equal(t, base+sh.OwnerSecret, ownerURL)
	require.Equal(t, sh.HelperHash, ownerHash)
	require.NotEqual(t, sh.OwnerSecret, sh.RecipientSecret)

	require.Empty(t, r.announce(ownerURL, sh.HelperHash, 51413, 0, "started"))
	require.Equal(t, []netip.AddrPort{ownerAddress}, r.announce(recipientURL, sh.HelperHash, 6881, 70, "started"))

	listed := r.get(sh.ID)
	require.Len(t, listed.Peers, 1)
	require.Equal(t, "127.0.0.1:6881", listed.Peers[0].Address)
	require.Zero(t, listed.Downloaded)
}

func TestAShareEndsAtTheFirstCompleteDownload(t *testing.T) {
	r := newRig(t)
	sh := r.start(share.Request{})
	ownerURL, _, _ := torrentOf(t, r.clientTorrent(sh.HelperHash))
	recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
	r.announce(ownerURL, sh.HelperHash, 51413, 0, "started")
	r.announce(recipientURL, sh.HelperHash, 6881, 70, "started")
	r.announce(recipientURL, sh.HelperHash, 6881, 30, "")
	r.check()
	require.Equal(t, share.StateActive, r.get(sh.ID).State)
	require.Equal(t, int64(40), r.get(sh.ID).Downloaded)

	r.announce(recipientURL, sh.HelperHash, 6881, 0, "completed")
	r.check()

	r.requireEnded(sh.ID, "The recipient has the files.")
	require.False(t, listening(r.port), "the Share tracker stops with the last Share on it")
	ended := r.get(sh.ID)
	require.Equal(t, int64(70), ended.Downloaded)
	require.Equal(t, []share.Peer{{Address: "127.0.0.1:6881", Progress: 1, SeenAt: r.clock.Now()}}, ended.Peers)
}

func TestAShareWithoutTheEndAtTheFirstDownloadRunsOn(t *testing.T) {
	r := newRig(t)
	sh := r.start(share.Request{EndAtFirstDownload: new(false)})
	recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
	r.announce(recipientURL, sh.HelperHash, 6881, 70, "started")
	r.announce(recipientURL, sh.HelperHash, 6881, 0, "completed")
	r.check()
	require.Equal(t, share.StateActive, r.get(sh.ID).State)
}

func TestAShareEnds(t *testing.T) {
	for name, tc := range map[string]struct {
		act    func(r *rig, sh *share.Share)
		reason string
	}{
		"after the set days": {
			act:    func(r *rig, _ *share.Share) { r.clock.advance(3 * 24 * time.Hour) },
			reason: "The Share expired.",
		},
		"when the user stops it": {
			act:    func(r *rig, sh *share.Share) { require.NoError(r.t, r.svc.Stop(r.t.Context(), sh.ID)) },
			reason: "Stopped by the user.",
		},
		"when another client deletes the real torrent": {
			act:    func(r *rig, _ *share.Share) { r.stub.DeleteElsewhereKeepFiles(r.hash) },
			reason: "The torrent was deleted.",
		},
		"when another client moves the real torrent": {
			act: func(r *rig, _ *share.Share) {
				r.stub.Edit(r.hash, func(t *qbtstub.Torrent) { t.SavePath = filepath.Join(r.dir, "moved") })
			},
			reason: "The torrent was moved.",
		},
		"when another client renames a file of the real torrent": {
			act: func(r *rig, _ *share.Share) {
				r.stub.Edit(r.hash, func(t *qbtstub.Torrent) { t.Files = []string{"Show.S01E01.mkv", "Renamed.S01E02.mkv"} })
			},
			reason: "The files of the torrent were renamed.",
		},
		"when the Share torrent leaves the client": {
			act:    func(r *rig, sh *share.Share) { r.stub.DeleteElsewhereKeepFiles(sh.HelperHash) },
			reason: "The Share torrent is gone from the client.",
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			sh := r.start(share.Request{Days: 2})
			r.clock.advance(time.Hour)
			r.check()
			require.Equal(t, share.StateActive, r.get(sh.ID).State)

			tc.act(r, sh)
			r.check()

			r.requireEnded(sh.ID, tc.reason)
			require.False(t, listening(r.port))
		})
	}
}

func TestAShareEndsBeforeQuiChangesTheRealTorrent(t *testing.T) {
	for name, tc := range map[string]struct {
		act      func(r *rig) error
		endpoint string
		reason   string
	}{
		"a delete": {
			act: func(r *rig) error {
				return r.sync.DeleteTorrents(r.t.Context(), r.instance.ID, []string{r.hash}, false,
					qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorUser})
			},
			endpoint: "torrents/delete",
			reason:   "The torrent was deleted.",
		},
		"a move": {
			act: func(r *rig) error {
				return r.sync.SetLocation(r.t.Context(), r.instance.ID, []string{r.hash}, filepath.Join(r.dir, "moved"))
			},
			endpoint: "torrents/setLocation",
			reason:   "The torrent was moved.",
		},
		"a rename": {
			act: func(r *rig) error {
				return r.sync.RenameTorrentFile(r.t.Context(), r.instance.ID, r.hash,
					showName+"/Show.S01E02.mkv", showName+"/Renamed.S01E02.mkv")
			},
			endpoint: "torrents/renameFile",
			reason:   "The files of the torrent were renamed.",
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			sh := r.start(share.Request{})

			require.NoError(t, tc.act(r))

			ended := r.get(sh.ID)
			require.Equal(t, share.StateEnded, ended.State, "the Share ends before the change, with no check")
			require.Equal(t, tc.reason, ended.EndReason)
			require.False(t, r.stub.Has(sh.HelperHash))
			writes := r.stub.WriteLog()
			helperDelete := slices.Index(writes, "torrents/delete")
			require.GreaterOrEqual(t, helperDelete, 0)
			require.Equal(t, tc.endpoint, writes[len(writes)-1], "the change of the real torrent comes last")
			require.Less(t, helperDelete, len(writes)-1)
		})
	}
}

// outsideTracker is a tracker URL that no test contacts.
const outsideTracker = "https://tracker.example/announce"

// useOutsideTracker clears qui's own tracker and sets an outside tracker URL.
func (r *rig) useOutsideTracker() {
	r.t.Helper()
	require.NoError(r.t, r.trackerSettings.SaveSettings(r.t.Context(), sharetracker.Settings{}))
	require.NoError(r.t, share.NewSettingsStore(r.db).SaveSettings(r.t.Context(), share.Options{
		MaxActive: 10, PeersPerShare: 20, TrackerURL: outsideTracker,
		Defaults: share.Defaults{Days: 7, EndAtFirstDownload: true, FirstDownloaderOnly: true},
	}))
}

func TestAShareOnAnOutsideTrackerReadsThePeersOfTheClient(t *testing.T) {
	r := newRig(t)
	r.useOutsideTracker()

	sh := r.start(share.Request{})

	require.Equal(t, share.TrackerOutside, sh.Tracker)
	require.False(t, sh.FirstDownloaderOnly, "only qui's own tracker can let one downloader in")
	require.False(t, listening(r.port), "qui's own tracker stays off")
	recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
	require.Equal(t, outsideTracker, recipientURL)
	ownerURL, _, _ := torrentOf(t, r.clientTorrent(sh.HelperHash))
	require.Equal(t, outsideTracker, ownerURL)

	r.stub.Edit(sh.HelperHash, func(t *qbtstub.Torrent) {
		t.Uploaded = 35
		t.Peers = []qbt.TorrentPeer{{IP: "198.51.100.7", Port: 6881, Progress: 0.5}}
	})
	r.check()

	listed := r.get(sh.ID)
	require.Equal(t, share.StateActive, listed.State)
	require.Equal(t, []share.Peer{{Address: "198.51.100.7:6881", Progress: 0.5, SeenAt: r.clock.Now()}}, listed.Peers)
	require.Equal(t, int64(35), listed.Uploaded)
	require.Equal(t, int64(35), listed.Downloaded)

	r.stub.Edit(sh.HelperHash, func(t *qbtstub.Torrent) { t.Peers[0].Progress = 1 })
	r.check()

	r.requireEnded(sh.ID, "The recipient has the files.")
}

func TestAShareStaysEndingWhileTheClientKeepsTheShareTorrent(t *testing.T) {
	r := newRig(t)
	sh := r.start(share.Request{})
	r.stub.Reject = http.StatusServiceUnavailable

	require.NoError(t, r.svc.Stop(t.Context(), sh.ID))

	require.Equal(t, share.StateEnding, r.get(sh.ID).State)
	require.True(t, r.stub.Has(sh.HelperHash))
	require.False(t, listening(r.port), "the tracker forgets an ending Share")
	r.check()
	require.Equal(t, share.StateEnding, r.get(sh.ID).State, "qui retries while the client keeps the Share torrent")

	r.stub.Reject = 0
	r.check()

	r.requireEnded(sh.ID, "Stopped by the user.")
}

func TestASharesRunsOnAfterARestartOfQui(t *testing.T) {
	r := newRig(t)
	sh := r.start(share.Request{})
	ownerURL, _, _ := torrentOf(t, r.clientTorrent(sh.HelperHash))
	recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
	r.announce(ownerURL, sh.HelperHash, 51413, 0, "started")
	r.announce(recipientURL, sh.HelperHash, 6881, 70, "started")
	r.check()
	r.svc.Close()
	require.False(t, listening(r.port))

	r.svc = r.newService(rollout.Switches{TransfersAndShares: true})
	require.NoError(t, r.svc.Resume(t.Context()))

	require.True(t, listening(r.port), "the tracker starts again for the active Share")
	listed := r.get(sh.ID)
	require.Equal(t, share.StateActive, listed.State)
	require.Len(t, listed.Peers, 1, "the peers of the Share stay")
	r.announce(ownerURL, sh.HelperHash, 51413, 0, "started")
	require.Equal(t, []netip.AddrPort{ownerAddress}, r.announce(recipientURL, sh.HelperHash, 6881, 40, ""))
	r.announce(recipientURL, sh.HelperHash, 6881, 0, "completed")
	r.check()
	r.requireEnded(sh.ID, "The recipient has the files.")
}

func TestAShareKeepsToTheLimitsOfTheSettings(t *testing.T) {
	r := newRig(t)
	require.NoError(t, share.NewSettingsStore(r.db).SaveSettings(t.Context(), share.Options{
		MaxActive: 1, PeersPerShare: 1, Defaults: share.Defaults{Days: 7, EndAtFirstDownload: true},
	}))
	other := r.seed("Other.Show.S01.1080p-GRP")

	sh := r.start(share.Request{})
	_, err := r.svc.Start(t.Context(), share.Request{InstanceID: r.instance.ID, Hash: other})
	require.True(t, share.IsRefusal(err))
	require.EqualError(t, err, "Active Shares: 1 of 1. Stop a Share first, or raise the limit in Settings → Shares.")

	ownerURL, _, _ := torrentOf(t, r.clientTorrent(sh.HelperHash))
	recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
	r.announce(ownerURL, sh.HelperHash, 51413, 0, "started")
	require.NotEmpty(t, r.announce(recipientURL, sh.HelperHash, 6881, 70, "started"))
	_, failure := r.sendAnnounce("", recipientURL, sh.HelperHash, 6882, 70, "started")
	require.Equal(t, "this Share has as many peers as it takes", failure, "one peer for each Share")

	require.NoError(t, r.svc.Stop(t.Context(), sh.ID))
	r.start(share.Request{Hash: other})
}

func TestAShareIsRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		setup  func(r *rig)
		reason string
	}{
		"on qBittorrent 5.0 (WebAPI 2.11.2)": {
			setup:  func(r *rig) { r.stub.WebAPIVersion = "2.11.2" },
			reason: "Share needs qBittorrent 5.1 or newer. Update qBittorrent on seedbox.",
		},
		"without a public peer address": {
			setup:  func(r *rig) { r.stub.SetExternalAddress("", "") },
			reason: "qui does not know the public peer address of seedbox. Set it in the settings of the instance.",
		},
		"without a tracker": {
			setup: func(r *rig) {
				require.NoError(r.t, r.trackerSettings.SaveSettings(r.t.Context(), sharetracker.Settings{}))
			},
			reason: "Set a tracker in Settings → Shares: the port and the public address of qui's tracker, or a tracker URL.",
		},
		"for a torrent that a Share gives already": {
			setup:  func(r *rig) { r.start(share.Request{}) },
			reason: "A Share of this torrent runs already.",
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			tc.setup(r)
			adds := r.stub.Writes("torrents/add")

			_, err := r.svc.Start(t.Context(), share.Request{InstanceID: r.instance.ID, Hash: r.hash})

			require.True(t, share.IsRefusal(err), "%v", err)
			require.EqualError(t, err, tc.reason)
			require.Equal(t, adds, r.stub.Writes("torrents/add"), "a refused Share adds no torrent")
		})
	}
}

func TestSharesStayClosedWhileTheSwitchIsOff(t *testing.T) {
	r := newRig(t)
	sh := r.start(share.Request{})
	r.svc.Close()
	writes := len(r.stub.WriteLog())

	off := r.newService(rollout.Switches{})
	r.clock.advance(60 * 24 * time.Hour)
	off.Run(t.Context())
	require.NoError(t, off.Resume(t.Context()))
	require.NoError(t, off.Check(t.Context()))
	off.BeforeTorrentChange(t.Context(), r.instance.ID, []string{r.hash}, qbittorrent.TorrentChangeDelete)

	require.False(t, listening(r.port), "the tracker stays off")
	require.Len(t, r.stub.WriteLog(), writes, "the Share service changes nothing in qBittorrent")
	stored, err := share.NewStore(r.db).Get(t.Context(), sh.ID)
	require.NoError(t, err)
	require.Equal(t, share.StateActive, stored.State)
	_, err = off.Start(t.Context(), share.Request{InstanceID: r.instance.ID, Hash: r.hash})
	require.ErrorIs(t, err, share.ErrOff)
	_, err = off.List(t.Context())
	require.ErrorIs(t, err, share.ErrOff)
	_, err = off.Settings(t.Context())
	require.ErrorIs(t, err, share.ErrOff)
	require.ErrorIs(t, off.Stop(t.Context(), sh.ID), share.ErrOff)
}

func TestWithNoSharesAndDefaultSettingsQuiWorksAsBefore(t *testing.T) {
	r := newRig(t)
	require.NoError(t, r.trackerSettings.SaveSettings(t.Context(), sharetracker.Settings{}))

	require.NoError(t, r.svc.Resume(t.Context()))
	r.clock.advance(60 * 24 * time.Hour)
	r.check()
	require.NoError(t, r.sync.SetLocation(t.Context(), r.instance.ID, []string{r.hash}, filepath.Join(r.dir, "moved")))
	require.NoError(t, r.sync.DeleteTorrents(t.Context(), r.instance.ID, []string{r.hash}, false,
		qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorUser}))

	require.Equal(t, []string{"torrents/setLocation", "torrents/delete"}, r.stub.WriteLog())
	require.False(t, r.stub.Has(r.hash))
	list, err := r.svc.List(t.Context())
	require.NoError(t, err)
	require.Empty(t, list)
	settings, err := r.svc.Settings(t.Context())
	require.NoError(t, err)
	require.Equal(t, share.Options{MaxActive: 10, PeersPerShare: 20,
		Defaults: share.Defaults{Days: 7, EndAtFirstDownload: true, FirstDownloaderOnly: true}}, settings.Options)
}

func TestEndedSharesLeaveTheList(t *testing.T) {
	r := newRig(t)
	kept := r.start(share.Request{})
	removed := r.start(share.Request{Hash: r.seed("Other.Show.S01.1080p-GRP")})
	require.ErrorIs(t, r.svc.Remove(t.Context(), kept.ID), share.ErrNotEnded)
	require.NoError(t, r.svc.Stop(t.Context(), kept.ID))
	require.NoError(t, r.svc.Stop(t.Context(), removed.ID))

	require.NoError(t, r.svc.Remove(t.Context(), removed.ID))

	require.Equal(t, []int64{kept.ID}, r.ids())
	require.False(t, r.helpers.IsHelper(r.instance.ID, removed.HelperHash), "the registry row goes with the Share")
	require.True(t, r.helpers.IsHelper(r.instance.ID, kept.HelperHash))
	require.ErrorIs(t, r.svc.Remove(t.Context(), removed.ID), share.ErrNotFound)

	r.clock.advance(30 * 24 * time.Hour)
	r.check()
	require.Equal(t, []int64{kept.ID}, r.ids(), "an ended Share stays 30 days")

	r.clock.advance(time.Minute)
	r.check()
	require.Empty(t, r.ids())
	require.False(t, r.helpers.IsHelper(r.instance.ID, kept.HelperHash))
}

// ids lists the ids of the Shares, the newest first.
func (r *rig) ids() []int64 {
	r.t.Helper()
	list, err := r.svc.List(r.t.Context())
	require.NoError(r.t, err)
	var ids []int64
	for _, sh := range list {
		ids = append(ids, sh.ID)
	}
	return ids
}

func TestTheShareSettingsMoveTheTracker(t *testing.T) {
	r := newRig(t)
	r.start(share.Request{})
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = busy.Close() })
	before, err := r.svc.Settings(t.Context())
	require.NoError(t, err)

	changed := before
	changed.Tracker.Port = busy.Addr().(*net.TCPAddr).Port
	changed.Options.MaxActive = 3
	require.ErrorIs(t, r.svc.SaveSettings(t.Context(), changed), sharetracker.ErrPortBusy)
	after, err := r.svc.Settings(t.Context())
	require.NoError(t, err)
	require.Equal(t, before, after, "a busy port saves nothing")

	changed.Tracker.Port = freePort(t)
	require.NoError(t, r.svc.SaveSettings(t.Context(), changed))
	after, err = r.svc.Settings(t.Context())
	require.NoError(t, err)
	require.Equal(t, changed, after)
	require.True(t, listening(changed.Tracker.Port))
	require.False(t, listening(r.port))

	changed.Options.PeersPerShare = 0
	require.ErrorIs(t, r.svc.SaveSettings(t.Context(), changed), share.ErrInvalidSettings)
}

func TestUnlockLetsTheNextDownloaderIn(t *testing.T) {
	r := newRig(t)
	require.NoError(t, r.trackerSettings.SaveSettings(t.Context(), sharetracker.Settings{
		Port: r.port, PublicAddress: "http://127.0.0.1:" + strconv.Itoa(r.port), TrustedProxies: []string{"127.0.0.1"},
	}))
	sh := r.start(share.Request{FirstDownloaderOnly: new(true)})
	ownerURL, _, _ := torrentOf(t, r.clientTorrent(sh.HelperHash))
	recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
	const first, next = "198.51.100.7", "198.51.100.8"
	r.sendAnnounce(ownerAddress.Addr().String(), ownerURL, sh.HelperHash, 51413, 0, "started")
	peers, _ := r.sendAnnounce(first, recipientURL, sh.HelperHash, 6881, 70, "started")
	require.Equal(t, []netip.AddrPort{ownerAddress}, peers)
	r.sendAnnounce(first, recipientURL, sh.HelperHash, 6881, 70, "stopped")
	peers, _ = r.sendAnnounce(next, recipientURL, sh.HelperHash, 6881, 70, "started")
	require.Empty(t, peers, "the first downloader holds the Share")

	require.NoError(t, r.svc.Unlock(t.Context(), sh.ID))

	peers, _ = r.sendAnnounce(next, recipientURL, sh.HelperHash, 6881, 70, "")
	require.Equal(t, []netip.AddrPort{ownerAddress}, peers)
}

func TestUnlockIsRefused(t *testing.T) {
	r := newRig(t)
	open := r.start(share.Request{FirstDownloaderOnly: new(false)})
	err := r.svc.Unlock(t.Context(), open.ID)
	require.True(t, share.IsRefusal(err))
	require.EqualError(t, err, "The Share lets every downloader in already.")

	require.NoError(t, r.svc.Stop(t.Context(), open.ID))
	err = r.svc.Unlock(t.Context(), open.ID)
	require.EqualError(t, err, "The Share has ended.")

	r.useOutsideTracker()
	outside := r.start(share.Request{Hash: r.seed("Other.Show.S01.1080p-GRP")})
	err = r.svc.Unlock(t.Context(), outside.ID)
	require.EqualError(t, err, "Unlock needs qui's own tracker. With an outside tracker, every downloader gets the address.")
}
