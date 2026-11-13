// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package systemnotice holds the open System notices: faults in qui that need
// an action from the user. The web UI reads them, and the notification
// service gets one event when a notice opens.
package systemnotice

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/autobrr/qui/internal/services/notifications"
)

// Kind names a System notice. The web UI translates its title and fix by kind.
type Kind string

const (
	// RecordingStopped opens when qui cannot write the Transfer history.
	RecordingStopped Kind = "recording_stopped"
)

type Severity string

const SeverityError Severity = "error"

type definition struct {
	severity Severity
	title    string
	fix      string
}

var definitions = map[Kind]definition{
	RecordingStopped: {
		severity: SeverityError,
		title:    "Recording stopped",
		fix:      "qui cannot write the transfer history. It tries again at the next write. Make sure that the disk of the qui database has free space and that qui can write the database file.",
	},
}

const notifyTimeout = 10 * time.Second

type Notice struct {
	Kind     Kind      `json:"kind"`
	Severity Severity  `json:"severity"`
	Cause    string    `json:"cause"`
	OpenedAt time.Time `json:"openedAt"`
}

type Module struct {
	notifier notifications.Notifier

	mu   sync.Mutex
	open map[Kind]Notice
}

// New returns a module with no open notice. It sends each notice that opens
// to notifier.
func New(notifier notifications.Notifier) *Module {
	return &Module{notifier: notifier, open: make(map[Kind]Notice)}
}

// Open opens the notice of kind with its cause. A notice that is already open
// stays open with the new cause, and sends no second notification.
func (m *Module) Open(kind Kind, cause string) {
	m.mu.Lock()
	notice, isOpen := m.open[kind]
	if !isOpen {
		notice = Notice{Kind: kind, Severity: definitions[kind].severity, OpenedAt: time.Now().UTC()}
	}
	notice.Cause = cause
	m.open[kind] = notice
	m.mu.Unlock()
	if !isOpen {
		m.send(notice)
	}
}

// Close closes the notice of kind. It does nothing when the notice is not open.
func (m *Module) Close(kind Kind) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.open, kind)
}

// List returns the open notices, oldest first.
func (m *Module) List() []Notice {
	m.mu.Lock()
	defer m.mu.Unlock()
	notices := slices.SortedFunc(maps.Values(m.open), func(a, b Notice) int {
		return cmp.Or(a.OpenedAt.Compare(b.OpenedAt), strings.Compare(string(a.Kind), string(b.Kind)))
	})
	if notices == nil {
		// The API answers [] when no notice is open, not null.
		return []Notice{}
	}
	return notices
}

func (m *Module) send(notice Notice) {
	if m.notifier == nil {
		return
	}
	def := definitions[notice.Kind]
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	m.notifier.Notify(ctx, notifications.Event{
		Type:         notifications.EventSystemNotice,
		Title:        def.title,
		Message:      def.fix + "\nCause: " + notice.Cause,
		ErrorMessage: notice.Cause,
	})
}
