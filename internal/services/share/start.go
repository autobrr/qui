// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package share

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/helpertorrent"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/torrentadd"
	"github.com/autobrr/qui/internal/services/peeraddress"
	"github.com/autobrr/qui/internal/services/sharetracker"
	"github.com/autobrr/qui/internal/services/transfer"
)

type Request struct {
	InstanceID          int
	Hash                string
	Days                int
	EndAtFirstDownload  *bool
	FirstDownloaderOnly *bool
}

type RefusalError struct{ Reason string }

func (r *RefusalError) Error() string { return r.Reason }

func refuse(format string, args ...any) error {
	return &RefusalError{Reason: fmt.Sprintf(format, args...)}
}

func (s *Service) Start(ctx context.Context, req Request) (*Share, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	sh, err := s.prepare(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := s.deps.Store.Create(ctx, &sh.Share); err != nil {
		return nil, err
	}
	if err := s.deps.Helpers.RegisterHelper(ctx, transfer.Helper{
		InstanceID: sh.InstanceID, Hash: sh.HelperHash, Random: []byte(sh.salt), Owner: transfer.OwnerShare, OwnerID: sh.ID,
	}); err != nil {
		s.fail(ctx, &sh.Share, "qui could not record the Share torrent.")
		return nil, err
	}
	if err := s.refresh(ctx); err != nil {
		reason := "qui's tracker cannot start: " + err.Error()
		if errors.Is(err, sharetracker.ErrPortBusy) {
			reason = "Another program uses the port of qui's tracker. Pick another port in Settings → Shares."
		}
		s.fail(ctx, &sh.Share, reason)
		return nil, refuse("%s", reason)
	}
	res := s.adder.Add(ctx, torrentadd.Request{
		InstanceID: sh.InstanceID,
		Torrent:    sh.ownerTorrent,
		Hash:       sh.HelperHash,
		Settings: torrentadd.Settings{
			SavePath:        sh.SavePath,
			Tags:            []string{transfer.ShareHelperTag},
			ContentLayout:   qbt.ContentLayout(sh.layout),
			Stopped:         new(false),
			Forced:          true,
			AutoTMM:         new(false),
			UseDownloadPath: new(false),
			SeedMode:        true,
		},
		Polls:        s.deps.Config.Polls,
		PollInterval: s.deps.Config.PollInterval,
	})
	if res.Outcome != torrentadd.Verified {
		reason := fmt.Sprintf("qBittorrent on %s did not seed the Share torrent: %s", sh.instanceName, describe(res))
		s.fail(ctx, &sh.Share, reason)
		return nil, refuse("%s", reason)
	}
	log.Info().Int64("share", sh.ID).Int("instanceID", sh.InstanceID).Str("hash", sh.Hash).Str("tracker", string(sh.Tracker)).
		Msg("share: started by the user")
	return &sh.Share, nil
}

func describe(res torrentadd.Result) string {
	switch {
	case res.Err != nil:
		return res.Err.Error()
	case res.Torrent != nil:
		return "qBittorrent shows " + string(res.Torrent.State)
	}
	return "qBittorrent does not list the torrent"
}

func (s *Service) fail(ctx context.Context, sh *Share, reason string) {
	if err := s.end(context.WithoutCancel(ctx), sh, reason, "a failed start"); err != nil {
		log.Error().Err(err).Int64("share", sh.ID).Msg("share: cannot end the Share that did not start")
	}
}

type prepared struct {
	Share
	instanceName string
	salt         string
	layout       string
	ownerTorrent []byte
}

func (s *Service) prepare(ctx context.Context, req Request) (*prepared, error) {
	hash := lower(req.Hash)
	if hash == "" {
		return nil, refuse("Name the torrent to share.")
	}
	options, err := s.deps.Settings.Settings(ctx)
	if err != nil {
		return nil, err
	}
	trackerSettings, err := s.deps.TrackerSettings.Settings(ctx)
	if err != nil {
		return nil, err
	}
	kind, base, err := trackerOf(trackerSettings, options)
	if err != nil {
		return nil, err
	}
	days := cmp.Or(req.Days, options.Defaults.Days)
	if days < 1 || days > maxDays {
		return nil, refuse("A Share lasts 1 to %d days.", maxDays)
	}
	endAtFirst := options.Defaults.EndAtFirstDownload
	if req.EndAtFirstDownload != nil {
		endAtFirst = *req.EndAtFirstDownload
	}
	firstOnly := options.Defaults.FirstDownloaderOnly
	if req.FirstDownloaderOnly != nil {
		firstOnly = *req.FirstDownloaderOnly
	}
	if kind == TrackerOutside {
		firstOnly = false
	}

	inst, err := s.deps.Instances.Get(ctx, req.InstanceID)
	if err != nil || inst == nil {
		return nil, refuse("Instance %d does not exist.", req.InstanceID)
	}
	if err := s.admit(ctx, inst, hash, options, kind); err != nil {
		return nil, err
	}

	src, err := s.readTorrent(ctx, inst, hash)
	if err != nil {
		return nil, err
	}
	salt := rand.Text()
	helper, err := helpertorrent.Build(src.export, src.names, salt)
	if errors.Is(err, helpertorrent.ErrV2Unsupported) {
		return nil, refuse("v2 and hybrid torrents are unsupported")
	}
	if err != nil {
		return nil, refuse("qui cannot build the Share torrent: %v", err)
	}
	ownerSecret, recipientSecret := rand.Text(), rand.Text()
	ownerURL, recipientURL := base, base
	if kind == TrackerOwn {
		ownerURL, recipientURL = base+ownerSecret, base+recipientSecret
	}
	ownerTorrent, err := withAnnounce(helper.TorrentFile, ownerURL)
	if err != nil {
		return nil, err
	}
	recipientTorrent, err := withAnnounce(helper.TorrentFile, recipientURL)
	if err != nil {
		return nil, err
	}
	now := s.deps.Now().UTC()
	return &prepared{
		InstanceID:          inst.ID,
		Hash:                hash,
		Name:                src.torrent.Name,
		SavePath:            src.torrent.SavePath,
		Files:               src.names,
		Size:                src.size,
		HelperHash:          helper.InfoHash,
		Tracker:             kind,
		OwnerSecret:         ownerSecret,
		RecipientSecret:     recipientSecret,
		TorrentFile:         recipientTorrent,
		EndAtFirstDownload:  endAtFirst,
		FirstDownloaderOnly: firstOnly,
		ExpiresAt:           now.Add(time.Duration(days) * 24 * time.Hour),
		State:               StateActive,
		CreatedAt:           now,
		instanceName:        inst.Name,
		salt:                salt,
		layout:              helper.ContentLayout,
		ownerTorrent:        ownerTorrent,
	}, nil
}

func (s *Service) admit(ctx context.Context, inst *models.Instance, hash string, options Options, kind TrackerKind) error {
	if err := s.supported(ctx, inst); err != nil {
		return err
	}
	if s.deps.Helpers.IsHelper(inst.ID, hash) {
		return refuse("qui added this torrent for a Transfer or a Share.")
	}
	unfinished, err := s.deps.Store.Unfinished(ctx)
	if err != nil {
		return err
	}
	active := 0
	for _, other := range unfinished {
		if other.InstanceID == inst.ID && other.Hash == hash {
			return refuse("A Share of this torrent runs already.")
		}
		if other.State == StateActive {
			active++
		}
	}
	if active >= options.MaxActive {
		return refuse("Active Shares: %d of %d. Stop a Share first, or raise the limit in Settings → Shares.", active, options.MaxActive)
	}
	if kind != TrackerOwn {
		return nil
	}
	_, err = s.deps.Peers.PublicPeerAddress(ctx, inst.ID)
	if errors.Is(err, peeraddress.ErrNoAddress) {
		return refuse("qui does not know the public peer address of %s. Set it in the settings of the instance.", inst.Name)
	}
	if err != nil {
		return refuse("qui cannot read the public peer address of %s: %v", inst.Name, err)
	}
	return nil
}

func trackerOf(trackerSettings sharetracker.Settings, options Options) (TrackerKind, string, error) {
	if trackerSettings.Port != 0 && trackerSettings.PublicAddress != "" {
		return TrackerOwn, strings.TrimSuffix(trackerSettings.PublicAddress, "/") + "/announce/", nil
	}
	if options.TrackerURL != "" {
		return TrackerOutside, options.TrackerURL, nil
	}
	return "", "", refuse("Set a tracker in Settings → Shares: the port and the public address of qui's tracker, or a tracker URL.")
}

func (s *Service) supported(ctx context.Context, inst *models.Instance) error {
	client, err := s.deps.Sync.GetClient(ctx, inst.ID)
	if err != nil {
		return refuse("qui cannot reach qBittorrent on %s: %v", inst.Name, err)
	}
	if !client.Supports(qbittorrent.CapTransferAndShare) {
		return refuse("Share needs qBittorrent 5.1 or newer. Update qBittorrent on %s.", inst.Name)
	}
	return nil
}

type source struct {
	torrent qbt.Torrent
	names   []string
	size    int64
	export  []byte
}

func (s *Service) readTorrent(ctx context.Context, inst *models.Instance, hash string) (*source, error) {
	torrents, err := s.deps.Sync.GetTorrentRefsFresh(ctx, inst.ID, qbittorrent.TorrentRefFilter{Hashes: []string{hash}})
	if err != nil {
		return nil, fmt.Errorf("read the torrents of %s: %w", inst.Name, err)
	}
	if len(torrents) == 0 {
		return nil, refuse("%s has no torrent %s.", inst.Name, hash)
	}
	torrent := *torrents[0]
	files, err := s.deps.Sync.GetTorrentFiles(qbittorrent.WithForceFilesRefresh(ctx), inst.ID, hash)
	if err != nil {
		return nil, fmt.Errorf("read the files of the torrent: %w", err)
	}
	if files == nil {
		return nil, refuse("%s has no torrent %s.", inst.Name, hash)
	}
	list := slices.SortedFunc(slices.Values(*files), func(a, b qbt.TorrentFile) int { return a.Index - b.Index })
	if slices.ContainsFunc(list, func(f qbt.TorrentFile) bool { return f.Priority == 0 }) {
		return nil, refuse("Torrents with skipped files are unsupported")
	}
	if torrent.Progress < 1 || slices.ContainsFunc(list, func(f qbt.TorrentFile) bool { return f.Progress < 1 }) {
		return nil, refuse("Only a complete torrent can be shared, and %s is not complete.", torrent.Name)
	}
	src := &source{torrent: torrent}
	for _, f := range list {
		src.names = append(src.names, f.Name)
		src.size += f.Size
	}
	if src.export, _, _, err = s.deps.Sync.ExportTorrent(ctx, inst.ID, hash); err != nil {
		return nil, fmt.Errorf("export the torrent: %w", err)
	}
	return src, nil
}

func withAnnounce(torrent []byte, announce string) ([]byte, error) {
	var in struct {
		Info bencode.Bytes `bencode:"info"`
	}
	if err := bencode.Unmarshal(torrent, &in); err != nil {
		return nil, fmt.Errorf("decode the Share torrent: %w", err)
	}
	out, err := bencode.Marshal(struct {
		Announce string        `bencode:"announce"`
		Info     bencode.Bytes `bencode:"info"`
	}{announce, in.Info})
	if err != nil {
		return nil, fmt.Errorf("encode the Share torrent: %w", err)
	}
	return out, nil
}
