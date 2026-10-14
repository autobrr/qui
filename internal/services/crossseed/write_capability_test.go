// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// A remote instance has Read only, so every link-tree seam refuses it even
// with a link mode and a base dir configured.
func TestLinkTreeSeamsRefuseRemoteInstance(t *testing.T) {
	remote := &models.Instance{
		ID: 1, UseHardlinks: true, HardlinkBaseDir: "/links",
		SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey",
	}
	local := *remote
	local.HasLocalFilesystemAccess = true

	for _, tc := range []struct {
		name     string
		instance *models.Instance
		writable bool
	}{
		{"local", &local, true},
		{"remote", remote, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.writable, len(filterLinkEligible([]*models.Instance{tc.instance})) == 1, "season pack link eligibility")

			wantReason := "no_filesystem_access"
			if tc.writable {
				wantReason = ""
			}
			assert.Equal(t, wantReason, manualAssemblyUnavailableReason(tc.instance), "manual assembly reason")
		})
	}
}

// Partial pool runs host file calls, so a remote stays out even once #2942
// gives remotes Write.
func TestPartialPoolRefusesRemoteInstance(t *testing.T) {
	remote := &models.Instance{
		ID: 1, UseHardlinks: true, HardlinkBaseDir: "/links",
		SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey",
	}
	local := *remote
	local.HasLocalFilesystemAccess = true

	for _, tc := range []struct {
		name     string
		instance *models.Instance
		pooled   bool
	}{
		{"local", &local, true},
		{"remote", remote, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &Service{
				instanceStore:   &mockInstanceStore{instances: map[int]*models.Instance{tc.instance.ID: tc.instance}},
				automationStore: &models.CrossSeedStore{},
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
				},
			}

			assert.Equal(t, tc.pooled, service.partialPoolAdmissionEnabled(t.Context(), tc.instance, true, &CrossSeedRequest{}, false), "partial pool admission")

			member := &models.CrossSeedPartialPoolMember{InstanceID: tc.instance.ID, Mode: models.CrossSeedPartialPoolModeHardlink}
			enabled, err := service.partialPoolMemberModeEnabled(t.Context(), member)
			require.NoError(t, err)
			assert.Equal(t, tc.pooled, enabled, "partial pool member mode")
		})
	}
}
