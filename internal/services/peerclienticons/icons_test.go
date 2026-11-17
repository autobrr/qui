// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package peerclienticons

import (
	"regexp"
	"testing"
)

func TestIconsListIsWellFormed(t *testing.T) {
	var (
		name   = regexp.MustCompile(`^[a-z0-9-]+$`)
		commit = regexp.MustCompile(`/[0-9a-f]{40}/`)
		hash   = regexp.MustCompile(`^[0-9a-f]{64}$`)
		fill   = regexp.MustCompile(`^#[0-9a-f]{6}$`)
	)
	seen := map[string]bool{}
	for _, icon := range Icons {
		if !name.MatchString(icon.Name) || seen[icon.Name] {
			t.Errorf("%q: name must be unique lowercase letters, digits and dashes", icon.Name)
		}
		seen[icon.Name] = true
		if !commit.MatchString(icon.URL) {
			t.Errorf("%s: URL %q has no 40-character commit SHA", icon.Name, icon.URL)
		}
		if !hash.MatchString(icon.SHA256) {
			t.Errorf("%s: hash %q is not 64 hex characters", icon.Name, icon.SHA256)
		}
		if icon.Fill != "" && !fill.MatchString(icon.Fill) {
			t.Errorf("%s: fill %q is not #rrggbb", icon.Name, icon.Fill)
		}
		if icon.ContentType != svg && icon.ContentType != png {
			t.Errorf("%s: content type %q is not SVG or PNG", icon.Name, icon.ContentType)
		}
	}
}
