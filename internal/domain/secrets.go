// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import "strings"

// RedactString replaces a string with asterisks of the same length
func RedactString(s string) string {
	if len(s) == 0 {
		return ""
	}

	return strings.Repeat("*", len(s))
}

// IsRedactedValue checks if a value appears to be redacted (all asterisks)
func IsRedactedValue(value string) bool {
	if value == "" {
		return false
	}

	// Check if the value is all asterisks
	for _, char := range value {
		if char != '*' {
			return false
		}
	}
	return true
}
