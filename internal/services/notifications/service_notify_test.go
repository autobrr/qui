// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notifications

import (
	"slices"
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

func TestOnlyATargetThatPicksSystemNoticeGetsIt(t *testing.T) {
	ctx := t.Context()
	store := models.NewNotificationTargetStore(testdb.NewMigratedSQLite(t, "notify-system-notice"))
	oldTypes := AllEventTypeStrings()
	oldTypes = slices.DeleteFunc(oldTypes, func(eventType string) bool { return eventType == string(EventSystemNotice) })
	// The targets from before the upgrade: one with each event type of that
	// time, and one with an empty list, which means all events.
	for _, eventTypes := range [][]string{oldTypes, nil} {
		_, err := store.Create(ctx, &models.NotificationTargetCreate{
			Name: "before the upgrade", URL: "generic://127.0.0.1/hook", Enabled: true, EventTypes: eventTypes,
		})
		require.NoError(t, err)
	}
	svc := NewService(store, nil, zerolog.Nop())

	svc.Notify(ctx, Event{Type: EventSystemNotice})
	require.Empty(t, svc.queue)

	_, err := store.Create(ctx, &models.NotificationTargetCreate{
		Name: "system notices", URL: "generic://127.0.0.1/hook", Enabled: true,
		EventTypes: []string{string(EventSystemNotice)},
	})
	require.NoError(t, err)
	svc.Notify(ctx, Event{Type: EventSystemNotice})
	require.Len(t, svc.queue, 1)
}

func TestStartReadsTheTargetsSoAnEventAfterADatabaseFaultStillFindsThem(t *testing.T) {
	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "notify-start")
	store := models.NewNotificationTargetStore(db)
	_, err := store.Create(ctx, &models.NotificationTargetCreate{
		Name: "system notices", URL: "generic://127.0.0.1/hook", Enabled: true,
		EventTypes: []string{string(EventSystemNotice)},
	})
	require.NoError(t, err)

	NewService(store, nil, zerolog.Nop()).Start(ctx)
	require.NoError(t, db.Close())

	targets, err := store.ListEnabled(ctx)
	require.NoError(t, err)
	require.Len(t, targets, 1)
}
