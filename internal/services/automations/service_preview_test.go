package automations

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
)

type testDBQuerier struct {
	db *sql.DB
}

func (q *testDBQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return q.db.QueryRowContext(ctx, query, args...)
}

func (q *testDBQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return q.db.ExecContext(ctx, query, args...)
}

func (q *testDBQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return q.db.QueryContext(ctx, query, args...)
}

func (q *testDBQuerier) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	tx, err := q.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

func TestSetupPreviewTrackerDisplayNames_LoadsWhenTrackerFieldUsed(t *testing.T) {
	ctx := context.Background()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	q := &testDBQuerier{db: sqlDB}
	_, err = q.ExecContext(ctx, `
		CREATE TABLE tracker_customizations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			display_name TEXT NOT NULL,
			domains TEXT NOT NULL DEFAULT '',
			included_in_stats TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)
	`)
	require.NoError(t, err)

	now := time.Now().UTC()
	_, err = q.ExecContext(ctx, `
		INSERT INTO tracker_customizations (display_name, domains, included_in_stats, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, "BHD", "bhd.example", "", now, now)
	require.NoError(t, err)

	store := models.NewTrackerCustomizationStore(q)
	s := &Service{
		trackerCustomizationStore: store,
	}

	evalCtx := &EvalContext{}
	cond := &RuleCondition{
		Field:    FieldTracker,
		Operator: OperatorNotEqual,
		Value:    "BHD",
	}

	s.setupPreviewTrackerDisplayNames(ctx, 1, cond, evalCtx)

	require.NotNil(t, evalCtx.TrackerDisplayNameByDomain)
	assert.Equal(t, "BHD", evalCtx.TrackerDisplayNameByDomain["bhd.example"])
}

func TestSetupPreviewTrackerDisplayNames_SkipsWhenTrackerFieldNotUsed(t *testing.T) {
	ctx := context.Background()

	s := &Service{
		trackerCustomizationStore: models.NewTrackerCustomizationStore(&mockQuerier{}),
	}

	evalCtx := &EvalContext{}
	cond := &RuleCondition{
		Field:    FieldTags,
		Operator: OperatorEqual,
		Value:    "tier1",
	}

	s.setupPreviewTrackerDisplayNames(ctx, 1, cond, evalCtx)

	assert.Nil(t, evalCtx.TrackerDisplayNameByDomain)
}
