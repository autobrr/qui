// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sqlite3store

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

type OptFunc func(*SQLite3Store)

type SQLite3Store struct {
	db              dbinterface.Querier
	stopCleanup     chan bool
	cleanupInterval time.Duration
}

func New(db dbinterface.Querier, opts ...OptFunc) *SQLite3Store {
	p := &SQLite3Store{
		db:              db,
		cleanupInterval: 5 * time.Minute,
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.cleanupInterval > 0 {
		p.stopCleanup = make(chan bool)
		go p.startCleanup(p.cleanupInterval)
	}

	return p
}

func (p *SQLite3Store) Find(token string) (b []byte, exists bool, err error) {
	return p.FindCtx(context.Background(), token)
}

func (p *SQLite3Store) FindCtx(ctx context.Context, token string) (b []byte, exists bool, err error) {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, "SELECT data FROM sessions WHERE token = ? AND expiry > ?", token, nowJulianDay())
	err = row.Scan(&b)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}

	if err = tx.Commit(); err != nil {
		return nil, false, err
	}

	return b, true, nil
}

func (p *SQLite3Store) Commit(token string, b []byte, expiry time.Time) error {
	return p.CommitCtx(context.Background(), token, b, expiry)
}

func (p *SQLite3Store) CommitCtx(ctx context.Context, token string, b []byte, expiry time.Time) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO sessions (token, data, expiry)
		VALUES (?, ?, ?)
		ON CONFLICT(token) DO UPDATE SET
			data = excluded.data,
			expiry = excluded.expiry
	`, token, b, toJulianDay(expiry.UTC()))
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (p *SQLite3Store) Delete(token string) error {
	return p.DeleteCtx(context.Background(), token)
}

func (p *SQLite3Store) DeleteCtx(ctx context.Context, token string) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE token = ?", token)
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (p *SQLite3Store) All() (map[string][]byte, error) {
	return p.AllCtx(context.Background())
}

func (p *SQLite3Store) AllCtx(ctx context.Context) (map[string][]byte, error) {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, "SELECT token, data FROM sessions WHERE expiry > ?", nowJulianDay())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make(map[string][]byte)

	for rows.Next() {
		var (
			token string
			data  []byte
		)

		err = rows.Scan(&token, &data)
		if err != nil {
			return nil, err
		}

		sessions[token] = data
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return sessions, nil
}

func (p *SQLite3Store) startCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for {
		select {
		case <-ticker.C:
			err := p.deleteExpired(context.Background())
			if err != nil {
				log.Println(err)
			}
		case <-p.stopCleanup:
			ticker.Stop()
			return
		}
	}
}

func (p *SQLite3Store) StopCleanup() {
	if p.stopCleanup != nil {
		p.stopCleanup <- true
	}
}

func (p *SQLite3Store) deleteExpired(ctx context.Context) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE expiry < ?", nowJulianDay())
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}

func nowJulianDay() float64 {
	return toJulianDay(time.Now().UTC())
}

func toJulianDay(t time.Time) float64 {
	return float64(t.UnixNano())/86400e9 + 2440587.5
}
