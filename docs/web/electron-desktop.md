# Web + Electron desktop shell

**Status:** in progress

When Qui runs inside the desktop Electron wrapper, the preload exposes
`window.quiDesktop.requestTouchId`. After a successful **login** or **first-time
registration**, the web app sets a session flag so the **authenticated** layout
runs a **one-time** Touch ID prompt. In a normal browser nothing is injected and
no prompt runs.

The Glassmorphism slider calls `window.quiDesktop.setWindowMaterial` with a
boolean: true when the theme color is below 100%, false at 100%. The native
window stays clear either way. Below 100%, the page shell, sidebar, cards, and
popovers keep that percentage of the active theme, and the shell turns on HUD
vibrancy so the desktop behind the window is blurred. At 100% the page is
solid and vibrancy is off. In a normal browser the helper does nothing.
Glassmorphism is not its own theme.

Setup and sign-in call `saveAccount`. On the login page, `unlockAccount` asks
for Touch ID and returns the saved username and password. Pages that show the
sidebar call `setSidebarChrome(true)` so the traffic lights line up with the
logo.

## Related specs

- `BR-web-01` in `docs/specs/catalog.md` — desktop biometric gate session flag.
- `BR-desktop-05` in `docs/specs/catalog.md` — clear window, 50% page while glass is on.
