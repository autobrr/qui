// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

var (
	// ErrClaimed is the refusal of a torrent that a running Transfer holds.
	ErrClaimed = errors.New("a Transfer holds this torrent")
	// ErrHelper is the refusal of a helper torrent: its files are the files
	// of a real torrent.
	ErrHelper = errors.New("qui added this torrent for a Transfer or a Share")
	// ErrHelperOwned is a second registration of a helper torrent by another owner.
	ErrHelperOwned = errors.New("another Transfer or Share owns this helper torrent")
)

type torrentKey struct {
	instanceID int
	hash       string
}

// Store keeps Transfers and helper torrents in the database. It also keeps
// the claimed torrents and the helper torrents in memory, so that a check of
// one torrent reads no database.
type Store struct {
	db dbinterface.Querier

	mu      sync.RWMutex
	claims  map[torrentKey]int64
	helpers map[torrentKey]OwnerKind
}

// NewStore reads the claims and the helper torrents of db.
func NewStore(ctx context.Context, db dbinterface.Querier) (*Store, error) {
	s := &Store{db: db, claims: make(map[torrentKey]int64), helpers: make(map[torrentKey]OwnerKind)}
	if err := s.load(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT instance_id, hash, transfer_id FROM torrent_transfer_claims`)
	if err != nil {
		return fmt.Errorf("read transfer claims: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key torrentKey
		var id int64
		if err := rows.Scan(&key.instanceID, &key.hash, &id); err != nil {
			return fmt.Errorf("read transfer claims: %w", err)
		}
		s.claims[key] = id
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read transfer claims: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT instance_id, hash, owner_kind FROM helper_torrents`)
	if err != nil {
		return fmt.Errorf("read helper torrents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key torrentKey
		var owner string
		if err := rows.Scan(&key.instanceID, &key.hash, &owner); err != nil {
			return fmt.Errorf("read helper torrents: %w", err)
		}
		s.helpers[key] = OwnerKind(owner)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read helper torrents: %w", err)
	}
	return nil
}

// Create stores record and sets its ID. In the same write it claims each
// member on the Transfer source. When another Transfer holds a member, it
// stores nothing and returns ErrClaimed.
func (s *Store) Create(ctx context.Context, record *Record) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transfer: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	id, err := insertRecord(ctx, tx, record)
	if err != nil {
		return err
	}
	claimed := make([]torrentKey, 0, len(record.Members))
	for i := range record.Members {
		member := &record.Members[i]
		member.Hash = strings.ToLower(member.Hash)
		if err := insertMember(ctx, tx, id, i, member); err != nil {
			return err
		}
		key := torrentKey{record.SourceInstanceID, member.Hash}
		result, err := tx.ExecContext(ctx, `INSERT INTO torrent_transfer_claims (instance_id, hash, transfer_id)
			VALUES (?, ?, ?) ON CONFLICT (instance_id, hash) DO NOTHING`, key.instanceID, key.hash, id)
		if err != nil {
			return fmt.Errorf("claim torrent: %w", err)
		}
		if n, err := result.RowsAffected(); err != nil {
			return fmt.Errorf("claim torrent: %w", err)
		} else if n == 0 {
			return fmt.Errorf("%w: %s on instance %d", ErrClaimed, member.Hash, key.instanceID)
		}
		claimed = append(claimed, key)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transfer: %w", err)
	}

	record.ID = id
	s.mu.Lock()
	for _, key := range claimed {
		s.claims[key] = id
	}
	s.mu.Unlock()
	return nil
}

func insertRecord(ctx context.Context, tx dbinterface.TxQuerier, r *Record) (int64, error) {
	source, err := json.Marshal(endpointJSONOf(r.SourceEndpoint))
	if err != nil {
		return 0, err
	}
	target, err := json.Marshal(endpointJSONOf(r.TargetEndpoint))
	if err != nil {
		return 0, err
	}
	options, err := json.Marshal(r.Options)
	if err != nil {
		return 0, err
	}
	steps, err := json.Marshal(r.Steps)
	if err != nil {
		return 0, err
	}
	logLines, err := json.Marshal(r.Log)
	if err != nil {
		return 0, err
	}

	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO torrent_transfers (
			kind, way, state, started_by, rule_id, source_instance_id, target_instance_id,
			source_endpoint_json, target_endpoint_json, options_json, steps_json, log_json,
			flush_state, reserved_bytes, retry_at, failure_class, failure_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id`,
		string(r.Kind), string(r.Way), string(r.State), string(r.StartedBy), r.RuleID, r.SourceInstanceID, r.TargetInstanceID,
		string(source), string(target), string(options), string(steps), string(logLines),
		string(r.FlushState), r.ReservedBytes, nullTime(r.RetryAt), string(r.FailureClass), r.FailureReason,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert transfer: %w", err)
	}
	return id, nil
}

func insertMember(ctx context.Context, tx dbinterface.TxQuerier, transferID int64, position int, m *Member) error {
	placement, err := json.Marshal(placementJSON{Category: m.Placement.Category, Tags: m.Placement.Tags, ContentLayout: m.Placement.ContentLayout})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO torrent_transfer_members (
			transfer_id, position, hash, name, source_save_path, target_instance_id, target_save_path, placement_json, proven_pieces
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		transferID, position, m.Hash, m.Name, m.SourceSavePath, m.TargetInstanceID, m.Placement.SavePath, string(placement), m.ProvenPieces,
	); err != nil {
		return fmt.Errorf("insert transfer member: %w", err)
	}
	return nil
}

// Get returns the Transfer with id and its members.
func (s *Store) Get(ctx context.Context, id int64) (*Record, error) {
	var (
		r                                        Record
		kind, way, state, startedBy              string
		ruleID                                   sql.NullInt64
		source, target, options, steps, logLines string
		flushState, failureClass                 string
		retryAt, finishedAt                      sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, `SELECT id, kind, way, state, started_by, rule_id, source_instance_id, target_instance_id,
			source_endpoint_json, target_endpoint_json, options_json, steps_json, log_json,
			flush_state, reserved_bytes, retry_at, failure_class, failure_reason, created_at, finished_at
		FROM torrent_transfers WHERE id = ?`, id).Scan(
		&r.ID, &kind, &way, &state, &startedBy, &ruleID, &r.SourceInstanceID, &r.TargetInstanceID,
		&source, &target, &options, &steps, &logLines,
		&flushState, &r.ReservedBytes, &retryAt, &failureClass, &r.FailureReason, &r.CreatedAt, &finishedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("read transfer %d: %w", id, err)
	}
	r.Kind, r.Way, r.State, r.StartedBy = Kind(kind), Way(way), State(state), StartedBy(startedBy)
	r.FlushState, r.FailureClass = FlushState(flushState), FailureClass(failureClass)
	if ruleID.Valid {
		r.RuleID = new(int(ruleID.Int64))
	}
	r.RetryAt, r.FinishedAt = retryAt.Time, finishedAt.Time
	var sourceEndpoint, targetEndpoint endpointJSON
	for _, field := range []struct {
		raw  string
		into any
	}{{source, &sourceEndpoint}, {target, &targetEndpoint}, {options, &r.Options}, {steps, &r.Steps}, {logLines, &r.Log}} {
		if err := json.Unmarshal([]byte(field.raw), field.into); err != nil {
			return nil, fmt.Errorf("decode transfer %d: %w", id, err)
		}
	}
	r.SourceEndpoint, r.TargetEndpoint = sourceEndpoint.endpoint(), targetEndpoint.endpoint()

	members, err := s.members(ctx, id)
	if err != nil {
		return nil, err
	}
	r.Members = members
	return &r, nil
}

func (s *Store) members(ctx context.Context, transferID int64) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT hash, name, source_save_path, target_instance_id, target_save_path, placement_json, proven_pieces
		FROM torrent_transfer_members WHERE transfer_id = ? ORDER BY position`, transferID)
	if err != nil {
		return nil, fmt.Errorf("read transfer members: %w", err)
	}
	defer rows.Close()
	var members []Member
	for rows.Next() {
		var (
			m         Member
			placement string
			stored    placementJSON
		)
		if err := rows.Scan(&m.Hash, &m.Name, &m.SourceSavePath, &m.TargetInstanceID, &m.Placement.SavePath, &placement, &m.ProvenPieces); err != nil {
			return nil, fmt.Errorf("read transfer members: %w", err)
		}
		if err := json.Unmarshal([]byte(placement), &stored); err != nil {
			return nil, fmt.Errorf("decode transfer member placement: %w", err)
		}
		m.Placement.Category, m.Placement.Tags, m.Placement.ContentLayout = stored.Category, stored.Tags, stored.ContentLayout
		members = append(members, m)
	}
	return members, rows.Err()
}

// Save writes what changes while a Transfer runs: the state, the steps, the
// log, the flush state, the reserved bytes, the retry, and the failure.
func (s *Store) Save(ctx context.Context, r *Record) error {
	steps, err := json.Marshal(r.Steps)
	if err != nil {
		return err
	}
	logLines, err := json.Marshal(r.Log)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE torrent_transfers SET state = ?, steps_json = ?, log_json = ?, flush_state = ?,
			reserved_bytes = ?, retry_at = ?, failure_class = ?, failure_reason = ? WHERE id = ?`,
		string(r.State), string(steps), string(logLines), string(r.FlushState),
		r.ReservedBytes, nullTime(r.RetryAt), string(r.FailureClass), r.FailureReason, r.ID); err != nil {
		return fmt.Errorf("save transfer %d: %w", r.ID, err)
	}
	return nil
}

// SaveProven writes the Proven pieces of the member at position.
func (s *Store) SaveProven(ctx context.Context, id int64, position int, proven []byte) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE torrent_transfer_members SET proven_pieces = ? WHERE transfer_id = ? AND position = ?`,
		proven, id, position); err != nil {
		return fmt.Errorf("save proven pieces of transfer %d: %w", id, err)
	}
	return nil
}

// Finish ends the Transfer in state, and releases its torrents and folders.
func (s *Store) Finish(ctx context.Context, id int64, state State) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin finish of transfer %d: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE torrent_transfers SET state = ?, finished_at = ? WHERE id = ?`,
		string(state), time.Now(), id); err != nil {
		return fmt.Errorf("finish transfer %d: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM torrent_transfer_claims WHERE transfer_id = ?`, id); err != nil {
		return fmt.Errorf("release torrents of transfer %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit finish of transfer %d: %w", id, err)
	}

	s.mu.Lock()
	for key, claimant := range s.claims {
		if claimant == id {
			delete(s.claims, key)
		}
	}
	s.mu.Unlock()
	return nil
}

// RunningFolders returns the save path of each member of each unfinished
// Transfer that reads or writes on the instance: the save path on the source
// and the save path of its placement on the target.
func (s *Store) RunningFolders(ctx context.Context, instanceID int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.source_save_path FROM torrent_transfer_members m
			JOIN torrent_transfers t ON t.id = m.transfer_id
			WHERE t.finished_at IS NULL AND t.source_instance_id = ?
		UNION
		SELECT m.target_save_path FROM torrent_transfer_members m
			JOIN torrent_transfers t ON t.id = m.transfer_id
			WHERE t.finished_at IS NULL AND m.target_instance_id = ?`, instanceID, instanceID)
	if err != nil {
		return nil, fmt.Errorf("read folders of running transfers: %w", err)
	}
	defer rows.Close()
	var folders []string
	for rows.Next() {
		var folder string
		if err := rows.Scan(&folder); err != nil {
			return nil, fmt.Errorf("read folders of running transfers: %w", err)
		}
		if folder != "" {
			folders = append(folders, folder)
		}
	}
	return folders, rows.Err()
}

// RegisterHelper writes the row of a helper torrent. Call it before the add
// of the helper: from the write on, every protection of qui knows the
// torrent as a helper. A second call for the same owner does nothing, and a
// call for another owner returns ErrHelperOwned.
func (s *Store) RegisterHelper(ctx context.Context, h Helper) error {
	hash := strings.ToLower(h.Hash)
	result, err := s.db.ExecContext(ctx, `INSERT INTO helper_torrents (instance_id, hash, random, owner_kind, owner_id)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT (instance_id, hash) DO NOTHING`,
		h.InstanceID, hash, h.Random, string(h.Owner), h.OwnerID)
	if err != nil {
		return fmt.Errorf("register helper torrent: %w", err)
	}
	if n, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("register helper torrent: %w", err)
	} else if n == 0 {
		var owner string
		var ownerID int64
		if err := s.db.QueryRowContext(ctx, `SELECT owner_kind, owner_id FROM helper_torrents WHERE instance_id = ? AND hash = ?`,
			h.InstanceID, hash).Scan(&owner, &ownerID); err != nil {
			return fmt.Errorf("read registered helper torrent: %w", err)
		}
		if OwnerKind(owner) != h.Owner || ownerID != h.OwnerID {
			return fmt.Errorf("%w: %s on instance %d", ErrHelperOwned, hash, h.InstanceID)
		}
	}

	s.mu.Lock()
	s.helpers[torrentKey{h.InstanceID, hash}] = h.Owner
	s.mu.Unlock()
	return nil
}

// HelpersOf returns the helper torrents of one Transfer or Share.
func (s *Store) HelpersOf(ctx context.Context, owner OwnerKind, ownerID int64) ([]Helper, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT instance_id, hash, random, owner_kind, owner_id, created_at
		FROM helper_torrents WHERE owner_kind = ? AND owner_id = ? ORDER BY instance_id, hash`, string(owner), ownerID)
	if err != nil {
		return nil, fmt.Errorf("read helper torrents: %w", err)
	}
	defer rows.Close()
	var helpers []Helper
	for rows.Next() {
		var h Helper
		var kind string
		if err := rows.Scan(&h.InstanceID, &h.Hash, &h.Random, &kind, &h.OwnerID, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("read helper torrents: %w", err)
		}
		h.Owner = OwnerKind(kind)
		helpers = append(helpers, h)
	}
	return helpers, rows.Err()
}

// RemoveHelpers deletes the rows of the helper torrents of one Transfer or
// Share. Call it only when the helper torrents are gone from the clients:
// from then on, qui protects them no more.
func (s *Store) RemoveHelpers(ctx context.Context, owner OwnerKind, ownerID int64) error {
	helpers, err := s.HelpersOf(ctx, owner, ownerID)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM helper_torrents WHERE owner_kind = ? AND owner_id = ?`, string(owner), ownerID); err != nil {
		return fmt.Errorf("remove helper torrents: %w", err)
	}
	s.mu.Lock()
	for _, h := range helpers {
		delete(s.helpers, torrentKey{h.InstanceID, strings.ToLower(h.Hash)})
	}
	s.mu.Unlock()
	return nil
}

// IsClaimed reports whether a running Transfer holds the torrent.
func (s *Store) IsClaimed(instanceID int, hash string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.claims) == 0 {
		return false
	}
	_, ok := s.claims[torrentKey{instanceID, strings.ToLower(hash)}]
	return ok
}

// IsHelper reports whether qui added the torrent as a helper torrent. Only
// the registry answers: the tag of a helper torrent proves nothing.
func (s *Store) IsHelper(instanceID int, hash string) bool {
	return s.HelperKind(instanceID, hash) != ""
}

// HelperKind returns the kind of the owner of a helper torrent, transfer or
// share, and "" for any other torrent.
func (s *Store) HelperKind(instanceID int, hash string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.helpers) == 0 {
		return ""
	}
	return string(s.helpers[torrentKey{instanceID, strings.ToLower(hash)}])
}

// Hold returns why qui refuses a delete, a move, or a rename of the torrent:
// ErrClaimed or ErrHelper. It returns nil for any other torrent.
func (s *Store) Hold(instanceID int, hash string) error {
	if s.IsClaimed(instanceID, hash) {
		return ErrClaimed
	}
	if s.IsHelper(instanceID, hash) {
		return ErrHelper
	}
	return nil
}

type endpointJSON struct {
	URL    string `json:"url"`
	Mode   string `json:"mode"`
	Walked string `json:"walked"`
}

func endpointJSONOf(e fsops.Endpoint) endpointJSON {
	return endpointJSON{URL: e.URL, Mode: string(e.Mode), Walked: e.Walked}
}

func (e endpointJSON) endpoint() fsops.Endpoint {
	return fsops.Endpoint{URL: e.URL, Mode: models.FilesystemMode(e.Mode), Walked: e.Walked}
}

type placementJSON struct {
	Category      string   `json:"category"`
	Tags          []string `json:"tags"`
	ContentLayout string   `json:"contentLayout"`
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
