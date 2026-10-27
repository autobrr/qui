// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

// The icon of qui.exe and qui-tray.exe in Explorer and Task Manager.
//go:generate go run github.com/akavel/rsrc@v0.10.2 -ico ../../internal/tray/qui.ico -arch amd64 -o rsrc_windows_amd64.syso

import (
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/autobrr/qui/internal/buildinfo"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/tray"
	"github.com/autobrr/qui/internal/update"
)

// runTray is main for qui-tray.exe. It always serves and takes only the serve
// flags: a GUI program has no console for the output of another command.
func runTray() {
	// Cobra otherwise exits after 5 s with a "use cmd.exe" message when
	// Explorer starts the exe: a double-click or the Start with Windows Run value.
	cobra.MousetrapHelpText = ""
	serve := RunServeCommand()
	serve.Use = "qui-tray"
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	serve.SetArgs(args)
	serve.Args = func(_ *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("qui-tray.exe only runs the server. Use qui.exe for the %q command", args[0])
		}
		return nil
	}
	if err := serve.Execute(); err != nil {
		tray.ShowError(err.Error())
		os.Exit(1)
	}
}

func (app *Application) trayMenu(cfg *config.AppConfig, binary string, restarter *update.Restarter) tray.Menu {
	return tray.Menu{
		Version:   buildinfo.Version,
		URL:       tray.URL(cfg.Config.Host, cfg.Config.Port, cfg.Config.BaseURL),
		ConfigDir: cfg.GetConfigDir(),
		Binary:    binary,
		Restart: func() {
			if !restarter.TryLock() {
				log.Info().Msg("Tray Restart ignored: an update or restart is already running")
				return
			}
			if err := restarter.Request(); err != nil {
				restarter.Unlock()
				log.Error().Err(err).Msg("refused restart")
			}
		},
		Quit: func() {
			select {
			case app.quit <- struct{}{}:
			default:
			}
		},
	}
}
