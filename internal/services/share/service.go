// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package share

import (
	"context"
	"encoding/hex"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/torrentadd"
	"github.com/autobrr/qui/internal/services/sharetracker"
	"github.com/autobrr/qui/internal/services/transfer"
)

const keepEnded = 30 * 24 * time.Hour

type Config struct {
	Polls        int
	PollInterval time.Duration
	Tick         time.Duration
}

func DefaultConfig() Config {
	return Config{Polls: 30, PollInterval: time.Second, Tick: 30 * time.Second}
}

// PeerAddresses gives the public peer address of an instance as host:port,
// or an error that wraps peeraddress.ErrNoAddress.
type PeerAddresses interface {
	PublicPeerAddress(ctx context.Context, instanceID int) (string, error)
}

type Deps struct {
	Store           *Store
	Settings        *SettingsStore
	TrackerSettings *sharetracker.SettingsStore
	// Helpers is the helper registry. A Share torrent has its row before qui
	// adds it.
	Helpers   *transfer.Store
	Instances *models.InstanceStore
	Sync      *qbittorrent.SyncManager
	Peers     PeerAddresses
	// TrackerHost is the address that qui's own tracker listens on; empty
	// listens on every address.
	TrackerHost string
	Now         func() time.Time
	Config      Config
}

// Service runs the Shares and owns qui's own Share tracker. It listens only
// while a Share on qui's own tracker is active.
type Service struct {
	deps    Deps
	adder   *torrentadd.Adder
	tracker *sharetracker.Tracker
	wake    chan struct{}

	// opMu runs one change of the Shares at a time, so that the limits hold
	// and one Share ends once.
	opMu sync.Mutex
	// holderMu orders each save of the holder of a lock with Unlock.
	holderMu sync.Mutex

	table atomic.Pointer[sharetracker.Table]

	mu     sync.Mutex
	live   map[int64]*live
	active map[torrentKey][]int64
}

type torrentKey struct {
	instanceID int
	hash       string
}

type live struct {
	instanceID         int
	size               int64
	endAtFirstDownload bool
	peers              map[string]Peer
	downloaded         int64
	uploaded           int64
	completed          bool
	dirty              bool
}

func (l *live) activity() Activity {
	return Activity{Peers: sortedPeers(maps.Values(l.peers)), Downloaded: l.downloaded, Uploaded: l.uploaded}
}

// NewService returns a Share service. It does nothing until Resume or Run,
// and nothing at all while the switch for Transfers and Shares is off.
func NewService(deps Deps) *Service {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	s := &Service{
		deps:   deps,
		adder:  torrentadd.New(deps.Sync),
		wake:   make(chan struct{}, 1),
		live:   map[int64]*live{},
		active: map[torrentKey][]int64{},
	}
	s.tracker = sharetracker.New(sharetracker.Config{Shares: s, Reports: s, Host: deps.TrackerHost, Now: deps.Now})
	return s
}

func (s *Service) Run(ctx context.Context) {
	if err := s.Resume(ctx); err != nil {
		log.Error().Err(err).Msg("share: cannot resume the Shares")
	}
	tick := time.Tick(s.deps.Config.Tick)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
		case <-s.wake:
		}
		if err := s.Check(ctx); err != nil && ctx.Err() == nil {
			log.Error().Err(err).Msg("share: cannot check the Shares")
		}
	}
}

func (s *Service) Resume(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.refresh(ctx)
}

func (s *Service) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.tracker.Stop(ctx); err != nil {
		log.Warn().Err(err).Msg("share: the Share tracker did not stop cleanly")
	}
}

// refresh reads the unfinished Shares into memory, and makes qui's own
// tracker listen while one of them is active on it. Call it under opMu.
func (s *Service) refresh(ctx context.Context) error {
	list, err := s.deps.Store.Unfinished(ctx)
	if err != nil {
		return err
	}
	options, err := s.deps.Settings.Settings(ctx)
	if err != nil {
		return err
	}
	trackerSettings, err := s.deps.TrackerSettings.Settings(ctx)
	if err != nil {
		return err
	}

	var tracked []sharetracker.Share
	invalid := 0
	var invalidErr error
	s.mu.Lock()
	known := s.live
	s.live = make(map[int64]*live, len(list))
	s.active = map[torrentKey][]int64{}
	for i := range list {
		sh := &list[i]
		l := known[sh.ID]
		if l == nil {
			l = &live{instanceID: sh.InstanceID, size: sh.Size, endAtFirstDownload: sh.EndAtFirstDownload,
				peers: map[string]Peer{}, downloaded: sh.Downloaded, uploaded: sh.Uploaded}
			for _, p := range sh.Peers {
				l.peers[p.Address] = p
			}
		}
		s.live[sh.ID] = l
		if sh.State != StateActive {
			continue
		}
		key := torrentKey{sh.InstanceID, sh.Hash}
		s.active[key] = append(s.active[key], sh.ID)
		if sh.Tracker == TrackerOwn {
			infoHash, err := infoHashOf(sh.HelperHash)
			if err != nil {
				log.Debug().Err(err).Int64("share", sh.ID).Msg("share: the Share torrent has no valid infohash")
				invalid++
				invalidErr = err
				continue
			}
			tracked = append(tracked, sharetracker.Share{ID: sh.ID, InfoHash: infoHash, OwnerSecret: sh.OwnerSecret,
				RecipientSecret: sh.RecipientSecret, FirstDownloaderOnly: sh.FirstDownloaderOnly, Holder: sh.Holder})
		}
	}
	s.mu.Unlock()
	if invalid > 0 {
		log.Error().Err(invalidErr).Int("shares", invalid).Msg("share: Share torrents have no valid infohash")
	}

	s.table.Store(sharetracker.NewTable(tracked, options.PeersPerShare))
	return s.tracker.Start(ctx, trackerSettings)
}

func (s *Service) ActiveShares() *sharetracker.Table {
	return s.table.Load()
}

func (s *Service) PublicPeerAddress(ctx context.Context, shareID int64) (netip.AddrPort, error) {
	s.mu.Lock()
	l := s.live[shareID]
	s.mu.Unlock()
	if l == nil {
		return netip.AddrPort{}, fmt.Errorf("%w: %d", ErrNotFound, shareID)
	}
	address, err := s.deps.Peers.PublicPeerAddress(ctx, l.instanceID)
	if err != nil {
		return netip.AddrPort{}, err
	}
	return resolve(ctx, address)
}

// resolve turns host:port into an address. A host name resolves to its
// first address.
func resolve(ctx context.Context, hostPort string) (netip.AddrPort, error) {
	if addr, err := netip.ParseAddrPort(hostPort); err == nil {
		return addr, nil
	}
	host, portText, err := net.SplitHostPort(hostPort)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("public peer address %q: %w", hostPort, err)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("public peer address %q: invalid port", hostPort)
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return netip.AddrPort{}, fmt.Errorf("public peer address %q: cannot resolve %s: %w", hostPort, host, err)
	}
	return netip.AddrPortFrom(addrs[0].Unmap(), uint16(port)), nil
}

// Announced keeps what one announce to qui's own tracker reported: the bytes
// that the user's client sent, and the address and the progress of a
// recipient. It stores the address of a recipient that takes the lock before
// the tracker answers, so that the lock outlives a restart of qui. A recipient
// that completed wakes Run. The progress of a recipient that the lock keeps
// out does not count.
func (s *Service) Announced(ctx context.Context, r sharetracker.Report) {
	if r.Locked {
		if err := s.saveHolder(context.WithoutCancel(ctx), r.ShareID, r.Peer.Addr()); err != nil {
			log.Error().Err(err).Int64("share", r.ShareID).Msg("share: cannot store who holds the lock; the next check tries again")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.live[r.ShareID]
	if l == nil {
		return
	}
	l.dirty = true
	if r.Owner {
		l.uploaded = max(l.uploaded, r.Uploaded)
		return
	}
	address := r.Peer.String()
	before, known := l.peers[address]
	progress := 1.0
	if l.size > 0 {
		progress = float64(max(l.size-r.Left, 0)) / float64(l.size)
	}
	l.peers[address] = Peer{Address: address, Progress: progress, SeenAt: s.deps.Now().UTC()}
	if r.Blocked {
		return
	}
	l.downloaded = max(l.downloaded, l.size-r.Left)
	if r.Event == sharetracker.EventCompleted || (r.Left == 0 && known && before.Progress < 1) {
		l.completed = true
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// saveHolder stores the address that took the lock of a Share, unless an
// Unlock gave the lock to another downloader since the announce took it.
func (s *Service) saveHolder(ctx context.Context, shareID int64, holder netip.Addr) error {
	s.holderMu.Lock()
	defer s.holderMu.Unlock()
	if s.tracker.Holder(shareID) != holder {
		return nil
	}
	return s.deps.Store.SetHolder(ctx, shareID, holder)
}

func infoHashOf(hexHash string) ([20]byte, error) {
	b, err := hex.DecodeString(hexHash)
	if err != nil || len(b) != 20 {
		return [20]byte{}, fmt.Errorf("infohash %q is not 40 hex digits", hexHash)
	}
	return [20]byte(b), nil
}

func lower(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}
