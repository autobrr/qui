# Web + Electron desktop shell

**Status:** in progress

When Qui runs inside the desktop Electron wrapper, the preload exposes
`window.quiDesktop.requestTouchId`. After a successful **login** or **first-time
registration**, the web app sets a session flag so the **authenticated** layout
runs a **one-time** Touch ID prompt. In a normal browser nothing is injected and
no prompt runs.

## Related specs

- `BR-web-01` in `docs/specs/catalog.md` — desktop biometric gate session flag.
