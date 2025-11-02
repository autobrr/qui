// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tmaxmax/go-sse"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"

	_ "modernc.org/sqlite"
)

func TestStreamManagerHandleSyncErrorPublishesErrorEvent(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	sub := &subscriptionState{
		id:      "subscription-1",
		options: StreamOptions{InstanceID: 42},
		created: time.Now(),
	}

	manager.subscriptions[sub.id] = sub
	manager.instanceIndex[sub.options.InstanceID] = map[string]*subscriptionState{
		sub.id: sub,
	}

	manager.HandleSyncError(sub.options.InstanceID, errors.New("sync failed"))

	messages := provider.messagesFor(sub.id)
	require.Len(t, messages, 1, "expected a single broadcast message")

	payload := decodeStreamPayload(t, messages[0])
	require.Equal(t, streamEventError, payload.Type)
	require.Equal(t, sub.options.InstanceID, payload.Meta.InstanceID)
	require.Greater(t, payload.Meta.RetryInSeconds, 0, "expected retry interval to be populated")
	require.Contains(t, payload.Err, "sync failed")
}

func TestStreamManagerHandleSyncErrorWithoutSubscribers(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	manager.HandleSyncError(7, errors.New("boom"))

	require.Empty(t, provider.allMessages(), "no subscribers should result in no messages")
}

func TestStreamManagerServeInstanceNotFound(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	manager := NewStreamManager(nil, nil, store)

	payload := []map[string]any{
		{
			"key":        "stream-99",
			"instanceId": 99,
			"page":       0,
			"limit":      50,
			"sort":       "addedOn",
			"order":      "desc",
			"search":     "",
			"filters":    nil,
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)

	manager.Serve(recorder, request)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestStreamManagerServeInstanceValidationError(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	ctx := context.Background()
	_, err := store.Create(ctx, "Test Instance", "http://localhost:8080", "user", "password", nil, nil, false)
	require.NoError(t, err, "failed to seed instance")

	manager := NewStreamManager(nil, nil, store)

	payload := []map[string]any{
		{
			"key":        "invalid-limit",
			"instanceId": 1,
			"page":       -1,
			"limit":      50,
			"sort":       "addedOn",
			"order":      "desc",
			"search":     "",
			"filters":    nil,
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)

	manager.Serve(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

// recordingProvider is a minimal sse.Provider that captures published messages for assertions.
type recordingProvider struct {
	mu       sync.Mutex
	messages map[string][]*sse.Message
}

func newRecordingProvider() *recordingProvider {
	return &recordingProvider{
		messages: make(map[string][]*sse.Message),
	}
}

func (p *recordingProvider) Subscribe(_ context.Context, _ sse.Subscription) error {
	return nil
}

func (p *recordingProvider) Publish(message *sse.Message, topics []string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, topic := range topics {
		p.messages[topic] = append(p.messages[topic], message)
	}
	return nil
}

func (p *recordingProvider) Shutdown(context.Context) error {
	return nil
}

func (p *recordingProvider) messagesFor(topic string) []*sse.Message {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]*sse.Message(nil), p.messages[topic]...)
}

func (p *recordingProvider) allMessages() []*sse.Message {
	p.mu.Lock()
	defer p.mu.Unlock()

	var result []*sse.Message
	for _, msgs := range p.messages {
		result = append(result, msgs...)
	}
	return result
}

func decodeStreamPayload(t *testing.T, message *sse.Message) *StreamPayload {
	t.Helper()

	raw, err := message.MarshalText()
	require.NoError(t, err, "failed to marshal SSE message")

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var builder strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			if builder.Len() > 0 {
				builder.WriteByte('\n')
			}
			builder.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}

	var payload StreamPayload
	err = json.Unmarshal([]byte(builder.String()), &payload)
	require.NoError(t, err, "failed to decode stream payload")
	return &payload
}

// testQuerier wraps *sql.DB to satisfy dbinterface.Querier for tests.
type testQuerier struct {
	db *sql.DB
}

func (q *testQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return q.db.QueryRowContext(ctx, query, args...)
}

func (q *testQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return q.db.ExecContext(ctx, query, args...)
}

func (q *testQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return q.db.QueryContext(ctx, query, args...)
}

func (q *testQuerier) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	tx, err := q.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &testTx{Tx: tx}, nil
}

type testTx struct {
	*sql.Tx
}

func (tx *testTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return tx.Tx.QueryRowContext(ctx, query, args...)
}

func (tx *testTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.Tx.ExecContext(ctx, query, args...)
}

func (tx *testTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return tx.Tx.QueryContext(ctx, query, args...)
}

func (tx *testTx) Commit() error {
	return tx.Tx.Commit()
}

func (tx *testTx) Rollback() error {
	return tx.Tx.Rollback()
}

func newTestInstanceStore(t *testing.T) (*models.InstanceStore, func()) {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err, "failed to open test database")

	// Ensure database resources are cleaned up on failure cases too.
	cleanup := func() {
		_ = sqlDB.Close()
	}

	// Create minimal schema needed by InstanceStore.
	schema := []string{
		`CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		);`,
		`CREATE TABLE instances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name_id INTEGER NOT NULL,
			host_id INTEGER NOT NULL,
			username_id INTEGER NOT NULL,
			password_encrypted TEXT NOT NULL,
			basic_username_id INTEGER,
			basic_password_encrypted TEXT,
			tls_skip_verify BOOLEAN NOT NULL DEFAULT 0,
			is_active BOOLEAN DEFAULT 1,
			last_connected_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (name_id) REFERENCES string_pool(id),
			FOREIGN KEY (host_id) REFERENCES string_pool(id),
			FOREIGN KEY (username_id) REFERENCES string_pool(id),
			FOREIGN KEY (basic_username_id) REFERENCES string_pool(id)
		);`,
		`CREATE VIEW instances_view AS
			SELECT 
				i.id,
				sp_name.value AS name,
				sp_host.value AS host,
				sp_username.value AS username,
				i.password_encrypted,
				sp_basic_username.value AS basic_username,
				i.basic_password_encrypted,
				i.tls_skip_verify
			FROM instances i
			INNER JOIN string_pool sp_name ON i.name_id = sp_name.id
			INNER JOIN string_pool sp_host ON i.host_id = sp_host.id
			INNER JOIN string_pool sp_username ON i.username_id = sp_username.id
			LEFT JOIN string_pool sp_basic_username ON i.basic_username_id = sp_basic_username.id;`,
	}

	for _, stmt := range schema {
		_, err := sqlDB.Exec(stmt)
		require.NoError(t, err, "failed to apply schema statement")
	}

	querier := &testQuerier{db: sqlDB}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	store, err := models.NewInstanceStore(querier, key)
	require.NoError(t, err, "failed to create instance store")

	return store, cleanup
}
