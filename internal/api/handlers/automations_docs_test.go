// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"os"
	"regexp"
	"testing"

	"github.com/autobrr/qui/internal/models"
)

var docsJSONBlock = regexp.MustCompile("(?ms)^[ \t]*```json\n(.*?)^[ \t]*```")

// TestAutomationDocsExamples runs each json block on the automations docs page through the rule check, so a docs example cannot drift from the code.
// The instance has local filesystem access, so the check does not reject fields that need it.
// The check does not look up programs or target instances.
func TestAutomationDocsExamples(t *testing.T) {
	doc, err := os.ReadFile("../../../documentation/docs/features/automations.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := docsJSONBlock.FindAllSubmatch(doc, -1)
	if len(blocks) == 0 {
		t.Fatal("no json blocks found")
	}
	instance := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	for i, block := range blocks {
		if _, err := decodeAutomationPayload(bytes.NewReader(block[1]), instance.ID, instance, nil); err != nil {
			t.Errorf("json block %d:\n%s\n%v", i+1, block[1], err)
		}
	}
}
