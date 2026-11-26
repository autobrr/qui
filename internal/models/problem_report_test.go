// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

var (
	problemReportKey      = []byte("01234567890123456789012345678901")
	otherProblemReportKey = []byte("abcdefghijabcdefghijabcdefghijab")
)

func TestProblemReportStoreSQLite(t *testing.T) {
	checkProblemReportStore(t, testdb.NewMigratedSQLite(t, "problem-reports"))
}

func TestProblemReportStorePostgresIntegration(t *testing.T) {
	checkProblemReportStore(t, testdb.NewMigratedPostgres(t, "problem-reports"))
}

func checkProblemReportStore(t *testing.T, db dbinterface.Querier) {
	t.Helper()
	ctx := t.Context()
	store, err := models.NewProblemReportStore(db, problemReportKey)
	require.NoError(t, err)

	signIn, err := store.SignIn(ctx)
	require.NoError(t, err)
	assert.Nil(t, signIn, "anonymous before any sign-in")

	key, err := store.InstallKey(ctx)
	require.NoError(t, err)
	assert.Len(t, key, 43, "32 bytes in base64url")
	again, err := store.InstallKey(ctx)
	require.NoError(t, err)
	assert.Equal(t, key, again)

	var stored string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT install_key_encrypted FROM problem_report_install WHERE id = 1`).Scan(&stored))
	assert.NotContains(t, stored, key)

	require.NoError(t, store.SaveSignIn(ctx, models.ProblemReportSignIn{Token: "token-123", Provider: "github", Name: "someone"}))
	signIn, err = store.SignIn(ctx)
	require.NoError(t, err)
	require.NotNil(t, signIn)
	assert.Equal(t, models.ProblemReportSignIn{Token: "token-123", Provider: "github", Name: "someone"}, *signIn)
	require.NoError(t, store.ClearSignIn(ctx))
	signIn, err = store.SignIn(ctx)
	require.NoError(t, err)
	assert.Nil(t, signIn)

	require.NoError(t, store.SaveSignIn(ctx, models.ProblemReportSignIn{Token: "token-123", Provider: "github", Name: "someone"}))
	rekeyed, err := models.NewProblemReportStore(db, otherProblemReportKey)
	require.NoError(t, err)
	start := make(chan struct{})
	keys := make([]string, 8)
	var wg sync.WaitGroup
	for i := range keys {
		wg.Go(func() {
			<-start
			var keyErr error
			keys[i], keyErr = rekeyed.InstallKey(ctx)
			assert.NoError(t, keyErr)
		})
	}
	close(start)
	wg.Wait()
	newKey, err := rekeyed.InstallKey(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, key, newKey)
	for _, got := range keys {
		assert.Equal(t, newKey, got)
	}
	signIn, err = rekeyed.SignIn(ctx)
	require.NoError(t, err)
	assert.Nil(t, signIn)

	sentAt := time.Date(2026, 11, 19, 8, 30, 0, 0, time.UTC)
	issue := 4321
	issueURL := "https://github.com/autobrr/qui/issues/4321"
	first := models.SentProblemReport{BrrbinID: "r1", SubmissionID: "s1", Title: "first", SentAt: sentAt, Status: models.ProblemReportReceived, StatusChangedAt: sentAt}
	second := models.SentProblemReport{BrrbinID: "r2", SubmissionID: "s2", Title: "second", SentAt: sentAt.Add(time.Minute), Status: models.ProblemReportIssue, IssueNumber: &issue, IssueURL: &issueURL, IssueClosed: true, StatusChangedAt: sentAt.Add(time.Hour)}
	require.NoError(t, store.AddSentReport(ctx, first))
	require.NoError(t, store.AddSentReport(ctx, second))
	duplicate := first
	duplicate.Title = "duplicate"
	require.NoError(t, store.AddSentReport(ctx, duplicate))

	reports, err := store.SentReports(ctx)
	require.NoError(t, err)
	require.Len(t, reports, 2)
	for i := range reports {
		reports[i].SentAt = reports[i].SentAt.UTC()
		reports[i].StatusChangedAt = reports[i].StatusChangedAt.UTC()
	}
	assert.Equal(t, []models.SentProblemReport{second, first}, reports, "newest first, one row per submission")

	reason := "Already fixed in v1.36.0."
	closed := second
	closed.Status, closed.IssueNumber, closed.IssueURL, closed.IssueClosed, closed.CloseReason = models.ProblemReportClosed, nil, nil, false, &reason
	closed.StatusChangedAt = sentAt.Add(2 * time.Hour)
	require.NoError(t, store.UpdateSentReportStatus(ctx, closed))
	reports, err = store.SentReports(ctx)
	require.NoError(t, err)
	require.Len(t, reports, 2)
	reports[0].SentAt, reports[0].StatusChangedAt = reports[0].SentAt.UTC(), reports[0].StatusChangedAt.UTC()
	assert.Equal(t, closed, reports[0], "the issue fields are cleared")

	found, err := store.HasSentReport(ctx, "r1")
	require.NoError(t, err)
	assert.True(t, found)
	require.NoError(t, store.DeleteSentReport(ctx, "r1"))
	found, err = store.HasSentReport(ctx, "r1")
	require.NoError(t, err)
	assert.False(t, found)
	reports, err = store.SentReports(ctx)
	require.NoError(t, err)
	require.Len(t, reports, 1)
	assert.Equal(t, "r2", reports[0].BrrbinID)
}
