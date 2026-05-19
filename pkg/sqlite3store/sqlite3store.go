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

// WithCleanupInterval sets a custom cleanup interval. The cleanupInterval
// parameter controls how frequently expired session data is removed by the
// background cleanup goroutine. Setting it to 0 prevents the cleanup goroutine
// from running (i.e. expired sessions will not be removed).
func WithCleanupInterval(interval time.Duration) OptFunc {
	return func(s *SQLite3Store) {
		s.cleanupInterval = interval
	}
}

// SQLite3Store represents the session store.
// Despite the package name, queries are portable across SQLite and Postgres.
type SQLite3Store struct {
	db              dbinterface.Querier
	stopCleanup     chan bool
	cleanupInterval time.Duration
}

// New returns a new SQLite3Store instance, with a background cleanup goroutine
// that runs every 5 minutes to remove expired session data.
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

// Find returns the data for a given session token from the SQLite3Store instance.
// If the session token is not found or is expired, the returned exists flag will
// be set to false.
func (p *SQLite3Store) Find(token string) (b []byte, exists bool, err error) {
	return p.FindCtx(context.Background(), token)
}

// FindCtx returns the data for a given session token from the SQLite3Store instance.
// If the session token is not found or is expired, the returned exists flag will
// be set to false.
func (p *SQLite3Store) FindCtx(ctx context.Context, token string) (b []byte, exists bool, err error) {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

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

// Commit adds a session token and data to the SQLite3Store instance with the
// given expiry time. If the session token already exists, then the data and expiry
// time are updated.
func (p *SQLite3Store) Commit(token string, b []byte, expiry time.Time) error {
	return p.CommitCtx(context.Background(), token, b, expiry)
}

// CommitCtx adds a session token and data to the SQLite3Store instance with the
// given expiry time. If the session token already exists, then the data and expiry
// time are updated.
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

// Delete removes a session token and corresponding data from the SQLite3Store
// instance.
func (p *SQLite3Store) Delete(token string) error {
	return p.DeleteCtx(context.Background(), token)
}

// DeleteCtx removes a session token and corresponding data from the SQLite3Store
// instance.
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

// All returns a map containing the token and data for all active (i.e.
// not expired) sessions in the SQLite3Store instance.
func (p *SQLite3Store) All() (map[string][]byte, error) {
	return p.AllCtx(context.Background())
}

// AllCtx returns a map containing the token and data for all active (i.e.
// not expired) sessions in the SQLite3Store instance.
func (p *SQLite3Store) AllCtx(ctx context.Context) (map[string][]byte, error) {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

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

// StopCleanup terminates the background cleanup goroutine for the SQLite3Store
// instance. It's rare to terminate this; generally SQLite3Store instances and
// their cleanup goroutines are intended to be long-lived and run for the lifetime
// of your application.
//
// There may be occasions though when your use of the SQLite3Store is transient.
// An example is creating a new SQLite3Store instance in a test function. In this
// scenario, the cleanup goroutine (which will run forever) will prevent the
// SQLite3Store object from being garbage collected even after the test function
// has finished. You can prevent this by manually calling StopCleanup.
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
	defer tx.Rollback()

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
