// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package externalprograms

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/models"
)

func TestBuildCommand_WindowsCreationFlags(t *testing.T) {
	service := &Service{}
	ctx := context.Background()

	t.Run("terminal mode gets new console", func(t *testing.T) {
		cmd := service.buildCommand(ctx, &models.ExternalProgram{
			Path:        `C:\Programs\test.exe`,
			UseTerminal: true,
		}, []string{"arg1"})

		assert.NotNil(t, cmd.SysProcAttr)
		assert.Equal(t, newWindowsSysProcAttr(true).CreationFlags, cmd.SysProcAttr.CreationFlags)
	})

	t.Run("direct mode stays detached", func(t *testing.T) {
		cmd := service.buildCommand(ctx, &models.ExternalProgram{
			Path:        `C:\Programs\test.exe`,
			UseTerminal: false,
		}, []string{"arg1"})

		assert.NotNil(t, cmd.SysProcAttr)
		assert.Equal(t, newWindowsSysProcAttr(false).CreationFlags, cmd.SysProcAttr.CreationFlags)
	})
}
