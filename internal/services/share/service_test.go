// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package share_test

import (
	"bytes"
	"context"
	"fmt"
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
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/peeraddress"
	"github.com/autobrr/qui/internal/services/share"
	"github.com/autobrr/qui/internal/services/sharetracker"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const showName = "Show.S01.1080p-GRP"

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
	peers           *slowPeers
	svc             *share.Service
}

type slowPeers struct {
	share.PeerAddresses
	hold atomic.Pointer[func()]
}

func (p *slowPeers) PublicPeerAddress(ctx context.Context, instanceID int) (string, error) {
	if hold := p.hold.Swap(nil); hold != nil {
		(*hold)()
	}
	return p.PeerAddresses.PublicPeerAddress(ctx, instanceID)
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
	r.peers = &slowPeers{PeerAddresses: peeraddress.New(r.instances, r.sync)}
	r.svc = r.newService()
	return r
}

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

func (r *rig) newService() *share.Service {
	svc := share.NewService(share.Deps{
		Store:           share.NewStore(r.db),
		Settings:        share.NewSettingsStore(r.db),
		TrackerSettings: r.trackerSettings,
		Helpers:         r.helpers,
		Instances:       r.instances,
		Sync:            r.sync,
		Peers:           r.peers,
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

func (r *rig) requireEnded(id int64, code, reason string) {
	r.t.Helper()
	sh := r.get(id)
	require.Equal(r.t, share.StateEnded, sh.State)
	require.Equal(r.t, reason, sh.EndReason)
	require.Equal(r.t, code, sh.EndReasonCode())
	require.Equal(r.t, r.clock.Now(), sh.EndedAt)
	require.False(r.t, r.stub.Has(sh.HelperHash), "the Share torrent stays in the client")
	for _, name := range []string{"Show.S01E01.mkv", "Show.S01E02.mkv"} {
		require.FileExists(r.t, filepath.Join(r.dir, showName, name))
	}
}

func (r *rig) announce(announceURL, infoHash string, port, left int, event string) []netip.AddrPort {
	r.t.Helper()
	peers, failure := r.sendAnnounce("", announceURL, infoHash, port, left, event)
	require.Empty(r.t, failure)
	return peers
}

func (r *rig) sendAnnounce(from, announceURL, infoHash string, port, left int, event string) ([]netip.AddrPort, string) {
	r.t.Helper()
	resp, err := http.DefaultClient.Do(r.announceRequest(from, announceURL, infoHash, port, left, event))
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

func (r *rig) announceRequest(from, announceURL, infoHash string, port, left int, event string) *http.Request {
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
	return req
}

func torrentOf(t *testing.T, data []byte) (announce, infoHash string, private bool) {
	t.Helper()
	mi, err := metainfo.Load(bytes.NewReader(data))
	require.NoError(t, err)
	info, err := mi.UnmarshalInfo()
	require.NoError(t, err)
	return mi.Announce, mi.HashInfoBytes().HexString(), info.Private != nil && *info.Private
}

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
	require.Equal(t, []string{"qui-share"}, r.stub.Tags(sh.HelperHash), "a script of qBittorrent skips the Share torrent by its tag")
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

func TestQuisTrackerAnswersTheFirstAnnounceOfAShareTorrent(t *testing.T) {
	for name, otherShare := range map[string]bool{
		"the first Share":                false,
		"while another Share runs on it": true,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			if otherShare {
				r.start(share.Request{Hash: r.seed("Other.Show.S01.1080p-GRP")})
			}
			var status int
			var announceErr error
			r.stub.OnAdd = func(hash string) {
				unfinished, err := share.NewStore(r.db).Unfinished(t.Context())
				if err != nil {
					announceErr = err
					return
				}
				i := slices.IndexFunc(unfinished, func(sh share.Share) bool { return sh.HelperHash == hash })
				if i < 0 {
					announceErr = fmt.Errorf("no unfinished Share has the torrent %s", hash)
					return
				}
				announceURL := "http://127.0.0.1:" + strconv.Itoa(r.port) + "/announce/" + unfinished[i].OwnerSecret
				resp, err := http.DefaultClient.Do(r.announceRequest("", announceURL, hash, 51413, 0, "started"))
				if err != nil {
					announceErr = err
					return
				}
				status = resp.StatusCode
				_ = resp.Body.Close()
			}

			r.start(share.Request{})

			require.NoError(t, announceErr)
			require.Equal(t, http.StatusOK, status)
		})
	}
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

func TestAnEndReasonWithoutACodeIsOther(t *testing.T) {
	t.Parallel()

	require.Empty(t, (&share.Share{State: share.StateActive}).EndReasonCode())
	require.Equal(t, "other", (&share.Share{State: share.StateEnded, EndReason: "The client lost the files."}).EndReasonCode())
}

func TestAShareEnds(t *testing.T) {
	for name, tc := range map[string]struct {
		act    func(r *rig, sh *share.Share)
		code   string
		reason string
	}{
		"after the set days": {
			act:    func(r *rig, _ *share.Share) { r.clock.advance(3 * 24 * time.Hour) },
			code:   "expired",
			reason: "The Share expired.",
		},
		"when another client deletes the real torrent": {
			act:    func(r *rig, _ *share.Share) { r.stub.DeleteElsewhereKeepFiles(r.hash) },
			code:   "torrent_deleted",
			reason: "The torrent was deleted.",
		},
		"when another client moves the real torrent": {
			act: func(r *rig, _ *share.Share) {
				r.stub.Edit(r.hash, func(t *qbtstub.Torrent) { t.SavePath = filepath.Join(r.dir, "moved") })
			},
			code:   "torrent_moved",
			reason: "The torrent was moved.",
		},
		"when another client renames a file of the real torrent": {
			act: func(r *rig, _ *share.Share) {
				r.stub.Edit(r.hash, func(t *qbtstub.Torrent) { t.Files = []string{"Show.S01E01.mkv", "Renamed.S01E02.mkv"} })
			},
			code:   "files_renamed",
			reason: "The files of the torrent were renamed.",
		},
		"when the Share torrent leaves the client": {
			act:    func(r *rig, sh *share.Share) { r.stub.DeleteElsewhereKeepFiles(sh.HelperHash) },
			code:   "share_torrent_gone",
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

			r.requireEnded(sh.ID, tc.code, tc.reason)
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

const outsideTracker = "https://tracker.example/announce"

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

	r.requireEnded(sh.ID, "downloaded", "The recipient has the files.")
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

	r.requireEnded(sh.ID, "stopped", "Stopped by the user.")
}

func (r *rig) failWrites(column string) (stop func()) {
	r.t.Helper()
	_, err := r.db.ExecContext(r.t.Context(), fmt.Sprintf(`
		CREATE TRIGGER fail_share_%[1]s BEFORE UPDATE OF %[1]s ON shares
		BEGIN SELECT RAISE(ABORT, '%[1]s write failed'); END`, column))
	require.NoError(r.t, err)
	return func() {
		_, err := r.db.ExecContext(r.t.Context(), `DROP TRIGGER fail_share_`+column)
		require.NoError(r.t, err)
	}
}

func (r *rig) stored(id int64) *share.Share {
	r.t.Helper()
	sh, err := share.NewStore(r.db).Get(r.t.Context(), id)
	require.NoError(r.t, err)
	return sh
}

func TestTheCheckWritesTheActivityAgainAfterAFailedWrite(t *testing.T) {
	r := newRig(t)
	sh := r.start(share.Request{})
	recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
	r.announce(recipientURL, sh.HelperHash, 6881, 70, "started")
	stop := r.failWrites("peers_json")

	require.ErrorContains(t, r.svc.Check(t.Context()), "peers_json write failed")
	stop()
	r.check()

	require.Len(t, r.stored(sh.ID).Peers, 1, "the activity of the Share is saved")
}

func TestAnEndingShareKeepsItsActivityUntilQuiSavesIt(t *testing.T) {
	r := newRig(t)
	sh := r.start(share.Request{})
	recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
	r.announce(recipientURL, sh.HelperHash, 6881, 70, "started")
	stop := r.failWrites("peers_json")

	require.ErrorContains(t, r.svc.Stop(t.Context(), sh.ID), "peers_json write failed")
	require.Equal(t, share.StateEnding, r.stored(sh.ID).State)
	stop()
	r.check()

	r.requireEnded(sh.ID, "stopped", "Stopped by the user.")
	require.Len(t, r.stored(sh.ID).Peers, 1, "the activity of the Share stays")
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

	r.svc = r.newService()
	require.NoError(t, r.svc.Resume(t.Context()))

	require.True(t, listening(r.port), "the tracker starts again for the active Share")
	listed := r.get(sh.ID)
	require.Equal(t, share.StateActive, listed.State)
	require.Len(t, listed.Peers, 1, "the peers of the Share stay")
	r.announce(ownerURL, sh.HelperHash, 51413, 0, "started")
	require.Equal(t, []netip.AddrPort{ownerAddress}, r.announce(recipientURL, sh.HelperHash, 6881, 40, ""))
	require.Equal(t, int64(30), r.get(sh.ID).Downloaded)
	r.announce(recipientURL, sh.HelperHash, 6881, 0, "completed")
	r.check()
	r.requireEnded(sh.ID, "downloaded", "The recipient has the files.")
	ended := r.get(sh.ID)
	require.Equal(t, int64(70), ended.Downloaded)
	require.Equal(t, []share.Peer{{Address: "127.0.0.1:6881", Progress: 1, SeenAt: r.clock.Now()}}, ended.Peers)
}

func TestTheShareTrackerStartsAtTheNextCheckOnceItsPortIsFree(t *testing.T) {
	r := newRig(t)
	sh := r.start(share.Request{})
	r.svc.Close()
	busy, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:"+strconv.Itoa(r.port))
	require.NoError(t, err)

	r.svc = r.newService()
	require.ErrorIs(t, r.svc.Resume(t.Context()), sharetracker.ErrPortBusy)
	require.NoError(t, busy.Close())
	r.check()

	require.True(t, listening(r.port), "the tracker starts for the active Share")
	ownerURL, _, _ := torrentOf(t, r.clientTorrent(sh.HelperHash))
	recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
	r.announce(ownerURL, sh.HelperHash, 51413, 0, "started")
	require.Equal(t, []netip.AddrPort{ownerAddress}, r.announce(recipientURL, sh.HelperHash, 6881, 70, "started"))
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

func TestAStartThatTheUserCancelsAfterTheAddEndsItsShare(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(t.Context())
	r.stub.OnAdd = func(string) { cancel() }

	_, err := r.svc.Start(ctx, share.Request{InstanceID: r.instance.ID, Hash: r.hash})
	require.True(t, share.IsRefusal(err), "%v", err)
	require.False(t, listening(r.port), "the tracker stops with the Share that did not start")
	r.check()

	list, err := r.svc.List(t.Context())
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, share.StateEnded, list[0].State)
	require.False(t, r.stub.Has(list[0].HelperHash), "the Share torrent leaves the client")
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

func (r *rig) ids() []int64 {
	r.t.Helper()
	list, err := r.svc.List(r.t.Context())
	require.NoError(r.t, err)
	ids := make([]int64, 0, len(list))
	for _, sh := range list {
		ids = append(ids, sh.ID)
	}
	return ids
}

func TestTheShareSettingsMoveTheTracker(t *testing.T) {
	r := newRig(t)
	r.useOutsideTracker()
	r.start(share.Request{})
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = busy.Close() })
	before, err := r.svc.Settings(t.Context())
	require.NoError(t, err)

	changed := before
	changed.Tracker.Port = busy.Addr().(*net.TCPAddr).Port
	changed.Tracker.PublicAddress = "http://127.0.0.1:" + strconv.Itoa(changed.Tracker.Port)
	changed.Options.MaxActive = 3
	require.ErrorIs(t, r.svc.SaveSettings(t.Context(), changed), sharetracker.ErrPortBusy)
	after, err := r.svc.Settings(t.Context())
	require.NoError(t, err)
	require.Equal(t, before, after, "a busy port saves nothing")

	changed.Tracker.Port = freePort(t)
	changed.Tracker.PublicAddress = "http://127.0.0.1:" + strconv.Itoa(changed.Tracker.Port)
	require.NoError(t, r.svc.SaveSettings(t.Context(), changed), "a Share on the outside tracker keeps no port")
	after, err = r.svc.Settings(t.Context())
	require.NoError(t, err)
	require.Equal(t, changed, after)
	sh := r.start(share.Request{Hash: r.seed("Other.Show.S01.1080p-GRP")})
	require.True(t, listening(changed.Tracker.Port))
	recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
	require.Equal(t, changed.Tracker.PublicAddress+"/announce/"+sh.RecipientSecret, recipientURL)

	changed.Options.PeersPerShare = 0
	require.ErrorIs(t, r.svc.SaveSettings(t.Context(), changed), share.ErrInvalidSettings)
}

func TestTheShareSettingsKeepTheTrackerWhileASharesRunsOnIt(t *testing.T) {
	r := newRig(t)
	sh := r.start(share.Request{})
	before, err := r.svc.Settings(t.Context())
	require.NoError(t, err)

	for name, change := range map[string]func(*sharetracker.Settings){
		"a new port":           func(s *sharetracker.Settings) { s.Port = freePort(t) },
		"a new public address": func(s *sharetracker.Settings) { s.PublicAddress = "http://198.51.100.20:" + strconv.Itoa(s.Port) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := before
			change(&changed.Tracker)
			err := r.svc.SaveSettings(t.Context(), changed)
			require.True(t, share.IsRefusal(err), "%v", err)
			require.EqualError(t, err, "End the active Shares on qui's tracker first: their .torrent files carry the old address.")
			after, err := r.svc.Settings(t.Context())
			require.NoError(t, err)
			require.Equal(t, before, after, "a refused change saves nothing")
		})
	}
	require.True(t, listening(r.port), "the tracker stays for the active Share")

	changed := before
	changed.Options.MaxActive = 3
	require.NoError(t, r.svc.SaveSettings(t.Context(), changed), "a save that keeps the port and the address")
	ownerURL, _, _ := torrentOf(t, r.clientTorrent(sh.HelperHash))
	recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
	r.announce(ownerURL, sh.HelperHash, 51413, 0, "started")
	require.Equal(t, []netip.AddrPort{ownerAddress}, r.announce(recipientURL, sh.HelperHash, 6881, 70, "started"))
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

func TestTheFirstDownloaderHoldsTheShareAfterARestartOfQui(t *testing.T) {
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

	r.svc.Close()
	r.svc = r.newService()
	require.NoError(t, r.svc.Resume(t.Context()))

	r.sendAnnounce(ownerAddress.Addr().String(), ownerURL, sh.HelperHash, 51413, 0, "started")
	peers, _ = r.sendAnnounce(next, recipientURL, sh.HelperHash, 6881, 70, "started")
	require.Empty(t, peers, "the first downloader still holds the Share after a restart")
	peers, _ = r.sendAnnounce(first, recipientURL, sh.HelperHash, 7000, 60, "started")
	require.Equal(t, []netip.AddrPort{ownerAddress}, peers, "the first downloader goes on from a new port")

	r.svc.Close()
	r.svc = r.newService()
	require.NoError(t, r.svc.Resume(t.Context()))
	_, err := r.db.ExecContext(t.Context(), `ALTER TABLE share_settings RENAME TO share_settings_gone`)
	require.NoError(t, err)
	require.NoError(t, r.svc.Unlock(t.Context(), sh.ID))

	r.sendAnnounce(ownerAddress.Addr().String(), ownerURL, sh.HelperHash, 51413, 0, "")
	peers, _ = r.sendAnnounce(next, recipientURL, sh.HelperHash, 6881, 70, "")
	require.Equal(t, []netip.AddrPort{ownerAddress}, peers, "Unlock after a restart lets the next downloader in")
}

func TestAnAnnounceFromBeforeUnlockLeavesTheLockWithTheNextDownloader(t *testing.T) {
	r := newRig(t)
	require.NoError(t, r.trackerSettings.SaveSettings(t.Context(), sharetracker.Settings{
		Port: r.port, PublicAddress: "http://127.0.0.1:" + strconv.Itoa(r.port), TrustedProxies: []string{"127.0.0.1"},
	}))
	sh := r.start(share.Request{FirstDownloaderOnly: new(true)})
	ownerURL, _, _ := torrentOf(t, r.clientTorrent(sh.HelperHash))
	recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
	const first, next = "198.51.100.7", "198.51.100.8"
	r.sendAnnounce(ownerAddress.Addr().String(), ownerURL, sh.HelperHash, 51413, 0, "started")

	looking, release := make(chan struct{}), make(chan struct{})
	hold := func() {
		close(looking)
		<-release
	}
	r.peers.hold.Store(&hold)
	firstAnnounce := r.announceRequest(first, recipientURL, sh.HelperHash, 6881, 70, "started")
	answered := make(chan struct{})
	go func() {
		defer close(answered)
		if resp, err := http.DefaultClient.Do(firstAnnounce); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-looking
	require.NoError(t, r.svc.Unlock(t.Context(), sh.ID))
	peers, _ := r.sendAnnounce(next, recipientURL, sh.HelperHash, 6881, 70, "started")
	require.Equal(t, []netip.AddrPort{ownerAddress}, peers)
	close(release)
	<-answered

	r.svc.Close()
	r.svc = r.newService()
	require.NoError(t, r.svc.Resume(t.Context()))

	r.sendAnnounce(ownerAddress.Addr().String(), ownerURL, sh.HelperHash, 51413, 0, "started")
	peers, _ = r.sendAnnounce(next, recipientURL, sh.HelperHash, 6881, 70, "")
	require.Equal(t, []netip.AddrPort{ownerAddress}, peers, "the next downloader holds the Share after a restart")
}

func TestTheCheckStoresTheLockHolderAfterAFailedWrite(t *testing.T) {
	r := newRig(t)
	require.NoError(t, r.trackerSettings.SaveSettings(t.Context(), sharetracker.Settings{
		Port: r.port, PublicAddress: "http://127.0.0.1:" + strconv.Itoa(r.port), TrustedProxies: []string{"127.0.0.1"},
	}))
	sh := r.start(share.Request{FirstDownloaderOnly: new(true)})
	ownerURL, _, _ := torrentOf(t, r.clientTorrent(sh.HelperHash))
	recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
	const first = "198.51.100.7"
	r.sendAnnounce(ownerAddress.Addr().String(), ownerURL, sh.HelperHash, 51413, 0, "started")
	stop := r.failWrites("holder")

	peers, _ := r.sendAnnounce(first, recipientURL, sh.HelperHash, 6881, 70, "started")
	require.Equal(t, []netip.AddrPort{ownerAddress}, peers)
	require.ErrorContains(t, r.svc.Check(t.Context()), "holder write failed")
	stop()
	r.check()

	require.Equal(t, netip.MustParseAddr(first), r.stored(sh.ID).Holder, "the lock holder outlives a restart of qui")
}

func TestALockedOutDownloaderDoesNotEndTheShare(t *testing.T) {
	for _, event := range []string{"completed", "stopped"} {
		t.Run(event, func(t *testing.T) {
			r := newRig(t)
			require.NoError(t, r.trackerSettings.SaveSettings(t.Context(), sharetracker.Settings{
				Port: r.port, PublicAddress: "http://127.0.0.1:" + strconv.Itoa(r.port), TrustedProxies: []string{"127.0.0.1"},
			}))
			sh := r.start(share.Request{FirstDownloaderOnly: new(true)})
			ownerURL, _, _ := torrentOf(t, r.clientTorrent(sh.HelperHash))
			recipientURL, _, _ := torrentOf(t, sh.TorrentFile)
			const first, next = "198.51.100.7", "198.51.100.8"
			r.sendAnnounce(ownerAddress.Addr().String(), ownerURL, sh.HelperHash, 51413, 0, "started")
			r.sendAnnounce(first, recipientURL, sh.HelperHash, 6881, 70, "started")
			r.sendAnnounce(next, recipientURL, sh.HelperHash, 6881, 70, "started")

			r.sendAnnounce(next, recipientURL, sh.HelperHash, 6881, 0, event)
			r.check()
			require.Equal(t, share.StateActive, r.get(sh.ID).State)
			require.Zero(t, r.get(sh.ID).Downloaded)

			r.sendAnnounce(first, recipientURL, sh.HelperHash, 6881, 0, event)
			r.check()
			r.requireEnded(sh.ID, "downloaded", "The recipient has the files.")
		})
	}
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

func TestAShareOnAnOutsideTrackerEndsWhenTheClientSentEveryByte(t *testing.T) {
	for _, tc := range []struct {
		name  string
		peers []qbt.TorrentPeer
		ends  bool
	}{
		{name: "the downloader left the peer list", ends: true},
		{name: "another peer still downloads", peers: []qbt.TorrentPeer{{IP: "198.51.100.9", Port: 6881, Progress: 0.4}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.useOutsideTracker()
			sh := r.start(share.Request{})
			r.stub.Edit(sh.HelperHash, func(t *qbtstub.Torrent) {
				t.Uploaded = 66
				t.Peers = []qbt.TorrentPeer{{IP: "198.51.100.7", Port: 6881, Progress: 0.9}}
			})
			r.check()
			require.Equal(t, share.StateActive, r.get(sh.ID).State)

			r.stub.Edit(sh.HelperHash, func(t *qbtstub.Torrent) { t.Uploaded, t.Peers = 70, tc.peers })
			r.check()

			if !tc.ends {
				require.Equal(t, share.StateActive, r.get(sh.ID).State)
				return
			}
			r.requireEnded(sh.ID, "downloaded", "The recipient has the files.")
			require.Equal(t, int64(70), r.get(sh.ID).Downloaded)
		})
	}
}
