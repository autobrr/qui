// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sharetracker_test

import (
	"context"
	"errors"
	"io"
	"iter"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/sharetracker"
)

const (
	shareID         = 7
	ownerSecret     = "owner-secret-7"
	recipientSecret = "recipient-secret-7"
)

var (
	infoHash     = [20]byte([]byte("share-torrent-hash-7"))
	ownerAddress = netip.MustParseAddrPort("203.0.113.10:51413")
	errNoAddress = errors.New("no public peer address")
)

func share() sharetracker.Share {
	return sharetracker.Share{ID: shareID, InfoHash: infoHash, OwnerSecret: ownerSecret, RecipientSecret: recipientSecret}
}

type shares struct {
	table atomic.Pointer[sharetracker.Table]
	hold  atomic.Pointer[hold]

	mu      sync.Mutex
	address map[int64]netip.AddrPort
}

type hold struct {
	read    chan struct{}
	release chan struct{}
}

func (s *shares) ActiveShares() *sharetracker.Table {
	table := s.table.Load()
	if h := s.hold.Swap(nil); h != nil {
		close(h.read)
		<-h.release
	}
	return table
}

func (s *shares) PublicPeerAddress(_ context.Context, id int64) (netip.AddrPort, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	address, ok := s.address[id]
	if !ok {
		return netip.AddrPort{}, errNoAddress
	}
	return address, nil
}

func (s *shares) set(list ...sharetracker.Share) {
	s.table.Store(sharetracker.NewTable(list, 20))
}

func (s *shares) setAddress(id int64, address netip.AddrPort) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.address[id] = address
}

type reports struct {
	mu   sync.Mutex
	list []sharetracker.Report
}

func (r *reports) Announced(_ context.Context, report sharetracker.Report) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.list = append(r.list, report)
}

func (r *reports) all() []sharetracker.Report {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.list)
}

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
	t       *testing.T
	shares  *shares
	reports *reports
	clock   *clock
	tracker *sharetracker.Tracker
	port    int
	base    string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{
		t:       t,
		shares:  &shares{address: map[int64]netip.AddrPort{shareID: ownerAddress}},
		reports: &reports{},
		clock:   &clock{now: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)},
	}
	r.shares.set(share())
	r.tracker = sharetracker.New(sharetracker.Config{
		Shares: r.shares, Reports: r.reports, Host: "127.0.0.1", Now: r.clock.Now,
	})
	r.port = freePort(t)
	r.base = "http://127.0.0.1:" + strconv.Itoa(r.port)
	require.NoError(t, r.tracker.Start(t.Context(), sharetracker.Settings{Port: r.port, TrustedProxies: []string{"127.0.0.1"}}))
	t.Cleanup(func() { require.NoError(t, r.tracker.Stop(context.Background())) })
	return r
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

type client struct {
	secret string
	from   string
	port   int
}

var (
	owner     = client{secret: ownerSecret, from: "192.0.2.1", port: 51413}
	recipient = client{secret: recipientSecret, from: "198.51.100.7", port: 6881}
)

type announce struct {
	left  int64
	event string
	extra url.Values
}

func (c client) query(a announce) url.Values {
	q := url.Values{}
	q.Set("info_hash", string(infoHash[:]))
	q.Set("peer_id", "-qB5100-abcdefghijkl")
	q.Set("port", strconv.Itoa(c.port))
	q.Set("uploaded", "0")
	q.Set("downloaded", "0")
	q.Set("left", strconv.FormatInt(a.left, 10))
	q.Set("compact", "1")
	if a.event != "" {
		q.Set("event", a.event)
	}
	maps.Copy(q, a.extra)
	return q
}

type answer struct {
	Status   int         `bencode:"-"`
	Header   http.Header `bencode:"-"`
	Body     []byte      `bencode:"-"`
	Interval int         `bencode:"interval"`
	Peers    string      `bencode:"peers"`
	Peers6   string      `bencode:"peers6"`
	Failure  string      `bencode:"failure reason"`
}

func (r *rig) get(path string, header http.Header) answer {
	r.t.Helper()
	req, err := http.NewRequestWithContext(r.t.Context(), http.MethodGet, r.base+path, nil)
	require.NoError(r.t, err)
	maps.Copy(req.Header, header)
	return r.do(req)
}

func (r *rig) do(req *http.Request) answer {
	r.t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(r.t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(r.t, err)
	a := answer{Status: resp.StatusCode, Header: resp.Header, Body: body}
	if resp.StatusCode == http.StatusOK {
		require.NoError(r.t, bencode.Unmarshal(body, &a), "body %q", body)
	}
	return a
}

func (r *rig) announce(c client, a announce) answer {
	r.t.Helper()
	header := http.Header{}
	if c.from != "" {
		header.Set("X-Forwarded-For", c.from)
	}
	return r.get("/announce/"+c.secret+"?"+c.query(a).Encode(), header)
}

func (a answer) peers(t *testing.T) []netip.AddrPort {
	t.Helper()
	require.Equal(t, http.StatusOK, a.Status, "body %q", a.Body)
	require.Empty(t, a.Failure)
	var list []netip.AddrPort
	for chunk := range chunks(t, a.Peers, 6) {
		list = append(list, netip.AddrPortFrom(netip.AddrFrom4([4]byte([]byte(chunk[:4]))), uint16(chunk[4])<<8|uint16(chunk[5])))
	}
	for chunk := range chunks(t, a.Peers6, 18) {
		list = append(list, netip.AddrPortFrom(netip.AddrFrom16([16]byte([]byte(chunk[:16]))), uint16(chunk[16])<<8|uint16(chunk[17])))
	}
	return list
}

func chunks(t *testing.T, s string, size int) iter.Seq[string] {
	t.Helper()
	require.Zero(t, len(s)%size, "compact peers %q", s)
	return func(yield func(string) bool) {
		for i := 0; i < len(s); i += size {
			if !yield(s[i : i+size]) {
				return
			}
		}
	}
}

func addr(s string) netip.AddrPort { return netip.MustParseAddrPort(s) }

func TestTheRecipientGetsTheOwnersPublicPeerAddress(t *testing.T) {
	t.Parallel()
	r := newRig(t)

	r.announce(owner, announce{event: "started"})
	got := r.announce(recipient, announce{left: 1000, event: "started"})

	require.Equal(t, []netip.AddrPort{ownerAddress}, got.peers(t))
	require.Equal(t, 300, got.Interval)

	r.shares.setAddress(shareID, addr("203.0.113.99:51413"))
	require.Equal(t, []netip.AddrPort{addr("203.0.113.99:51413")}, r.announce(recipient, announce{left: 900}).peers(t), "a new external address of the user's client")

	r.announce(owner, announce{event: "stopped"})
	require.Empty(t, r.announce(recipient, announce{left: 900}).peers(t), "the user's client stopped")
}

func TestTheRecipientGetsNoAddressForTheUsersClientWithoutAPublicPeerAddress(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.announce(owner, announce{event: "started"})

	r.shares.mu.Lock()
	delete(r.shares.address, shareID)
	r.shares.mu.Unlock()
	got := r.announce(recipient, announce{left: 1000, event: "started"})

	require.Empty(t, got.peers(t))
}

func requireBare404(t *testing.T, got answer) {
	t.Helper()
	require.Equal(t, http.StatusNotFound, got.Status)
	require.Empty(t, got.Body)
	require.Empty(t, got.Header.Values("Server"))
}

func TestTheTrackerAnswersOnlyTheSecretsAndInfohashesOfActiveShares(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	other := sharetracker.Share{ID: 8, InfoHash: [20]byte([]byte("other-torrent-hash-8")), OwnerSecret: "owner-secret-8", RecipientSecret: "recipient-secret-8"}
	r.shares.set(share(), other)

	ok := r.announce(recipient, announce{left: 1000, event: "started"})
	require.Equal(t, http.StatusOK, ok.Status)
	require.Empty(t, ok.Header.Values("Server"))

	for name, c := range map[string]client{
		"unknown secret":                {secret: "guessed-secret", from: recipient.from, port: 6881},
		"secret of another Share":       {secret: "recipient-secret-8", from: recipient.from, port: 6881},
		"owner secret of another Share": {secret: "owner-secret-8", from: recipient.from, port: 6881},
		"empty secret":                  {secret: "", from: recipient.from, port: 6881},
	} {
		t.Run(name, func(t *testing.T) {
			requireBare404(t, r.announce(c, announce{left: 1000}))
		})
	}

	r.shares.set(other)
	requireBare404(t, r.announce(recipient, announce{left: 1000}))
	requireBare404(t, r.announce(owner, announce{}))
	require.Len(t, r.reports.all(), 1)
}

func TestTheTrackerAnswersEveryOtherRequestWithABare404(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	valid := "/announce/" + recipientSecret + "?" + recipient.query(announce{left: 1000}).Encode()

	for _, path := range []string{"/announce/", "/scrape/" + recipientSecret, "/announce/../announce/" + recipientSecret} {
		t.Run(path, func(t *testing.T) {
			requireBare404(t, r.get(path, http.Header{"X-Forwarded-For": {recipient.from}}))
		})
	}
	post, err := http.NewRequestWithContext(t.Context(), http.MethodPost, r.base+valid, nil)
	require.NoError(t, err)
	post.Header.Set("X-Forwarded-For", recipient.from)
	requireBare404(t, r.do(post))
	require.Empty(t, r.reports.all())
}

func TestTheTrackerReportsEachAnnounce(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	counts := func(uploaded, downloaded, left int64, event string) announce {
		return announce{left: left, event: event, extra: url.Values{
			"uploaded":   {strconv.FormatInt(uploaded, 10)},
			"downloaded": {strconv.FormatInt(downloaded, 10)},
		}}
	}

	r.announce(owner, counts(0, 0, 0, "started"))
	r.announce(recipient, counts(0, 0, 4096, "started"))
	r.announce(recipient, counts(0, 1024, 3072, ""))
	r.announce(owner, counts(1024, 0, 0, ""))
	r.announce(recipient, counts(0, 4096, 0, "completed"))
	r.announce(recipient, counts(0, 4096, 0, "stopped"))

	ownerPeer, recipientPeer := addr("192.0.2.1:51413"), addr("198.51.100.7:6881")
	require.Equal(t, []sharetracker.Report{
		{ShareID: shareID, Owner: true, Peer: ownerPeer, Event: sharetracker.EventStarted},
		{ShareID: shareID, Peer: recipientPeer, Left: 4096, Event: sharetracker.EventStarted},
		{ShareID: shareID, Peer: recipientPeer, Downloaded: 1024, Left: 3072, Event: sharetracker.EventNone},
		{ShareID: shareID, Owner: true, Peer: ownerPeer, Uploaded: 1024, Event: sharetracker.EventNone},
		{ShareID: shareID, Peer: recipientPeer, Downloaded: 4096, Event: sharetracker.EventCompleted},
		{ShareID: shareID, Peer: recipientPeer, Downloaded: 4096, Event: sharetracker.EventStopped},
	}, r.reports.all())
}

func TestOnlyTheFirstDownloaderGetsTheUsersAddressUntilAnUnlock(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	locked := share()
	locked.FirstDownloaderOnly = true
	r.shares.set(locked)
	first := client{secret: recipientSecret, from: "198.51.100.1", port: 6881}
	second := client{secret: recipientSecret, from: "198.51.100.2", port: 6882}
	seeder := client{secret: recipientSecret, from: "198.51.100.3", port: 6883}

	r.announce(owner, announce{event: "started"})
	require.Empty(t, r.announce(seeder, announce{left: 0, event: "started"}).peers(t), "a client that downloads nothing takes no lock")
	require.Equal(t, []netip.AddrPort{ownerAddress}, r.announce(first, announce{left: 1000, event: "started"}).peers(t))
	require.Empty(t, r.announce(second, announce{left: 1000, event: "started"}).peers(t))
	require.Empty(t, r.announce(seeder, announce{left: 0}).peers(t))
	require.Equal(t, []netip.AddrPort{addr("198.51.100.1:6881")}, r.announce(owner, announce{}).peers(t))

	restarted := client{secret: recipientSecret, from: "198.51.100.1", port: 7000}
	require.Equal(t, []netip.AddrPort{ownerAddress}, r.announce(restarted, announce{left: 900, event: "started"}).peers(t), "the lock holds the address of the first downloader")

	r.tracker.Unlock(shareID)
	require.Equal(t, []netip.AddrPort{ownerAddress}, r.announce(second, announce{left: 1000}).peers(t))
	require.Empty(t, r.announce(first, announce{left: 900}).peers(t))
	require.Equal(t, []netip.AddrPort{addr("198.51.100.2:6882")}, r.announce(owner, announce{}).peers(t))
}

func TestAnAnnounceWithAnOlderTableKeepsTheLockOfANewShare(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	locked := sharetracker.Share{ID: 8, InfoHash: infoHash, OwnerSecret: "owner-secret-8", RecipientSecret: "recipient-secret-8", FirstDownloaderOnly: true}
	r.shares.setAddress(locked.ID, ownerAddress)
	lockedOwner := client{secret: locked.OwnerSecret, from: "192.0.2.1", port: 51413}
	first := client{secret: locked.RecipientSecret, from: "198.51.100.1", port: 6881}
	second := client{secret: locked.RecipientSecret, from: "198.51.100.2", port: 6882}

	h := &hold{read: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(h.release) })
	t.Cleanup(release)
	r.shares.hold.Store(h)
	older := make(chan struct{})
	go func() {
		defer close(older)
		r.announce(recipient, announce{left: 1000})
	}()
	<-h.read

	r.shares.set(share(), locked)
	r.announce(lockedOwner, announce{event: "started"})
	require.Equal(t, []netip.AddrPort{ownerAddress}, r.announce(first, announce{left: 1000, event: "started"}).peers(t))
	release()
	<-older

	r.announce(lockedOwner, announce{})
	require.Empty(t, r.announce(second, announce{left: 1000, event: "started"}).peers(t), "the lock holds the first downloader")
}

func TestAShareTakesAtMostItsPeersPerShare(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.announce(owner, announce{event: "started"})
	recipientAt := func(i int) client {
		return client{secret: recipientSecret, from: "198.51.100." + strconv.Itoa(i), port: 6881}
	}

	for i := range 20 {
		require.Empty(t, r.announce(recipientAt(i), announce{left: 1000, event: "started"}).Failure)
	}
	full := r.announce(recipientAt(20), announce{left: 1000, event: "started"})
	require.Equal(t, http.StatusOK, full.Status)
	require.NotEmpty(t, full.Failure)
	require.Len(t, r.announce(owner, announce{}).peers(t), 20)
	require.Len(t, r.announce(recipientAt(0), announce{left: 900}).peers(t), 20, "a client of the Share keeps its place")

	r.announce(recipientAt(0), announce{left: 900, event: "stopped"})
	require.Len(t, r.announce(recipientAt(20), announce{left: 1000, event: "started"}).peers(t), 20)
	require.Len(t, r.reports.all(), 20+1+1+1+1+1, "the refused announce has no report")
}

func TestEachAddressGetsThirtyAnnouncesAMinute(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	busy := client{secret: recipientSecret, from: "198.51.100.1", port: 6881}
	calm := client{secret: recipientSecret, from: "198.51.100.2", port: 6881}
	require.Equal(t, http.StatusOK, r.announce(calm, announce{left: 1000}).Status)
	r.clock.advance(30 * time.Second)

	for range 30 {
		require.Equal(t, http.StatusOK, r.announce(busy, announce{left: 1000}).Status)
	}
	limited := r.announce(busy, announce{left: 1000})
	require.Equal(t, http.StatusTooManyRequests, limited.Status)
	require.Empty(t, limited.Body)
	require.Equal(t, http.StatusTooManyRequests, r.announce(client{secret: "guessed-secret", from: busy.from, port: 6881}, announce{}).Status)
	require.Equal(t, http.StatusOK, r.announce(calm, announce{left: 1000}).Status)

	r.clock.advance(31 * time.Second)
	require.Equal(t, http.StatusOK, r.announce(calm, announce{left: 1000}).Status)
	require.Equal(t, http.StatusTooManyRequests, r.announce(busy, announce{left: 1000}).Status)
	r.clock.advance(29 * time.Second)
	require.Equal(t, http.StatusOK, r.announce(busy, announce{left: 1000}).Status)
	require.Len(t, r.reports.all(), 34)
}

func TestTheTrackerChecksPortAndNumwant(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.announce(owner, announce{event: "started"})
	r.announce(client{secret: recipientSecret, from: "198.51.100.1", port: 6881}, announce{left: 1000})
	r.announce(client{secret: recipientSecret, from: "198.51.100.2", port: 6881}, announce{left: 1000})

	require.Len(t, r.announce(owner, announce{extra: url.Values{"numwant": {"1"}}}).peers(t), 1)
	require.Empty(t, r.announce(owner, announce{extra: url.Values{"numwant": {"0"}}}).peers(t))
	for name, extra := range map[string]url.Values{
		"port 0":               {"port": {"0"}},
		"port 65536":           {"port": {"65536"}},
		"numwant -1":           {"numwant": {"-1"}},
		"numwant not a number": {"numwant": {"ten"}},
	} {
		t.Run(name, func(t *testing.T) {
			q := owner.query(announce{extra: extra})
			got := r.get("/announce/"+ownerSecret+"?"+q.Encode(), http.Header{"X-Forwarded-For": {owner.from}})
			require.Equal(t, http.StatusOK, got.Status)
			require.NotEmpty(t, got.Failure)
		})
	}
	require.Len(t, r.reports.all(), 5)
}

func TestAClientThatStopsAnnouncingDropsOutAfterTwoIntervals(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	silent := client{secret: recipientSecret, from: "198.51.100.1", port: 6881}
	active := client{secret: recipientSecret, from: "198.51.100.2", port: 6882}

	r.announce(owner, announce{event: "started"})
	r.announce(silent, announce{left: 1000, event: "started"})
	r.announce(active, announce{left: 1000, event: "started"})
	r.clock.advance(sharetracker.Interval)
	r.announce(active, announce{left: 900})
	r.clock.advance(sharetracker.Interval)
	require.ElementsMatch(t, []netip.AddrPort{addr("198.51.100.1:6881"), addr("198.51.100.2:6882")}, r.announce(owner, announce{}).peers(t))

	r.clock.advance(sharetracker.Interval)
	require.Equal(t, []netip.AddrPort{addr("198.51.100.2:6882")}, r.announce(owner, announce{}).peers(t))

	r.clock.advance(2*sharetracker.Interval + time.Second)
	require.Empty(t, r.announce(active, announce{left: 800}).peers(t), "the user's client stopped announcing")
}

func TestTheTrackerTrustsAForwardedAddressOnlyFromATrustedProxy(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	forwarded := func(chain string) netip.AddrPort {
		t.Helper()
		before := len(r.reports.all())
		r.get("/announce/"+recipientSecret+"?"+recipient.query(announce{left: 1000, extra: url.Values{"ip": {"192.0.2.99"}}}).Encode(), http.Header{"X-Forwarded-For": {chain}})
		reports := r.reports.all()
		require.Len(t, reports, before+1)
		return reports[before].Peer
	}

	require.Equal(t, addr("198.51.100.7:6881"), forwarded("198.51.100.7"))
	require.Equal(t, addr("198.51.100.7:6881"), forwarded("192.0.2.66, 198.51.100.7"), "the client cannot name its own address before the proxy")
	require.Equal(t, addr("198.51.100.7:6881"), forwarded("198.51.100.7, 127.0.0.1"), "a chain of trusted proxies")

	require.NoError(t, r.tracker.Start(t.Context(), sharetracker.Settings{Port: r.port, TrustedProxies: []string{"10.0.0.0/8"}}))
	require.Equal(t, addr("127.0.0.1:6881"), forwarded("198.51.100.7"))

	require.NoError(t, r.tracker.Start(t.Context(), sharetracker.Settings{Port: r.port}))
	require.Equal(t, addr("127.0.0.1:6881"), forwarded("198.51.100.7"))
}

func TestTheTrackerAnswersAClientWithoutAnAddressWithABare404(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	query := "/announce/" + recipientSecret + "?" + recipient.query(announce{left: 1000}).Encode()

	requireBare404(t, r.get(query, nil))
	requireBare404(t, r.get(query, http.Header{"X-Forwarded-For": {"127.0.0.1"}}))
	require.Empty(t, r.reports.all())
}

func TestTheTrackerTrustsAProxyWrittenAsAnIPv4MappedAddress(t *testing.T) {
	t.Parallel()
	for _, proxy := range []string{"::ffff:127.0.0.1", "::ffff:127.0.0.1/128", "::ffff:127.0.0.0/104"} {
		t.Run(proxy, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			require.NoError(t, r.tracker.Start(t.Context(), sharetracker.Settings{Port: r.port, TrustedProxies: []string{proxy}}))

			r.announce(recipient, announce{left: 1000})

			require.Equal(t, addr("198.51.100.7:6881"), r.reports.all()[0].Peer)
		})
	}
}

func TestTheTrackerGivesIPv4AndIPv6Peers(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	ipv6 := client{secret: recipientSecret, from: "2001:db8::7", port: 6881}
	ipv4 := client{secret: recipientSecret, from: "198.51.100.7", port: 6882}
	r.shares.setAddress(shareID, addr("[2001:db8::10]:51413"))

	r.announce(owner, announce{event: "started"})
	got := r.announce(ipv6, announce{left: 1000, event: "started"})
	require.Equal(t, []netip.AddrPort{addr("[2001:db8::10]:51413")}, got.peers(t))
	require.Empty(t, got.Peers)
	require.ElementsMatch(t, []netip.AddrPort{addr("[2001:db8::10]:51413"), addr("[2001:db8::7]:6881")}, r.announce(ipv4, announce{left: 1000, event: "started"}).peers(t), "without the lock, a recipient also gets the other recipients")

	got = r.announce(owner, announce{})
	require.ElementsMatch(t, []netip.AddrPort{addr("[2001:db8::7]:6881"), addr("198.51.100.7:6882")}, got.peers(t))
	require.Len(t, got.Peers, 6)
	require.Len(t, got.Peers6, 18)
}

func requireListening(t *testing.T, port int, want bool) {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err == nil {
		require.NoError(t, conn.Close())
	}
	require.Equal(t, want, err == nil, "listening on %d: %v", port, err)
}

func TestTheTrackerListensOnlyWithAnActiveShareAndAPort(t *testing.T) {
	t.Parallel()
	s := &shares{address: map[int64]netip.AddrPort{}}
	tracker := sharetracker.New(sharetracker.Config{Shares: s, Reports: &reports{}, Host: "127.0.0.1"})
	t.Cleanup(func() { require.NoError(t, tracker.Stop(context.Background())) })
	port := freePort(t)

	require.NoError(t, tracker.Start(t.Context(), sharetracker.Settings{Port: port}))
	requireListening(t, port, false)
	s.set()
	require.NoError(t, tracker.Start(t.Context(), sharetracker.Settings{Port: port}))
	requireListening(t, port, false)

	s.set(share())
	require.NoError(t, tracker.Start(t.Context(), sharetracker.Settings{Port: port}))
	requireListening(t, port, true)
	require.NoError(t, tracker.Start(t.Context(), sharetracker.Settings{}))
	requireListening(t, port, false)

	require.NoError(t, tracker.Start(t.Context(), sharetracker.Settings{Port: port}))
	requireListening(t, port, true)
	s.set()
	require.NoError(t, tracker.Start(t.Context(), sharetracker.Settings{Port: port}))
	requireListening(t, port, false)

	s.set(share())
	require.NoError(t, tracker.Start(t.Context(), sharetracker.Settings{Port: port}))
	require.NoError(t, tracker.Stop(context.Background()))
	requireListening(t, port, false)
}

func TestTheTrackerReportsABusyPortAndKeepsItsOldPort(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = busy.Close() })
	busyPort := busy.Addr().(*net.TCPAddr).Port

	err = r.tracker.Start(t.Context(), sharetracker.Settings{Port: busyPort})
	require.ErrorIs(t, err, sharetracker.ErrPortBusy)
	require.Equal(t, http.StatusOK, r.announce(recipient, announce{left: 1000}).Status)

	newPort := freePort(t)
	require.NoError(t, r.tracker.Start(t.Context(), sharetracker.Settings{Port: newPort}))
	requireListening(t, newPort, true)
	requireListening(t, r.port, false)
}

func TestTheTrackerRefusesALargeRequest(t *testing.T) {
	t.Parallel()
	r := newRig(t)

	got := r.get("/announce/"+recipientSecret+"?"+recipient.query(announce{left: 1000}).Encode(), http.Header{"X-Padding": {strings.Repeat("a", 16<<10)}})

	require.Equal(t, http.StatusRequestHeaderFieldsTooLarge, got.Status)
	require.Empty(t, r.reports.all())
}

func TestEndingSharesWhileAnnouncesRun(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	list := make([]sharetracker.Share, 8)
	for i := range list {
		list[i] = sharetracker.Share{
			ID:                  int64(100 + i),
			InfoHash:            infoHash,
			OwnerSecret:         "owner-" + strconv.Itoa(i),
			RecipientSecret:     "recipient-" + strconv.Itoa(i),
			FirstDownloaderOnly: i%2 == 0,
		}
		r.shares.setAddress(list[i].ID, ownerAddress)
	}
	r.shares.set(list...)

	start := make(chan struct{})
	done := make(chan struct{})
	var announcers sync.WaitGroup
	for g := range 8 {
		announcers.Go(func() {
			<-start
			for i := range 25 {
				s := list[(g+i)%len(list)]
				c := client{secret: s.RecipientSecret, from: "198.51.100." + strconv.Itoa(g), port: 6881 + i}
				if i%3 == 0 {
					c.secret = s.OwnerSecret
				}
				got := r.announce(c, announce{left: int64(1000 - i), event: []string{"started", "", "completed", "stopped"}[i%4]})
				assert.Contains(t, []int{http.StatusOK, http.StatusNotFound}, got.Status)
			}
		})
	}
	var swapper sync.WaitGroup
	swapper.Go(func() {
		<-start
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			default:
			}
			r.shares.set(list[i%len(list):]...)
			r.tracker.Unlock(list[i%len(list)].ID)
			r.clock.advance(time.Second)
		}
	})
	close(start)
	announcers.Wait()
	close(done)
	swapper.Wait()

	r.shares.set(list[4:]...)
	requireBare404(t, r.announce(client{secret: list[0].RecipientSecret, from: "192.0.2.200", port: 6881}, announce{left: 1000}))
}
