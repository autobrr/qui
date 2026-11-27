// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package secrets

import "errors"

var (
	ErrNoMatch   = errors.New("secrets: the Stand-in matches no saved value")
	ErrAmbiguous = errors.New("secrets: the Stand-in matches more than one saved value")
)

func Restore(a Audience, k Kind, sent string, saved func() ([]string, error)) (string, error) {
	if a.sees(k) || !IsStandIn(sent) {
		return sent, nil
	}
	values, err := saved()
	if err != nil {
		return "", err
	}
	var match string
	found := false
	for _, v := range values {
		if v != sent && StandIn(v) != sent {
			continue
		}
		if found && v != match {
			return "", ErrAmbiguous
		}
		match, found = v, true
	}
	if !found {
		return "", ErrNoMatch
	}
	return match, nil
}
