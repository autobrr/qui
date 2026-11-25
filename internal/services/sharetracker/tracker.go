// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sharetracker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"
)

const Interval = 5 * time.Minute

const lookupTimeout = 5 * time.Second

var ErrPortBusy = errors.New("the Share tracker port is in use")

type Share struct {
	ID                  int64
	InfoHash            [20]byte
	OwnerSecret         string
	RecipientSecret     string
	FirstDownloaderOnly bool
	Holder              netip.Addr
}

type Table struct {
	clients       map[string]announcer
	shares        map[int64]Share
	peersPerShare int
}

type announcer struct {
	share Share
	owner bool
}

func NewTable(shares []Share, peersPerShare int) *Table {
	t := &Table{clients: make(map[string]announcer, 2*len(shares)), shares: make(map[int64]Share, len(shares)), peersPerShare: peersPerShare}
	for _, s := range shares {
		t.clients[s.OwnerSecret] = announcer{share: s, owner: true}
		t.clients[s.RecipientSecret] = announcer{share: s}
		t.shares[s.ID] = s
	}
	return t
}

func (t *Table) lookup(secret string) (announcer, bool) {
	if t == nil || secret == "" {
		return announcer{}, false
	}
	a, ok := t.clients[secret]
	return a, ok
}

func (t *Table) share(id int64) (Share, bool) {
	if t == nil {
		return Share{}, false
	}
	s, ok := t.shares[id]
	return s, ok
}

func (t *Table) empty() bool {
	return t == nil || len(t.shares) == 0
}

type Shares interface {
	ActiveShares() *Table
	PublicPeerAddress(ctx context.Context, shareID int64) (netip.AddrPort, error)
}

type Event string

const (
	EventNone      Event = ""
	EventStarted   Event = "started"
	EventCompleted Event = "completed"
	EventStopped   Event = "stopped"
)

type Report struct {
	ShareID    int64
	Owner      bool
	Peer       netip.AddrPort
	Uploaded   int64
	Downloaded int64
	Left       int64
	Event      Event
	Locked     bool
	Blocked    bool
}

type Reports interface {
	Announced(ctx context.Context, r Report)
}

type Config struct {
	Shares  Shares
	Reports Reports
	Host    string
	Now     func() time.Time
}

type Tracker struct {
	shares  Shares
	reports Reports
	host    string
	now     func() time.Time

	handler atomic.Pointer[http.Handler]

	runMu  sync.Mutex
	server *http.Server
	port   int
	served chan struct{}

	limit rateLimit

	mu     sync.Mutex
	swarms map[int64]*swarm
}

func New(cfg Config) *Tracker {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Tracker{
		shares:  cfg.Shares,
		reports: cfg.Reports,
		host:    cfg.Host,
		now:     now,
		swarms:  make(map[int64]*swarm),
		limit:   rateLimit{windows: make(map[netip.Addr]window)},
	}
}

func (t *Tracker) Start(ctx context.Context, settings Settings) error {
	proxies, err := settings.trustedProxies()
	if err != nil {
		return err
	}
	t.runMu.Lock()
	defer t.runMu.Unlock()
	if settings.Port == 0 || t.shares.ActiveShares().empty() {
		return t.stopLocked(ctx)
	}
	handler := clientAddress(proxies)(http.HandlerFunc(t.serve))
	t.handler.Store(&handler)
	if t.server != nil && t.port == settings.Port {
		return nil
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort(t.host, strconv.Itoa(settings.Port)))
	if errors.Is(err, errAddrInUse) {
		return fmt.Errorf("%w: %d", ErrPortBusy, settings.Port)
	}
	if err != nil {
		return fmt.Errorf("share tracker: listen on port %d: %w", settings.Port, err)
	}
	if err := t.stopLocked(ctx); err != nil {
		log.Warn().Err(err).Msg("share tracker: the old port did not close cleanly")
	}
	server := &http.Server{
		Handler:                      http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { (*t.handler.Load()).ServeHTTP(w, r) }),
		ReadHeaderTimeout:            5 * time.Second,
		ReadTimeout:                  10 * time.Second,
		WriteTimeout:                 10 * time.Second,
		IdleTimeout:                  time.Minute,
		MaxHeaderBytes:               4 << 10,
		DisableGeneralOptionsHandler: true,
	}
	served := make(chan struct{})
	go func() {
		defer close(served)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Int("port", settings.Port).Msg("share tracker: stopped serving")
		}
	}()
	t.server, t.port, t.served = server, settings.Port, served
	log.Info().Int("port", settings.Port).Msg("share tracker: listening")
	return nil
}

func (t *Tracker) CheckPort(ctx context.Context, port int) error {
	if port == 0 {
		return nil
	}
	t.runMu.Lock()
	defer t.runMu.Unlock()
	if t.server != nil && t.port == port {
		return nil
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort(t.host, strconv.Itoa(port)))
	if errors.Is(err, errAddrInUse) {
		return fmt.Errorf("%w: %d", ErrPortBusy, port)
	}
	if err != nil {
		return fmt.Errorf("share tracker: listen on port %d: %w", port, err)
	}
	return listener.Close()
}

func (t *Tracker) Stop(ctx context.Context) error {
	t.runMu.Lock()
	defer t.runMu.Unlock()
	return t.stopLocked(ctx)
}

func (t *Tracker) stopLocked(ctx context.Context) error {
	if t.server == nil {
		return nil
	}
	err := t.server.Shutdown(ctx)
	if err != nil {
		err = errors.Join(err, t.server.Close())
	}
	<-t.served
	log.Info().Int("port", t.port).Msg("share tracker: stopped")
	t.server, t.port, t.served = nil, 0, nil
	return err
}

func clientAddress(proxies []netip.Prefix) func(http.Handler) http.Handler {
	if len(proxies) == 0 {
		return middleware.ClientIPFromRemoteAddr
	}
	list := make([]string, len(proxies))
	for i, p := range proxies {
		list[i] = p.String()
	}
	return middleware.ClientIPFromXFF(list...)
}

func notFound(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
}

func (t *Tracker) serve(w http.ResponseWriter, r *http.Request) {
	secret, ok := strings.CutPrefix(r.URL.Path, "/announce/")
	if r.Method != http.MethodGet || !ok || strings.Contains(secret, "/") {
		log.Trace().Str("method", r.Method).Msg("share tracker: 404: the request is not an announce")
		notFound(w)
		return
	}
	from := middleware.GetClientIPAddr(r.Context())
	if !from.IsValid() {
		log.Debug().Str("remoteAddr", r.RemoteAddr).Bool("forwardedFor", r.Header.Get("X-Forwarded-For") != "").
			Msg("share tracker: 404: no client address because all addresses are trusted proxies, or X-Forwarded-For is missing or invalid")
		notFound(w)
		return
	}
	if !t.limit.allow(from, t.now()) {
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	table := t.shares.ActiveShares()
	client, ok := table.lookup(secret)
	if !ok {
		log.Trace().Stringer("from", from).Msg("share tracker: 404: no active Share has this announce URL")
		notFound(w)
		return
	}
	req, err := parseAnnounce(r.URL.RawQuery)
	if err != nil {
		fail(w, err.Error())
		return
	}
	if req.infoHash != client.share.InfoHash {
		log.Trace().Int64("shareID", client.share.ID).Stringer("from", from).Msg("share tracker: 404: the infohash does not match the Share")
		notFound(w)
		return
	}
	peer := netip.AddrPortFrom(from, req.port)
	answer, err := t.join(table, client, peer, req)
	if err != nil {
		fail(w, err.Error())
		return
	}
	if answer.owner {
		ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
		address, err := t.shares.PublicPeerAddress(ctx, client.share.ID)
		cancel()
		if err != nil {
			log.Debug().Err(err).Int64("shareID", client.share.ID).Msg("share tracker: no public peer address for the user's client")
		} else {
			answer.peers = append([]netip.AddrPort{address}, answer.peers...)
		}
	}
	t.reports.Announced(r.Context(), Report{
		ShareID:    client.share.ID,
		Owner:      client.owner,
		Peer:       peer,
		Uploaded:   req.uploaded,
		Downloaded: req.downloaded,
		Left:       req.left,
		Event:      req.event,
		Locked:     answer.locked,
		Blocked:    answer.blocked,
	})
	reply(w, answer.peers[:min(len(answer.peers), req.numwant)])
}
