# Qui Desktop

**Status:** in progress

The desktop build is a **thin wrapper** around the same Qui **web app** you get in a
browser on the host loopback (default `http://localhost:7476`). Sign-in,
registration (`/setup`), sessions, and navigation are **only** in that web UI.

## Behaviour

- **Main window**: Uses **transparent macOS vibrancy**. On launch it shows a short “Starting Qui…” status, starts the Docker containers, waits until `GET /api/version` succeeds, then loads the real Qui page at `QUI_WEB_LOGIN_URL` (`http://localhost:7476/login`). That page is the same one a browser gets. There is no second sign-in form.
- **Tray** (menu bar): Docker status, transfer stats, pause/resume, update checks,
  **Reload Qui web app**, and Quit.
- **Docker**: Qui and qBittorrent containers are started before the web UI is opened. If they are stopped, the app starts them instead of failing the first navigation.
  The **Docker image update** dialog is shown **at most once per app session** (unless you use **Tray → Check for Docker Updates**), so digest quirks with multi-arch images do not spam prompts.
- **Touch ID (Electron only)**: After you **sign in or finish registration**, the shell may prompt once for **Touch ID** to lock the OS session to the app window.

## Related spec

- `BR-desktop-07` in `docs/specs/catalog.md` — main window loads the bundled Qui web URL (wrapper).
