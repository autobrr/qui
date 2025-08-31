package sqlite3store

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"
)

type SqlDB interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
}

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
type SQLite3Store struct {
	db              SqlDB
	stopCleanup     chan bool
	cleanupInterval time.Duration
}

// New returns a new SQLite3Store instance, with a background cleanup goroutine
// that runs every 5 minutes to remove expired session data.
func New(db SqlDB, opts ...OptFunc) *SQLite3Store {
	p := &SQLite3Store{
		db:              db,
		cleanupInterval: 5 * time.Minute,
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.cleanupInterval > 0 {
		p.stopCleanup = make(chan bool)
		go p.startCleanup()
	}

	return p
}

// Find returns the data for a session token from the SQLite3 database. If the
// session token is not found or is expired, the found return value will be false.
func (p *SQLite3Store) Find(token string) (b []byte, found bool, err error) {
	return p.FindCtx(context.Background(), token)
}

// FindCtx returns the data for a session token from the SQLite3 database. If the
// session token is not found or is expired, the found return value will be false.
func (p *SQLite3Store) FindCtx(ctx context.Context, token string) (b []byte, found bool, err error) {
	row := p.db.QueryRowContext(ctx, "SELECT data FROM sessions WHERE token = ? AND expiry > ?", token, time.Now().Unix())
	err = row.Scan(&b)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return b, true, nil
}

// Commit adds a session token and data to the SQLite3 database with the given expiry
// time. If the session token already exists, then the data and expiry time are updated.
func (p *SQLite3Store) Commit(token string, b []byte, expiry time.Time) error {
	return p.CommitCtx(context.Background(), token, b, expiry)
}

// CommitCtx adds a session token and data to the SQLite3 database with the given
// expiry time. If the session token already exists, then the data and expiry time are updated.
func (p *SQLite3Store) CommitCtx(ctx context.Context, token string, b []byte, expiry time.Time) error {
	_, err := p.db.ExecContext(ctx, "INSERT OR REPLACE INTO sessions (token, data, expiry) VALUES (?, ?, ?)", token, b, expiry.Unix())
	return err
}

// Delete removes a session token and corresponding data from the SQLite3 database.
func (p *SQLite3Store) Delete(token string) error {
	return p.DeleteCtx(context.Background(), token)
}

// DeleteCtx removes a session token and corresponding data from the SQLite3 database.
func (p *SQLite3Store) DeleteCtx(ctx context.Context, token string) error {
	_, err := p.db.ExecContext(ctx, "DELETE FROM sessions WHERE token = ?", token)
	return err
}

// All returns a map containing data for all active sessions. The map key is the
// session token and the map value is the session data.
func (p *SQLite3Store) All() (map[string][]byte, error) {
	return p.AllCtx(context.Background())
}

// AllCtx returns a map containing data for all active sessions. The map key is the
// session token and the map value is the session data.
func (p *SQLite3Store) AllCtx(ctx context.Context) (map[string][]byte, error) {
	rows, err := p.db.QueryContext(ctx, "SELECT token, data FROM sessions WHERE expiry > ?", time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make(map[string][]byte)

	for rows.Next() {
		var token string
		var data []byte

		err := rows.Scan(&token, &data)
		if err != nil {
			return nil, err
		}

		sessions[token] = data
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return sessions, nil
}

// StopCleanup terminates the background cleanup goroutine for the SQLite3Store
// instance. StopCleanup is intended to be called before shutting down your
// application.
func (p *SQLite3Store) StopCleanup() {
	if p.stopCleanup != nil {
		p.stopCleanup <- true
	}
}

// startCleanup runs a background goroutine to delete expired session data.
func (p *SQLite3Store) startCleanup() {
	ticker := time.NewTicker(p.cleanupInterval)
	for {
		select {
		case <-ticker.C:
			err := p.deleteExpired()
			if err != nil {
				log.Printf("sqlite3store: unable to delete expired session data: %v", err)
			}
		case <-p.stopCleanup:
			ticker.Stop()
			return
		}
	}
}

// deleteExpired deletes expired session data from the SQLite3 database.
func (p *SQLite3Store) deleteExpired() error {
	_, err := p.db.ExecContext(context.Background(), "DELETE FROM sessions WHERE expiry <= ?", time.Now().Unix())
	return err
}
