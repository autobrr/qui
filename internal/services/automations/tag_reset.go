// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"github.com/autobrr/qui/internal/models"
)

func shouldResetTagActionInClient(action *models.TagAction) bool {
	if action == nil || !action.Enabled || action.UseTrackerAsTag {
		return false
	}

	if len(models.SanitizeCommaSeparatedStringSlice(action.Tags)) == 0 {
		return false
	}

	return action.DeleteFromClient
}
