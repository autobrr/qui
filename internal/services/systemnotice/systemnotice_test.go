// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package systemnotice_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/systemnotice"
)

// sent keeps each event that the module sends.
type sent struct {
	mu     sync.Mutex
	events []notifications.Event
}

func (s *sent) Notify(_ context.Context, event notifications.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *sent) all() []notifications.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]notifications.Event(nil), s.events...)
}

func TestANoticeOpensOnceAndSendsOneNotification(t *testing.T) {
	t.Parallel()
	notifier := &sent{}
	notices := systemnotice.New(notifier)

	notices.Open(systemnotice.RecordingStopped, "attempt to write a readonly database")
	notices.Open(systemnotice.RecordingStopped, "disk I/O error")

	open := notices.List()
	require.Len(t, open, 1)
	require.Equal(t, systemnotice.RecordingStopped, open[0].Kind)
	require.Equal(t, systemnotice.SeverityError, open[0].Severity)
	require.Equal(t, "disk I/O error", open[0].Cause, "the notice shows the latest cause")

	events := notifier.all()
	require.Len(t, events, 1)
	require.Equal(t, notifications.EventSystemNotice, events[0].Type)
	require.Equal(t, "Recording stopped", events[0].Title)
	require.Contains(t, events[0].Message, "attempt to write a readonly database")
}

func TestAClosedNoticeSendsAgainWhenItOpensAgain(t *testing.T) {
	t.Parallel()
	notifier := &sent{}
	notices := systemnotice.New(notifier)

	notices.Open(systemnotice.RecordingStopped, "disk I/O error")
	notices.Close(systemnotice.RecordingStopped)
	require.Empty(t, notices.List())

	notices.Open(systemnotice.RecordingStopped, "disk I/O error")
	require.Len(t, notices.List(), 1)
	require.Len(t, notifier.all(), 2)
}

func TestDismiss(t *testing.T) {
	t.Parallel()
	notices := systemnotice.New(nil)
	notices.Open(systemnotice.RecordingStopped, "disk I/O error")
	notices.Open(systemnotice.ShrinkFailed, "database or disk is full")

	require.ErrorIs(t, notices.Dismiss(t.Context(), systemnotice.RecordingStopped), systemnotice.ErrNotDismissible)
	require.NoError(t, notices.Dismiss(t.Context(), systemnotice.ShrinkFailed))

	open := notices.List()
	require.Len(t, open, 1)
	require.Equal(t, systemnotice.RecordingStopped, open[0].Kind, "a notice that closes itself stays open")
}

func TestTheNotificationGivesTheFixOfTheReason(t *testing.T) {
	t.Parallel()
	notifier := &sent{}
	notices := systemnotice.New(notifier)

	notices.OpenWithReason(systemnotice.ShrinkFailed, "disk_full", "database or disk is full")

	open := notices.List()
	require.Len(t, open, 1)
	require.Equal(t, "disk_full", open[0].Reason)
	require.Equal(t, systemnotice.SeverityWarning, open[0].Severity)
	events := notifier.all()
	require.Len(t, events, 1)
	require.Equal(t, "Shrink failed", events[0].Title)
	require.Contains(t, events[0].Message, "Free space on the disk")
}
