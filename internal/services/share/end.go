// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package share

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"net"
	"slices"
	"strconv"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/transfer"
)

// The reasons why a Share ends, for the user.
const (
	reasonDownloaded = "The recipient has the files."
	reasonExpired    = "The Share expired."
	reasonStopped    = "Stopped by the user."
	reasonDeleted    = "The torrent was deleted."
	reasonMoved      = "The torrent was moved."
	reasonRenamed    = "The files of the torrent were renamed."
	reasonGone       = "The Share torrent is gone from the client."
)

// end ends an active Share: the tracker forgets it, and qui removes the
// Share torrent from the client, never its files. The Share stays ending
// while the client is offline. by names what ended it, for the log. Call it
// under opMu.
func (s *Service) end(ctx context.Context, sh *Share, reason, by string) error {
	sh.State, sh.EndReason = StateEnding, reason
	if err := s.deps.Store.SetState(ctx, sh.ID, StateEnding, reason, sh.EndedAt); err != nil {
		return err
	}
	log.Info().Int64("share", sh.ID).Int("instanceID", sh.InstanceID).Str("reason", reason).Str("by", by).Msg("share: ending")
	if err := s.refresh(ctx); err != nil {
		log.Error().Err(err).Msg("share: cannot update the Share tracker")
	}
	return s.removeTorrent(ctx, sh)
}

// removeTorrent removes the Share torrent of an ending Share, and ends the
// Share when the torrent is gone from the client. Call it under opMu.
func (s *Service) removeTorrent(ctx context.Context, sh *Share) error {
	gone, err := s.gone(ctx, sh)
	if err == nil && !gone {
		if err = s.deps.Sync.DeleteTorrents(ctx, sh.InstanceID, []string{sh.HelperHash}, false,
			qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorUser}); err == nil {
			gone, err = s.gone(ctx, sh)
		}
	}
	if err != nil || !gone {
		log.Warn().Err(err).Int64("share", sh.ID).Msg("share: the Share torrent is not gone yet; qui retries")
		return nil
	}
	s.mu.Lock()
	l := s.live[sh.ID]
	delete(s.live, sh.ID)
	s.mu.Unlock()
	if l != nil {
		if err := s.deps.Store.SaveActivity(ctx, sh.ID, l.activity()); err != nil {
			return err
		}
	}
	sh.State, sh.EndedAt = StateEnded, s.deps.Now().UTC()
	if err := s.deps.Store.SetState(ctx, sh.ID, StateEnded, sh.EndReason, sh.EndedAt); err != nil {
		return err
	}
	log.Info().Int64("share", sh.ID).Str("reason", sh.EndReason).Msg("share: ended")
	return nil
}

// Check ends each active Share whose end condition holds, retries the
// removal of each ending Share, starts or stops qui's own tracker for the
// active Shares, which retries a start that failed, and removes the rows of
// the Shares that ended more than 30 days ago, with the registry rows of
// their Share torrents. Run calls it at each tick.
func (s *Service) Check(ctx context.Context) error {
	if !s.on() {
		return nil
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()

	unfinished, err := s.deps.Store.Unfinished(ctx)
	if err != nil {
		return err
	}
	views := map[int]*clientView{}
	var errs []error
	for i := range unfinished {
		sh := &unfinished[i]
		if sh.State == StateEnding {
			errs = append(errs, s.removeTorrent(ctx, sh))
			continue
		}
		view := views[sh.InstanceID]
		if view == nil {
			view = s.readClient(ctx, sh.InstanceID, unfinished)
			views[sh.InstanceID] = view
		}
		if reason := s.endReason(ctx, sh, view); reason != "" {
			errs = append(errs, s.end(ctx, sh, reason, "the check of the Shares"))
			continue
		}
		errs = append(errs, s.saveActivity(ctx, sh.ID))
	}
	errs = append(errs, s.refresh(ctx), s.removeOld(ctx))
	return errors.Join(errs...)
}

// clientView is one fresh read of the torrents of the Shares on an
// instance. err is set while the client is offline.
type clientView struct {
	torrents map[string]*qbt.Torrent
	err      error
}

func (s *Service) readClient(ctx context.Context, instanceID int, shares []Share) *clientView {
	var hashes []string
	for _, sh := range shares {
		if sh.InstanceID == instanceID {
			hashes = append(hashes, sh.Hash, sh.HelperHash)
		}
	}
	torrents, err := s.deps.Sync.GetTorrentRefsFresh(ctx, instanceID, qbittorrent.TorrentRefFilter{Hashes: hashes})
	view := &clientView{torrents: map[string]*qbt.Torrent{}, err: err}
	for _, t := range torrents {
		view.torrents[lower(t.Hash)] = t
	}
	return view
}

// endReason returns why the active Share ends now, or "".
func (s *Service) endReason(ctx context.Context, sh *Share, view *clientView) string {
	s.mu.Lock()
	completed := s.live[sh.ID] != nil && s.live[sh.ID].completed
	s.mu.Unlock()
	switch {
	case completed && sh.EndAtFirstDownload:
		return reasonDownloaded
	case !s.deps.Now().Before(sh.ExpiresAt):
		return reasonExpired
	case view.err != nil:
		return ""
	}
	torrent := view.torrents[sh.Hash]
	switch {
	case torrent == nil:
		return reasonDeleted
	case torrent.SavePath != sh.SavePath:
		return reasonMoved
	}
	helper := view.torrents[sh.HelperHash]
	if helper == nil {
		return reasonGone
	}
	files, err := s.deps.Sync.GetTorrentFiles(qbittorrent.WithForceFilesRefresh(ctx), sh.InstanceID, sh.Hash)
	if err != nil || files == nil {
		return ""
	}
	list := slices.SortedFunc(slices.Values(*files), func(a, b qbt.TorrentFile) int { return a.Index - b.Index })
	if !slices.EqualFunc(list, sh.Files, func(f qbt.TorrentFile, name string) bool { return f.Name == name }) {
		return reasonRenamed
	}
	if sh.Tracker == TrackerOutside && s.readPeers(ctx, sh, helper) && sh.EndAtFirstDownload {
		return reasonDownloaded
	}
	return ""
}

// readPeers keeps the connected peers and the uploaded bytes of the Share
// torrent of a Share on an outside tracker, as best effort, and reports
// whether a peer has every piece.
func (s *Service) readPeers(ctx context.Context, sh *Share, helper *qbt.Torrent) bool {
	peers, err := s.deps.Sync.GetTorrentPeers(ctx, sh.InstanceID, sh.HelperHash)
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.live[sh.ID]
	if l == nil {
		return false
	}
	l.uploaded, l.dirty = max(l.uploaded, helper.Uploaded), true
	if err != nil {
		log.Debug().Err(err).Int64("share", sh.ID).Msg("share: cannot read the peers of the Share torrent")
		return false
	}
	complete := false
	now := s.deps.Now().UTC()
	for _, p := range peers.Peers {
		address := net.JoinHostPort(p.IP, strconv.Itoa(p.Port))
		l.peers[address] = Peer{Address: address, Progress: p.Progress, SeenAt: now}
		l.downloaded = max(l.downloaded, int64(p.Progress*float64(l.size)))
		complete = complete || p.Progress >= 1
	}
	return complete
}

func (s *Service) saveActivity(ctx context.Context, id int64) error {
	s.mu.Lock()
	l := s.live[id]
	if l == nil || !l.dirty {
		s.mu.Unlock()
		return nil
	}
	l.dirty = false
	activity := l.activity()
	s.mu.Unlock()
	if err := s.deps.Store.SaveActivity(ctx, id, activity); err != nil {
		s.mu.Lock()
		l.dirty = true
		s.mu.Unlock()
		return err
	}
	return nil
}

// removeOld removes the Shares that ended more than 30 days ago.
func (s *Service) removeOld(ctx context.Context) error {
	ids, err := s.deps.Store.EndedBefore(ctx, s.deps.Now().Add(-keepEnded))
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.removeRow(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// removeRow removes the row of an ended Share and the registry rows of its
// Share torrent.
func (s *Service) removeRow(ctx context.Context, id int64) error {
	sh, err := s.deps.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if sh.State != StateEnded {
		return fmt.Errorf("%w: %d", ErrNotEnded, id)
	}
	if err := s.deps.Helpers.RemoveHelpers(ctx, transfer.OwnerShare, id); err != nil {
		return err
	}
	return s.deps.Store.Remove(ctx, id)
}

// gone reports whether a fresh read of the client does not list the Share
// torrent.
func (s *Service) gone(ctx context.Context, sh *Share) (bool, error) {
	left, err := s.deps.Sync.GetTorrentRefsFresh(ctx, sh.InstanceID, qbittorrent.TorrentRefFilter{Hashes: []string{sh.HelperHash}})
	return len(left) == 0, err
}

func sortedPeers(peers iter.Seq[Peer]) []Peer {
	return slices.SortedFunc(peers, func(a, b Peer) int { return cmp.Compare(a.Address, b.Address) })
}

// changeReasons gives the end reason of a Share for each change of its real
// torrent.
var changeReasons = map[qbittorrent.TorrentChange]string{
	qbittorrent.TorrentChangeDelete: reasonDeleted,
	qbittorrent.TorrentChangeMove:   reasonMoved,
	qbittorrent.TorrentChangeRename: reasonRenamed,
}

// BeforeTorrentChange ends the active Shares of the torrents before qui
// deletes, moves, or renames them, so that the Share torrent leaves the
// client first. It looks up the Shares before it takes opMu: the delete of a
// Share torrent comes back here with a hash that no Share has. While the
// switch is off, no Share is active in memory.
func (s *Service) BeforeTorrentChange(ctx context.Context, instanceID int, hashes []string, change qbittorrent.TorrentChange) {
	ids := make([]int64, 0, len(hashes))
	s.mu.Lock()
	for _, hash := range hashes {
		ids = append(ids, s.active[torrentKey{instanceID, lower(hash)}]...)
	}
	s.mu.Unlock()
	if len(ids) == 0 {
		return
	}

	s.opMu.Lock()
	defer s.opMu.Unlock()
	failed := 0
	var lastErr error
	for _, id := range ids {
		sh, err := s.deps.Store.Get(ctx, id)
		if err == nil && sh.State == StateActive {
			err = s.end(ctx, sh, changeReasons[change], "a change of the torrent in qui")
		}
		if err != nil {
			log.Debug().Err(err).Int64("share", id).Msg("share: cannot end the Share before the change of its torrent")
			failed++
			lastErr = err
		}
	}
	if failed > 0 {
		log.Error().Err(lastErr).Int("instanceID", instanceID).Int("failed", failed).
			Msg("share: cannot end Shares before the change of their torrents")
	}
}
