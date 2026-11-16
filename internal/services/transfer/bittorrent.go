// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/helpertorrent"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/torrentadd"
	"github.com/autobrr/qui/internal/services/peeraddress"
)

const (
	// stepPass comes before qui adds the Transfer torrent of a member, with
	// its hash, and stepPassed once the target has every piece, with the
	// bytes that the target downloaded.
	stepPass     = "pass by BitTorrent"
	stepPassed   = "passed by BitTorrent"
	stepFallBack = "fall back to SFTP"
)

type noConnection struct{ reason string }

func (e *noConnection) Error() string { return e.reason }

func (j *job) passByBitTorrent(ctx context.Context) error {
	for _, p := range j.parts {
		if p.headStartProven {
			continue
		}
		err := j.pass(ctx, p)
		if why, ok := errors.AsType[*noConnection](err); ok {
			return j.fallBack(ctx, why)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (j *job) fallBack(ctx context.Context, why *noConnection) error {
	if j.rec.Options.ForcedWay == WayBitTorrent || j.srcFiles == nil || j.blind {
		return temporary("%s", why.reason)
	}
	if err := j.removeHelpers(ctx); err != nil {
		return temporary("qui cannot remove its helper torrents: %v", err)
	}
	j.logf("%s qui copies the files over SFTP instead.", why.reason)
	j.rec.Way = WaySFTP
	if err := j.step(ctx, StateCopying, stepFallBack); err != nil {
		return err
	}
	if j.made == nil {
		j.made = map[*groupFile]int64{}
	}
	for _, f := range j.files {
		if _, made := j.made[f]; made || f.onTarget {
			continue
		}
		size, made, err := j.ownTemp(ctx, f)
		if err != nil {
			return err
		}
		if made {
			j.made[f] = size
		}
	}
	return nil
}

func (j *job) pass(ctx context.Context, p *part) error {
	if !j.blind {
		defer j.unlink(context.WithoutCancel(ctx), p)
		if err := j.link(ctx, p); err != nil {
			return err
		}
	}
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	random := hex.EncodeToString(salt)
	var targetNames []string
	for _, f := range p.plan {
		switch {
		case f.pad:
		case j.blind:
			targetNames = append(targetNames, f.name)
		default:
			targetNames = append(targetNames, j.checkName(f))
		}
	}
	helper, err := helpertorrent.Build(p.src.export, targetNames, random)
	if err != nil {
		return permanent("qui cannot build the Transfer torrent: %v", err)
	}
	if err := j.stepWith(ctx, StateCopying, stepPass, helper.InfoHash); err != nil {
		return err
	}
	if err := j.addHelper(ctx, j.target, helper, random, torrentadd.Settings{
		SavePath: p.Placement.SavePath,
		Stopped:  new(false),
		Forced:   true,
	}); err != nil {
		return err
	}
	torrent, checked, err := j.await(ctx, j.target, helper.InfoHash, j.svc.deps.Config.CheckPolls, func(t *qbt.Torrent) bool { return !isChecking(t.State) })
	switch {
	case err != nil:
		return err
	case !checked:
		return temporary("The check of the Transfer torrent on %s did not end.", j.target.Name)
	}
	seeded := torrent.Progress < 1
	if seeded {
		if err := j.seedFromSource(ctx, p, helper, random, targetNames); err != nil {
			return err
		}
		if err := j.connect(ctx, helper.InfoHash); err != nil {
			return err
		}
		if torrent, err = j.download(ctx, helper.InfoHash); err != nil {
			return err
		}
	}
	for _, inst := range []*models.Instance{j.target, j.source} {
		if inst == j.source && !seeded {
			continue
		}
		if err := j.svc.deps.Sync.DeleteTorrents(ctx, inst.ID, []string{helper.InfoHash}, false, j.actor()); err != nil {
			return temporary("qui cannot remove the Transfer torrent from %s: %v", inst.Name, err)
		}
	}
	// The fresh read of the source also drops the Transfer torrent from
	// what qui knows of it.
	if err := j.checkSource(ctx); err != nil {
		return err
	}
	p.passed = true
	return j.stepWith(ctx, StateCopying, stepPassed, strconv.FormatInt(torrent.Downloaded, 10))
}

func (j *job) addHelper(ctx context.Context, inst *models.Instance, helper helpertorrent.Helper, random string, settings torrentadd.Settings) error {
	if err := j.svc.deps.Store.RegisterHelper(ctx, Helper{
		InstanceID: inst.ID, Hash: helper.InfoHash, Random: []byte(random), Owner: OwnerTransfer, OwnerID: j.rec.ID,
	}); err != nil {
		return err
	}
	settings.ContentLayout = qbt.ContentLayout(helper.ContentLayout)
	settings.AutoTMM, settings.UseDownloadPath = new(false), new(false)
	res := j.svc.adder.Add(ctx, torrentadd.Request{
		InstanceID:   inst.ID,
		Torrent:      helper.TorrentFile,
		Hash:         helper.InfoHash,
		Settings:     settings,
		Polls:        1,
		PollInterval: j.svc.deps.Config.PollInterval,
	})
	if res.Failure == torrentadd.AddRefused {
		return temporary("%s did not take the Transfer torrent: %s", inst.Name, describe(res))
	}
	return nil
}

// seedFromSource renames the files of the stopped Transfer torrent to the
// files of the source: a rename of a missing file changes only its name.
func (j *job) seedFromSource(ctx context.Context, p *part, helper helpertorrent.Helper, random string, targetNames []string) error {
	if err := j.addHelper(ctx, j.source, helper, random, torrentadd.Settings{
		SavePath: p.src.torrent.SavePath,
		Stopped:  new(true),
		SeedMode: true,
	}); err != nil {
		return err
	}
	ctx = qbittorrent.WithPostAddBulkActionRetry(ctx)
	sourceNames := names(p.plan)
	for i, name := range targetNames {
		if name == sourceNames[i] {
			continue
		}
		if err := j.svc.deps.Sync.RenameTorrentFile(ctx, j.source.ID, helper.InfoHash, name, sourceNames[i]); err != nil {
			return temporary("qui cannot rename %s in the Transfer torrent on %s: %v", sourceNames[i], j.source.Name, err)
		}
	}
	if err := j.svc.deps.Sync.SetForceStart(ctx, j.source.ID, []string{helper.InfoHash}, true); err != nil {
		return temporary("qui cannot start the Transfer torrent on %s: %v", j.source.Name, err)
	}
	_, seeds, err := j.await(ctx, j.source, helper.InfoHash, j.svc.deps.Config.Polls, func(t *qbt.Torrent) bool { return t.Progress >= 1 })
	switch {
	case err != nil:
		return err
	case !seeds:
		return temporary("The Transfer torrent on %s did not start to seed.", j.source.Name)
	}
	return nil
}

func (j *job) connect(ctx context.Context, hash string) error {
	var why []string
	for i, dir := range []struct{ from, to *models.Instance }{{j.target, j.source}, {j.source, j.target}} {
		if i > 0 {
			j.logf("No byte arrived on %s from %s. qui asks %s to connect to %s.", j.target.Name, j.source.Name, j.source.Name, j.target.Name)
		}
		address, err := j.svc.deps.Peers.PublicPeerAddress(ctx, dir.to.ID)
		switch {
		case errors.Is(err, peeraddress.ErrNoAddress):
			why = append(why, dir.to.Name+" has no public peer address")
			continue
		case err != nil:
			why = append(why, fmt.Sprintf("qui cannot read the public peer address of %s: %v", dir.to.Name, err))
			continue
		}
		if err := j.svc.deps.Sync.AddPeersToTorrents(qbittorrent.WithPostAddBulkActionRetry(ctx), dir.from.ID, []string{hash}, []string{address}); err != nil {
			why = append(why, fmt.Sprintf("%s did not take the address %s of %s: %v", dir.from.Name, address, dir.to.Name, err))
			continue
		}
		_, arrived, err := j.await(ctx, j.target, hash, j.svc.deps.Config.PeerPolls, func(t *qbt.Torrent) bool { return t.Downloaded > 0 || t.Progress >= 1 })
		if err != nil {
			return err
		}
		if arrived {
			return nil
		}
		why = append(why, fmt.Sprintf("%s did not reach %s at %s", dir.from.Name, dir.to.Name, address))
	}
	return &noConnection{reason: "The clients did not connect: " + strings.Join(why, ", and ") + "."}
}

func (j *job) download(ctx context.Context, hash string) (*qbt.Torrent, error) {
	cfg := j.svc.deps.Config
	last, idle := int64(-1), 0
	for poll := 0; ; poll++ {
		if err := j.pause(ctx, poll); err != nil {
			return nil, err
		}
		torrent, err := j.helperOn(ctx, j.target, hash)
		switch {
		case err != nil:
			return nil, err
		case torrent == nil:
			return nil, temporary("The Transfer torrent left %s.", j.target.Name)
		case torrent.Progress >= 1 && !isChecking(torrent.State):
			return torrent, nil
		case torrent.Downloaded > last:
			last, idle = torrent.Downloaded, 0
		default:
			if idle++; idle >= cfg.PeerPolls {
				return nil, temporary("The Transfer torrent on %s got no byte from %s for a while, at %.0f%%.", j.target.Name, j.source.Name, torrent.Progress*100)
			}
		}
	}
}

func (j *job) await(ctx context.Context, inst *models.Instance, hash string, polls int, done func(*qbt.Torrent) bool) (*qbt.Torrent, bool, error) {
	var torrent *qbt.Torrent
	for poll := range polls {
		if err := j.pause(ctx, poll); err != nil {
			return nil, false, err
		}
		var err error
		if torrent, err = j.helperOn(ctx, inst, hash); err != nil {
			return nil, false, err
		}
		if torrent != nil && done(torrent) {
			return torrent, true, nil
		}
	}
	return torrent, false, nil
}

func (j *job) helperOn(ctx context.Context, inst *models.Instance, hash string) (*qbt.Torrent, error) {
	torrent, err := j.svc.torrent(ctx, inst.ID, hash)
	switch {
	case err != nil:
		return nil, temporary("qui cannot read the Transfer torrent on %s: %v", inst.Name, err)
	case torrent == nil:
		return nil, nil
	case torrent.State == qbt.TorrentStateMissingFiles || torrent.State == qbt.TorrentStateError:
		return nil, temporary("The Transfer torrent on %s does not find its files: qBittorrent shows %s.", inst.Name, torrent.State)
	}
	return torrent, nil
}

func (j *job) pause(ctx context.Context, poll int) error {
	if poll == 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(j.svc.deps.Config.PollInterval):
		return nil
	}
}

func (j *job) downloaded() int64 {
	var total int64
	for _, step := range j.rec.Steps {
		if step.Name == stepPassed {
			n, _ := strconv.ParseInt(step.Detail, 10, 64)
			total += n
		}
	}
	return total
}
