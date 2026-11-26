// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package share

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

var (
	ErrNotFound = errors.New("no such Share")
	ErrNotEnded = errors.New("the Share has not ended")
)

type Store struct {
	db dbinterface.Querier
}

func NewStore(db dbinterface.Querier) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, sh *Share) error {
	files, err := json.Marshal(sh.Files)
	if err != nil {
		return err
	}
	peers, err := json.Marshal(peersOrEmpty(sh.Peers))
	if err != nil {
		return err
	}
	err = s.db.QueryRowContext(ctx, `INSERT INTO shares (
			instance_id, hash, name, save_path, files_json, size, helper_hash, tracker, owner_secret, recipient_secret,
			torrent_file, end_at_first_download, first_downloader_only, holder, expires_at, state, end_reason,
			peers_json, downloaded, uploaded, created_at, ended_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id`,
		sh.InstanceID, sh.Hash, sh.Name, sh.SavePath, string(files), sh.Size, sh.HelperHash, string(sh.Tracker), sh.OwnerSecret, sh.RecipientSecret,
		sh.TorrentFile, sh.EndAtFirstDownload, sh.FirstDownloaderOnly, string(sh.Holder.AppendTo(nil)), sh.ExpiresAt.UTC(), string(sh.State), sh.EndReason,
		string(peers), sh.Downloaded, sh.Uploaded, sh.CreatedAt.UTC(), nullTime(sh.EndedAt),
	).Scan(&sh.ID)
	if err != nil {
		return fmt.Errorf("insert share: %w", err)
	}
	return nil
}

const columns = `id, instance_id, hash, name, save_path, files_json, size, helper_hash, tracker, owner_secret, recipient_secret,
	end_at_first_download, first_downloader_only, holder, expires_at, state, end_reason, peers_json, downloaded, uploaded, created_at, ended_at`

type scanner interface {
	Scan(dest ...any) error
}

func scanShare(row scanner, extra ...any) (*Share, error) {
	var (
		sh             Share
		tracker, state string
		files, peers   string
		holder         string
		endedAt        sql.NullTime
	)
	dest := append([]any{
		&sh.ID, &sh.InstanceID, &sh.Hash, &sh.Name, &sh.SavePath, &files, &sh.Size, &sh.HelperHash, &tracker, &sh.OwnerSecret, &sh.RecipientSecret,
		&sh.EndAtFirstDownload, &sh.FirstDownloaderOnly, &holder, &sh.ExpiresAt, &state, &sh.EndReason, &peers, &sh.Downloaded, &sh.Uploaded, &sh.CreatedAt, &endedAt,
	}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	sh.Tracker, sh.State = TrackerKind(tracker), State(state)
	sh.ExpiresAt, sh.CreatedAt, sh.EndedAt = sh.ExpiresAt.UTC(), sh.CreatedAt.UTC(), endedAt.Time.UTC()
	if !endedAt.Valid {
		sh.EndedAt = time.Time{}
	}
	if err := json.Unmarshal([]byte(files), &sh.Files); err != nil {
		return nil, fmt.Errorf("decode files of share %d: %w", sh.ID, err)
	}
	if err := json.Unmarshal([]byte(peers), &sh.Peers); err != nil {
		return nil, fmt.Errorf("decode peers of share %d: %w", sh.ID, err)
	}
	if err := sh.Holder.UnmarshalText([]byte(holder)); err != nil {
		return nil, fmt.Errorf("decode holder of share %d: %w", sh.ID, err)
	}
	if len(sh.Peers) == 0 {
		sh.Peers = nil
	}
	return &sh, nil
}

func (s *Store) Get(ctx context.Context, id int64) (*Share, error) {
	var torrent []byte
	sh, err := scanShare(s.db.QueryRowContext(ctx, `SELECT `+columns+`, torrent_file FROM shares WHERE id = ?`, id), &torrent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %d", ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("read share %d: %w", id, err)
	}
	sh.TorrentFile = torrent
	return sh, nil
}

func (s *Store) List(ctx context.Context) ([]Share, error) {
	return s.query(ctx, `SELECT `+columns+` FROM shares ORDER BY id DESC`)
}

func (s *Store) Unfinished(ctx context.Context) ([]Share, error) {
	return s.query(ctx, `SELECT `+columns+` FROM shares WHERE state <> ? ORDER BY id`, string(StateEnded))
}

func (s *Store) query(ctx context.Context, query string, args ...any) ([]Share, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read shares: %w", err)
	}
	defer rows.Close()
	var list []Share
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, fmt.Errorf("read shares: %w", err)
		}
		list = append(list, *sh)
	}
	return list, rows.Err()
}

func (s *Store) SetState(ctx context.Context, id int64, state State, reason string, endedAt time.Time) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE shares SET state = ?, end_reason = ?, ended_at = ? WHERE id = ?`,
		string(state), reason, nullTime(endedAt), id); err != nil {
		return fmt.Errorf("save state of share %d: %w", id, err)
	}
	return nil
}

func (s *Store) SetHolder(ctx context.Context, id int64, holder netip.Addr) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE shares SET holder = ? WHERE id = ?`, string(holder.AppendTo(nil)), id); err != nil {
		return fmt.Errorf("save holder of share %d: %w", id, err)
	}
	return nil
}

func (s *Store) SaveActivity(ctx context.Context, id int64, a Activity) error {
	peers, err := json.Marshal(peersOrEmpty(a.Peers))
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE shares SET peers_json = ?, downloaded = ?, uploaded = ? WHERE id = ?`,
		string(peers), a.Downloaded, a.Uploaded, id); err != nil {
		return fmt.Errorf("save activity of share %d: %w", id, err)
	}
	return nil
}

func (s *Store) Remove(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM shares WHERE id = ? AND state = ?`, id, string(StateEnded))
	if err != nil {
		return fmt.Errorf("remove share %d: %w", id, err)
	}
	if n, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("remove share %d: %w", id, err)
	} else if n == 0 {
		if _, err := s.Get(ctx, id); err != nil {
			return err
		}
		return fmt.Errorf("%w: %d", ErrNotEnded, id)
	}
	return nil
}

func (s *Store) EndedBefore(ctx context.Context, t time.Time) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM shares WHERE state = ? AND ended_at < ? ORDER BY id`, string(StateEnded), t.UTC())
	if err != nil {
		return nil, fmt.Errorf("read ended shares: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read ended shares: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func peersOrEmpty(peers []Peer) []Peer {
	if peers == nil {
		return []Peer{}
	}
	return peers
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
