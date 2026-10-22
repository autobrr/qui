// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notifications

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestNotifyQueuesOnlyEventsATargetListensFor(t *testing.T) {
	ctx := t.Context()
	store := models.NewNotificationTargetStore(testdb.NewMigratedSQLite(t, "notify"))
	completed, err := store.Create(ctx, &models.NotificationTargetCreate{
		Name: "completed", URL: "generic://127.0.0.1/hook", Enabled: true,
		EventTypes: []string{string(EventTorrentCompleted)},
	})
	require.NoError(t, err)
	_, err = store.Create(ctx, &models.NotificationTargetCreate{
		Name: "disabled", URL: "generic://127.0.0.1/hook", Enabled: false,
		EventTypes: []string{string(EventTorrentAdded)},
	})
	require.NoError(t, err)

	svc := NewService(store, nil, zerolog.Nop())

	svc.Notify(ctx, Event{Type: EventTorrentAdded})
	require.Empty(t, svc.queue)

	svc.Notify(ctx, Event{Type: EventTorrentCompleted})
	require.Len(t, svc.queue, 1)

	added, err := store.Create(ctx, &models.NotificationTargetCreate{
		Name: "added", URL: "generic://127.0.0.1/hook", Enabled: true,
		EventTypes: []string{string(EventTorrentAdded)},
	})
	require.NoError(t, err)
	svc.Notify(ctx, Event{Type: EventTorrentAdded})
	require.Len(t, svc.queue, 2)

	_, err = store.Update(ctx, added.ID, &models.NotificationTargetUpdate{
		Name: "added", URL: "generic://127.0.0.1/hook", Enabled: false,
		EventTypes: []string{string(EventTorrentAdded)},
	})
	require.NoError(t, err)
	svc.Notify(ctx, Event{Type: EventTorrentAdded})
	require.Len(t, svc.queue, 2)

	require.NoError(t, store.Delete(ctx, completed.ID))
	svc.Notify(ctx, Event{Type: EventTorrentCompleted})
	require.Len(t, svc.queue, 2)
}
