// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestService_LookupExternalIDsReadsOnlyAPartialAnswerAsIncompleteNearItsEnd(t *testing.T) {
	testCachedAnswerNearItsEnd(t, func(t *testing.T) *database.DB {
		return testdb.NewMigratedSQLite(t, "arr-cache-lifetime")
	})
}

func TestService_LookupExternalIDsReadsOnlyAPartialAnswerAsIncompleteNearItsEndPostgresIntegration(t *testing.T) {
	testCachedAnswerNearItsEnd(t, func(t *testing.T) *database.DB {
		return testdb.NewMigratedPostgres(t, "arr-cache-lifetime")
	})
}

func testCachedAnswerNearItsEnd(t *testing.T, openDB func(t *testing.T) *database.DB) {
	t.Helper()
	ids := &models.ExternalIDs{TVDbID: 471000}
	tests := []struct {
		name           string
		ids            *models.ExternalIDs
		kept           time.Duration
		wantIncomplete bool
	}{
		{name: "a full answer", ids: ids, kept: DefaultPositiveCacheTTL},
		{name: "a no IDs answer", kept: DefaultNegativeCacheTTL},
		{name: "a partial answer", ids: ids, kept: incompleteCacheTTL, wantIncomplete: true},
		{name: "a partial no IDs answer", kept: incompleteCacheTTL, wantIncomplete: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openDB(t)
			cacheStore := models.NewArrIDCacheStore(db)
			service := NewService(nil, cacheStore)
			service.nextCacheCleanup = time.Now().Add(time.Hour)
			titleHash := models.ComputeTitleHash(solitudeRelease)
			episodeMap := &models.EpisodeMap{Season: 1, Episode: 1, Absolute: 1}
			require.NoError(t, cacheStore.SetWithTitles(t.Context(), titleHash, string(ContentTypeTV), nil, tt.ids, []string{"Solitude"}, episodeMap, true, tt.ids == nil, tt.kept))
			entry, err := cacheStore.Get(t.Context(), titleHash, string(ContentTypeTV))
			require.NoError(t, err)
			require.Equal(t, tt.kept, entry.ExpiresAt.Sub(entry.CachedAt))
			age := time.Until(entry.ExpiresAt) - 5*time.Minute
			_, err = db.ExecContext(t.Context(), `UPDATE arr_id_cache SET cached_at = ?, expires_at = ? WHERE id = ?`,
				entry.CachedAt.Add(-age), entry.ExpiresAt.Add(-age), entry.ID)
			require.NoError(t, err)

			result, err := service.LookupExternalIDs(t.Context(), solitudeRelease, ContentTypeTV)

			require.NoError(t, err)
			require.True(t, result.FromCache)
			require.Equal(t, tt.wantIncomplete, result.Incomplete)
		})
	}
}
