// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package secrets

type Kind uint8

const (
	TrackerPasskey Kind = iota
	NotificationToken
)

const (
	everyKind = 1<<TrackerPasskey | 1<<NotificationToken
	audiences = everyKind + 1
)

type Audience struct {
	kinds uint8
}

func NewAudience(sees ...Kind) Audience {
	var a Audience
	for _, k := range sees {
		a = a.with(k)
	}
	return a
}

func Full() Audience {
	return Audience{kinds: everyKind}
}

func (a Audience) sees(k Kind) bool {
	return a.kinds&(1<<k) != 0
}

func (a Audience) with(k Kind) Audience {
	return Audience{kinds: (a.kinds | 1<<k) & everyKind}
}
