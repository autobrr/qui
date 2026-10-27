// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package tray

import (
	_ "embed"
	stdlog "log"
	"os"
	"strings"
	"sync/atomic"

	"fyne.io/systray"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

//go:embed qui.ico
var icon []byte

const (
	runKey   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue = "qui"
)

var running atomic.Bool

// Run shows the Tray and runs its message loop on the calling goroutine, which
// must be the main one. It returns after Remove. When the icon cannot be
// added, for example in a session without a desktop, it logs a warning and
// blocks while qui keeps serving.
func Run(m Menu) {
	// systray reports its errors, a failed icon add included, through the
	// standard logger. stderr is not a valid handle here, so send them to the log.
	stdlog.SetFlags(0)
	stdlog.SetOutput(warnWriter{})
	running.Store(true)
	// No onExit: systray runs it on every WM_ENDSESSION, a cancelled logoff too.
	systray.Run(func() { onReady(m) }, nil)
}

// Remove deletes the icon before qui exits. Without it, a dead icon stays in
// the notification area until the mouse moves over it.
func Remove() {
	if running.Load() {
		systray.Quit()
	}
}

// ShowError shows msg in a native error dialog and waits until the user closes it.
func ShowError(msg string) {
	text, _ := windows.UTF16PtrFromString(msg)
	caption, _ := windows.UTF16PtrFromString("qui")
	_, _ = windows.MessageBox(0, text, caption, windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND)
}

func onReady(m Menu) {
	systray.SetIcon(icon)
	systray.SetTooltip("qui " + m.Version + "\n" + m.URL)
	systray.SetOnTapped(func() { shellOpen(m.URL) })

	openItem := systray.AddMenuItem("Open qui", "")
	restart := systray.AddMenuItem("Restart", "")
	folder := systray.AddMenuItem("Open config folder", "")
	systray.AddSeparator()
	startup := systray.AddMenuItemCheckbox("Start with Windows", "", startsWithWindows(m.Binary))
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "")

	go func() {
		for {
			select {
			case <-openItem.ClickedCh:
				shellOpen(m.URL)
			case <-restart.ClickedCh:
				m.Restart()
			case <-folder.ClickedCh:
				shellOpen(m.ConfigDir)
			case <-startup.ClickedCh:
				if err := setStartWithWindows(m.Binary, !startup.Checked()); err != nil {
					log.Error().Err(err).Msg("could not change Start with Windows")
					continue
				}
				if startup.Checked() {
					startup.Uncheck()
				} else {
					startup.Check()
				}
			case <-quit.ClickedCh:
				m.Quit()
			}
		}
	}()
}

// shellOpen opens a URL or a folder with its default handler.
func shellOpen(target string) {
	verb, _ := windows.UTF16PtrFromString("open")
	file, _ := windows.UTF16PtrFromString(target)
	if err := windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		log.Error().Err(err).Str("target", target).Msg("could not open")
	}
}

// startsWithWindows reports whether the Run value exists and starts this binary
// with the flags it runs with now.
func startsWithWindows(binary string) bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	value, _, err := key.GetStringValue(runValue)
	return err == nil && strings.EqualFold(value, runCommand(binary))
}

// runCommand keeps the serve flags, so a Tray started with --config-dir starts
// on the same config at logon. The supervisor passes os.Args to this child.
func runCommand(binary string) string {
	return windows.ComposeCommandLine(append([]string{binary}, os.Args[1:]...))
}

// setStartWithWindows writes the Run value under HKCU, which needs no admin rights.
func setStartWithWindows(binary string, enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if !enabled {
		return key.DeleteValue(runValue)
	}
	return key.SetStringValue(runValue, runCommand(binary))
}

type warnWriter struct{}

func (warnWriter) Write(p []byte) (int, error) {
	log.Warn().Str("component", "tray").Msg(strings.TrimSpace(string(p)))
	return len(p), nil
}
